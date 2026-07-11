// Package ui is the Bubble Tea layer: the root model, message handling,
// and all rendering. It follows the MVU split — model.go holds state,
// update.go mutates it, view.go and the per-region renderers draw it.
// Everything data-shaped lives in internal/core; the data layer that
// feeds it lives in internal/data. ui only arranges what they produce.
package ui

import (
	"context"
	"time"

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
	screenTheme
)

// Model is the single Bubble Tea state struct for the whole app.
type Model struct {
	cfg data.Config

	theme  Theme
	glyphs Glyphs
	keys   KeyMap

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

	// Stores the snapshot is aggregated from (tui/SPEC.md §7): the 31-day
	// baseline view rows and the deduped raw rows (today-slice ∪ live).
	baseline []core.DailyRow
	rows     *core.RowStore
	loc      *time.Location

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

	// tf is the active window; mode is the active bar display mode (`m`
	// key, tui/SPEC.md §3); snap is the snapshot derived for them.
	tf   core.Timeframe
	mode core.Mode
	snap core.Snapshot
	// manualScale, when non-nil, pins the shared bar scale (tui/SPEC.md §3
	// manual zoom, Stage D.3) instead of the auto-ranged core.ScaleFor
	// result. Transient view state, not config: reset to nil on refresh,
	// timeframe switch, and mode toggle. Auto-ranging does not re-engage
	// while it holds, even if new data overflows it (bars just clamp).
	manualScale *float64
	// burst is the most recent coalesced live-request burst (tui/SPEC.md
	// §5/§7), recomputed in rebuilt(); nil until the first live row of the
	// session. Drives the single latest-burst highlight.
	burst *core.Burst

	// --- Stage D animation (tui/SPEC.md §6) -------------------------
	// spring is the shared config; anim holds one animated bar width per
	// model (keyed by name, like selected, so it survives re-sorts).
	// animating guards the ~30 fps tick loop so it never stacks and stops
	// the instant every bar settles (idle = 0 fps).
	spring    harmonica.Spring
	anim      map[string]*barAnim
	animating bool
	// animSync marks the in-flight batch as synchronized — set by
	// withSyncedAnim, consumed by stepAnim (tui/SPEC.md §6 fade).
	animSync bool
	// accentEmphasisUntil is the deadline through which a freshly-arrived
	// burst renders bold — the arrival signal (tui/SPEC.md §6).
	accentEmphasisUntil time.Time

	// selected tracks the selected model by *name*, not row index, so
	// selection survives re-sorts and window switches (tui/SPEC.md §2).
	selected string
	// scroll is the index of the first visible model block when the list
	// is in scroll mode.
	scroll int
	// detailsScroll is the line-index scroll offset for the details
	// screen's flat content list (computeViewport) — independent of
	// scroll, the bars list's block-indexed offset (tui/SPEC.md §2 Stage
	// E).
	detailsScroll int

	// colors is the committed, source-of-truth palette (tui/SPEC.md §5
	// Stage E.1) — theme is always buildTheme(colors), rebuilt whenever
	// colors changes (New, and a theme-picker save).
	colors themeColors
	// colorsHint is a one-time startup diagnostic naming any [colors]
	// token that fell back to default due to an invalid value ("" when
	// clean) — surfaced on the theme screen itself, not the fixed rows.
	colorsHint string
	// themeCursor is the selected token row (0..len(themeTokens)-1) on
	// the theme screen.
	themeCursor int
	// themeScroll is the theme screen's own viewport scroll offset —
	// mirrors detailsScroll, independent of it.
	themeScroll int
	// themeDraft is the picker's uncommitted working copy: cycling,
	// toggling bright, and resetting mutate only this. colors (and theme)
	// only change on save (`s`). Re-seeded from colors every time the
	// picker opens, so repeated open/edit/cancel/open cycles always start
	// from the last *committed* state, never silently from built-in
	// defaults.
	themeDraft themeColors

	scr       screen
	showHelp  bool
	showModal bool
}

// New assembles the initial model from a validated config: the render
// scaffolding (theme, glyphs, keymap) plus the data-layer clients and
// empty stores. No I/O happens here — Init kicks off the live feed and
// the first fetches (tui/SPEC.md §7 startup sequence).
func New(cfg data.Config) Model {
	glyphs := DefaultGlyphs()
	colors, colorsHint := resolveThemeColors(cfg.Colors)
	theme := buildTheme(colors)

	rest := data.NewRESTClient(cfg)
	live, _ := data.NewLiveSource(cfg, rest)

	// A cancellable context lets the `p` toggle stop the source and start a
	// fresh one (Start is once-only, so switching needs a new instance).
	ctx, cancel := context.WithCancel(context.Background())

	m := Model{
		cfg:        cfg,
		theme:      theme,
		glyphs:     glyphs,
		keys:       newKeyMap(),
		rest:       rest,
		live:       live,
		liveCtx:    ctx,
		liveCancel: cancel,
		sched:      data.NewCreditScheduler(),
		rows:       core.NewRowStore(),
		loc:        time.Local,
		conn:       initialConn(live),
		loading:    true,
		tf:         core.TimeframeToday,
		spring:     newSpring(),
		anim:       make(map[string]*barAnim),
		colors:     colors,
		colorsHint: colorsHint,
		themeDraft: colors,
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
		Mode:     m.mode,
	})

	var spend float64
	var totalTokens int64
	for _, st := range models {
		spend += st.Cost
		totalTokens += st.TotalTokens()
	}
	lastReq, lag := core.SnapshotMeta(rows, m.tf, now, m.loc)

	m.snap = core.Snapshot{
		Timeframe:     m.tf,
		Models:        models,
		Spend:         spend,
		TotalTokens:   totalTokens,
		Credits:       m.creditsVal,
		CreditsAt:     m.creditsAt,
		LastRequestAt: lastReq,
		LagSeconds:    lag,
		Conn:          m.conn,
	}
	m.burst = core.LatestBurst(rows)

	// Selection follows the model name; when it left the window, fall back
	// to the top model.
	if m.selectedIndex() < 0 {
		m.selected = ""
		if len(models) > 0 {
			m.selected = models[0].Name
		}
	}
	m.scroll = m.currentLayout().clampScroll(m.scroll)
	m = m.clampDetailsScroll()
	m = m.reconcileAnim()
	return m.ensureSelectionVisible()
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

// selectedModel resolves the current selection to its ModelStat; ok is
// false when nothing is selected or the selection isn't in this window
// (tui/SPEC.md §2 Stage E).
func (m Model) selectedModel() (core.ModelStat, bool) {
	idx := m.selectedIndex()
	if idx < 0 {
		return core.ModelStat{}, false
	}
	return m.snap.Models[idx], true
}

// scale is the shared bar scale S for the current snapshot, in the active
// mode's unit (tui/SPEC.md §3). A pinned manualScale (Stage D.3) overrides
// the auto-ranged result entirely.
func (m Model) scale() float64 {
	if m.manualScale != nil {
		return *m.manualScale
	}
	var max float64
	for _, st := range m.snap.Models {
		if v := st.Value(m.mode); v > max {
			max = v
		}
	}
	return core.ScaleFor(max, m.mode)
}
