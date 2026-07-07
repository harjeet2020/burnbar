// Package ui is the Bubble Tea layer: the root model, message handling,
// and all rendering. It follows the MVU split — model.go holds state,
// update.go mutates it, view.go and the per-region renderers draw it.
// Everything data-shaped lives in internal/core; the data layer that
// feeds it lives in internal/data. ui only arranges what they produce.
package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"

	"github.com/harjeet2020/burnbar/tui/internal/core"
	"github.com/harjeet2020/burnbar/tui/internal/data"
)

// screen identifies which view owns region 2 (the only region whose
// content ever changes — header, status, and hints persist, tui/SPEC.md §2).
type screen int

const (
	screenMeter screen = iota
	screenDetails
)

// Model is the single Bubble Tea state struct for the whole app.
type Model struct {
	cfg data.Config

	theme  Theme
	glyphs Glyphs
	keys   KeyMap
	help   help.Model

	// width/height cache the latest WindowSizeMsg — all layout derives
	// from them on every render (tui/SPEC.md §4).
	width  int
	height int

	// --- Data layer (Stage C) ---------------------------------------
	// rest/credits/live are the clients; sched debounces credit polls.
	rest    *data.RESTClient
	credits *data.CreditsClient
	live    data.LiveSource
	liveCh  <-chan data.LiveEvent
	liveCtx context.Context
	// liveCancel stops the running source; the `p` toggle calls it to swap
	// sources at runtime. liveGen tags the active source so events from a
	// stopped one are ignored (mirrors heartbeatGen).
	liveCancel context.CancelFunc
	liveGen    int
	sched      *data.CreditScheduler

	// Stores the snapshot is aggregated from (tui/SPEC.md §7): the 30-day
	// baseline view rows and the deduped raw rows (today-slice ∪ live).
	baseline []core.DailyRow
	rows     *core.RowStore
	loc      *time.Location
	// anchor is the accent1 reset point — app start, last refresh, or the
	// last debounced focus-gain. The *effective* anchor the aggregate uses
	// is max(anchor, now−accentWindow), a rolling floor so live rows older
	// than accentWindow stop being highlighted even without a focus event
	// (tui/SPEC.md §5) — see effectiveAnchor.
	anchor time.Time
	// focusGen guards the debounced focus-clear timer: a newer focus-gain
	// bumps it so a stale focusClearMsg is ignored (mirrors heartbeatGen).
	focusGen int
	// pendingFocusAt is the time of the most recent focus-gain; the
	// debounced clear jumps the anchor here so accent1 clears ~1 s after
	// you click into the app (tui/SPEC.md §5).
	pendingFocusAt time.Time

	// Credits state, kept out of the snapshot until rebuilt() folds it in.
	creditsVal   *float64
	creditsAt    time.Time
	creditsHint  string // e.g. "check openrouter_api_key"; "" when healthy
	heartbeatGen int    // generation guard for the idle credits heartbeat

	// Connection + load state.
	conn    core.ConnState
	loading bool      // true until the first baseline lands
	dataErr string    // last baseline/slice fetch error, "" when clean
	dataAt  time.Time // when the currently shown baseline was fetched

	// tf is the active window; snap is the snapshot derived for it.
	tf   core.Timeframe
	snap core.Snapshot

	// --- Stage D animation (tui/SPEC.md §6) -------------------------
	// spring is the shared config; anim holds one animated bar width per
	// model (keyed by name, like selected, so it survives re-sorts).
	// animating guards the ~30 fps tick loop so it never stacks and stops
	// the instant every bar settles (idle = 0 fps).
	spring    harmonica.Spring
	anim      map[string]*barAnim
	animating bool
	// accentEmphasisUntil is the deadline through which a freshly-arrived
	// accent2 slice renders bold — the arrival signal (tui/SPEC.md §6).
	accentEmphasisUntil time.Time

	// selected tracks the selected model by *name*, not row index, so
	// selection survives re-sorts and window switches (tui/SPEC.md §2).
	selected string
	// scroll is the index of the first visible model block when the list
	// is in scroll mode.
	scroll int

	scr      screen
	showHelp bool
}

// New assembles the initial model from a validated config: the render
// scaffolding (theme, glyphs, keymap) plus the data-layer clients and
// empty stores. No I/O happens here — Init kicks off the live feed and
// the first fetches (tui/SPEC.md §7 startup sequence).
func New(cfg data.Config) Model {
	glyphs := DefaultGlyphs()
	theme := DefaultTheme()

	rest := data.NewRESTClient(cfg)
	live, _ := data.NewLiveSource(cfg, rest)

	// A cancellable context lets the `p` toggle stop the source and start a
	// fresh one (Start is once-only, so switching needs a new instance).
	ctx, cancel := context.WithCancel(context.Background())

	m := Model{
		cfg:        cfg,
		theme:      theme,
		glyphs:     glyphs,
		keys:       newKeyMap(glyphs),
		help:       newHelpModel(theme, glyphs),
		rest:       rest,
		live:       live,
		liveCtx:    ctx,
		liveCancel: cancel,
		sched:      data.NewCreditScheduler(),
		rows:       core.NewRowStore(),
		loc:        time.Local,
		anchor:     time.Now(),
		conn:       initialConn(live),
		loading:    true,
		tf:         core.TimeframeToday,
		spring:     newSpring(),
		anim:       make(map[string]*barAnim),
	}
	if cfg.HasCredentialsForCredits() {
		m.credits = data.NewCreditsClient(cfg)
	} else {
		m.creditsHint = "set openrouter_api_key"
	}
	return m.rebuilt(time.Now())
}

// initialConn is the connection state shown before the first live event:
// polling sources are "polling" from the outset, realtime is "reconnecting"
// until the first join confirms.
func initialConn(live data.LiveSource) core.ConnState {
	if live != nil && live.Name() == "polling" {
		return core.ConnPolling
	}
	return core.ConnReconnecting
}

// Init starts the live feed and the initial credits fetch. The baseline
// and today-slice fetches are deferred until the source's first join
// (subscribe before fetch, tui/SPEC.md §7) — see the LiveJoined handler.
func (m Model) Init() tea.Cmd {
	now := time.Now()
	cmds := []tea.Cmd{
		m.startLiveCmd(),
		// Stage D steady-state timers (tui/SPEC.md §6/§7): the relative-time
		// tick and the two midnight rollovers, each self-rescheduling.
		armRelativeTick(),
		armLocalRollover(now, m.loc),
		armUTCRollover(now),
	}
	if m.credits != nil {
		cmds = append(cmds, m.fetchCreditsCmd())
	}
	return tea.Batch(cmds...)
}

// rebuilt recomputes the derived snapshot from the current stores, credits,
// and connection state — the single place Aggregate() is called and the
// heart of the data→view path (tui/SPEC.md §7). It also repairs the
// selection and scroll offset for the new model list.
func (m Model) rebuilt(now time.Time) Model {
	rows := m.rows.Rows()
	models := core.Aggregate(core.AggregateInput{
		Baseline: m.baseline,
		Rows:     rows,
		Window:   m.tf,
		Now:      now,
		Loc:      m.loc,
		Anchor:   m.effectiveAnchor(now),
	})

	var spend float64
	for _, st := range models {
		spend += st.Cost
	}
	lastReq, lag := core.SnapshotMeta(rows, m.tf, now, m.loc)

	m.snap = core.Snapshot{
		Timeframe:     m.tf,
		Models:        models,
		Spend:         spend,
		Credits:       m.creditsVal,
		CreditsAt:     m.creditsAt,
		LastRequestAt: lastReq,
		LagSeconds:    lag,
		Conn:          m.conn,
	}

	// Selection follows the model name; when it left the window, fall back
	// to the top model.
	if m.selectedIndex() < 0 {
		m.selected = ""
		if len(models) > 0 {
			m.selected = models[0].Name
		}
	}
	m.scroll = m.currentLayout().clampScroll(m.scroll)
	m = m.reconcileAnim()
	return m.ensureSelectionVisible()
}

// effectiveAnchor is the accent1 anchor the aggregate actually uses:
// max(m.anchor, now−accentWindow). The rolling floor makes accent1 a
// "recent activity" window that ages out on its own — applied
// unconditionally, since keyboard focus is the only signal terminals
// report and the meter's usual home is visible-but-unfocused beside the
// working terminal (tui/SPEC.md §5).
func (m Model) effectiveAnchor(now time.Time) time.Time {
	if floor := now.Add(-accentWindow); floor.After(m.anchor) {
		return floor
	}
	return m.anchor
}

// selectedIndex resolves the selected model name to its position in the
// current snapshot; -1 when the selection isn't in this window.
func (m Model) selectedIndex() int {
	for i, st := range m.snap.Models {
		if st.Name == m.selected {
			return i
		}
	}
	return -1
}

// scale is the shared bar scale S for the current snapshot (tui/SPEC.md §3).
func (m Model) scale() int64 {
	var max int64
	for _, st := range m.snap.Models {
		if t := st.TotalTokens(); t > max {
			max = t
		}
	}
	return core.ScaleFor(max)
}
