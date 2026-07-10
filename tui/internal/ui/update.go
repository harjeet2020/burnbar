// Message handling — the only place state changes. Keyboard is primary;
// every mouse action mirrors a key binding (tui/SPEC.md §2). All hit-
// testing goes through the same computeLayout the view renders from.

package ui

import (
	"context"
	"errors"
	"log"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
	"github.com/harjeet2020/burnbar/tui/internal/data"
)

// Update routes messages to the focused handler. Input handlers are pure
// state transitions; the data handlers fold fetch/live results into the
// stores and re-derive the snapshot (tui/SPEC.md §7). I/O only ever
// happens inside the tea.Cmds these return, never inline.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// All layout is re-derived from these on the next render — a
		// resize snaps, never animates (tui/SPEC.md §4).
		m.width, m.height = msg.Width, msg.Height
		m.scroll = m.currentLayout().clampScroll(m.scroll)
		// Bars snap to the new width — a resize must feel like the terminal
		// is in charge, never animated (tui/SPEC.md §4/§6).
		m = m.snapBars()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)

	// --- Data layer (Stage C) -------------------------------------------
	case liveStartedMsg:
		if msg.gen != m.liveGen {
			return m, nil // a source the toggle has since replaced
		}
		m.liveCh = msg.ch
		return m, waitLiveCmd(m.liveCh, m.liveGen)
	case liveMsg:
		if msg.gen != m.liveGen {
			return m, nil
		}
		return m.handleLive(msg.ev)
	case liveClosedMsg:
		if msg.gen != m.liveGen {
			return m, nil // the old source we intentionally stopped on toggle
		}
		m.conn = core.ConnOffline
		return m.rebuilt(time.Now()), nil
	case baselineMsg:
		return m.handleBaseline(msg)
	case todaySliceMsg:
		return m.handleTodaySlice(msg)
	case baselineRetryMsg:
		return m, m.fetchBaselineCmd()
	case sliceRetryMsg:
		return m, m.fetchTodaySliceCmd()
	case creditsMsg:
		return m.handleCredits(msg)
	case creditTickMsg:
		return m.handleCreditTick(msg)
	case heartbeatMsg:
		if msg.gen != m.heartbeatGen || m.credits == nil {
			return m, nil // a since-reset heartbeat, or credits disabled
		}
		return m, m.fetchCreditsCmd()
	case relativeTickMsg:
		// Re-derive so relative timestamps refresh (tui/SPEC.md §6), then
		// re-arm — the sole steady-state wakeup.
		return m.rebuilt(time.Now()), armRelativeTick()
	case localRolloverMsg:
		// Local midnight: drop the stale today-slice, refetch it, and
		// reschedule (tui/SPEC.md §7). Week/month are untouched.
		now := time.Now()
		m.rows.ClearFetched()
		m = m.rebuilt(now)
		return m.withAnim(tea.Batch(m.fetchTodaySliceCmd(), armLocalRollover(now, m.loc)))
	case utcRolloverMsg:
		// UTC midnight: week/month day boundaries shifted — re-aggregate
		// (no refetch) and reschedule (tui/SPEC.md §7).
		now := time.Now()
		return m.rebuilt(now).withAnim(armUTCRollover(now))
	case animTickMsg:
		// One frame: advance every spring; when all have settled, stop the
		// loop and go back to 0 fps (tui/SPEC.md §6).
		var settled bool
		m, settled = m.stepAnim()
		if settled {
			m.animating = false
			log.Printf("anim: loop stop (settled)")
			return m, nil
		}
		return m, animTickCmd()
	case emphasisEndMsg:
		// No state change needed — the bold check reads the clock; returning
		// forces the redraw that drops it (tui/SPEC.md §6).
		return m, nil
	}
	return m, nil
}

// handleLive folds one live event into the model (tui/SPEC.md §7). A join
// re-baselines (clearing stale live rows first); a row is deduped in and
// nudges the credits debounce; a bare connection change updates the chip.
// Every branch re-arms waitLiveCmd to keep the feed flowing.
func (m Model) handleLive(ev data.LiveEvent) (tea.Model, tea.Cmd) {
	switch ev.Kind {
	case data.LiveJoined:
		m.conn = ev.State
		// Missed events while disconnected are healed by re-baselining,
		// so stale live rows go and the baseline+slice are refetched.
		m.rows.ClearLive()
		m = m.rebuilt(time.Now())
		return m.withAnim(tea.Batch(m.fetchBaselineCmd(), m.fetchTodaySliceCmd(), waitLiveCmd(m.liveCh, m.liveGen)))
	case data.LiveRow:
		now := time.Now()
		isNew := m.rows.Upsert(ev.Row)
		m = m.rebuilt(now)
		cmds := []tea.Cmd{waitLiveCmd(m.liveCh, m.liveGen)}
		cmds = append(cmds, m.onCreditEvent(now)...)
		// A genuinely-new arrival is the newest live row → the latest
		// burst; flash it bold for accentEmphasis (tui/SPEC.md §6). The
		// one-shot end command guarantees a redraw drops the bold even if
		// the bar animation has already settled (no tick loop otherwise).
		if isNew {
			m.accentEmphasisUntil = now.Add(accentEmphasis)
			cmds = append(cmds, emphasisEndCmd())
		}
		return m.withAnim(tea.Batch(cmds...))
	default: // data.LiveConn
		m.conn = ev.State
		return m.rebuilt(time.Now()), waitLiveCmd(m.liveCh, m.liveGen)
	}
}

// handleBaseline stores a fresh baseline (or keeps the stale one and
// schedules a retry on failure — tui/SPEC.md §8).
func (m Model) handleBaseline(msg baselineMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.dataErr = truncateErr(msg.err)
		return m, delayMsg(baselineBackoff, baselineRetryMsg{})
	}
	m.baseline = msg.rows
	m.loading = false
	m.dataErr = ""
	m.dataAt = time.Now()
	return m.rebuilt(time.Now()).withAnim(nil)
}

// handleTodaySlice folds the fetched raw rows into the store (as fetched,
// not live) or schedules a retry.
func (m Model) handleTodaySlice(msg todaySliceMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.dataErr = truncateErr(msg.err)
		return m, delayMsg(sliceBackoff, sliceRetryMsg{})
	}
	for _, r := range msg.rows {
		m.rows.Upsert(r)
	}
	m.loading = false
	m.dataErr = ""
	return m.rebuilt(time.Now()).withAnim(nil)
}

// handleCredits folds a credits result in and re-arms the idle heartbeat
// (which resets on any completed poll, success or failure — tui/SPEC.md §7).
func (m Model) handleCredits(msg creditsMsg) (tea.Model, tea.Cmd) {
	m.heartbeatGen++
	hb := armHeartbeat(m.heartbeatGen)

	if msg.err != nil {
		if errors.Is(msg.err, data.ErrCreditsUnauthorized) {
			m.creditsHint = "check openrouter_api_key"
		} else {
			m.creditsHint = "credits unavailable"
		}
		// Keep the last value with its growing age (tui/SPEC.md §7/§8).
		return m.rebuilt(time.Now()), hb
	}
	bal := msg.balance
	m.creditsVal = &bal
	m.creditsAt = msg.at
	m.creditsHint = ""
	return m.rebuilt(time.Now()), hb
}

// handleCreditTick fires a credits poll if the tick is still current
// (not superseded by a push or merge — the scheduler decides).
func (m Model) handleCreditTick(msg creditTickMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, a := range m.sched.OnTick(msg.id, time.Now()) {
		if a.PollNow {
			cmds = append(cmds, m.fetchCreditsCmd())
		}
	}
	return m, tea.Batch(cmds...)
}

// onCreditEvent runs the burst debounce for a broadcast event and turns
// its actions into commands (a poll and/or a re-armed tick). No-op when
// credits are disabled.
func (m Model) onCreditEvent(now time.Time) []tea.Cmd {
	if m.credits == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, a := range m.sched.OnEvent(now) {
		if a.PollNow {
			cmds = append(cmds, m.fetchCreditsCmd())
		}
		if !a.ArmTickAt.IsZero() {
			cmds = append(cmds, armCreditTick(a))
		}
	}
	return cmds
}

// currentLayout resolves geometry for the current size and model count.
func (m Model) currentLayout() layout {
	return computeLayout(m.width, m.height, len(m.snap.Models))
}

// handleKey dispatches key presses through the central keymap.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys

	// Quit and suspend always work, whatever is on screen (tui/SPEC.md §8).
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Suspend):
		return m, tea.Suspend
	}

	// The help overlay captures everything else while open.
	if m.showHelp {
		if key.Matches(msg, k.Help) || key.Matches(msg, k.Back) {
			m.showHelp = false
		}
		return m, nil
	}

	// The recent-request modal captures everything else while open
	// (tui/SPEC.md §2 Stage D.4), mirroring showHelp above.
	if m.showModal {
		if key.Matches(msg, k.Modal) || key.Matches(msg, k.Back) {
			m.showModal = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, k.Help):
		m.showHelp = true
	case key.Matches(msg, k.Modal):
		m.showModal = true
	case key.Matches(msg, k.Timeframe):
		// A window switch re-cuts the data → animate the new bar lengths.
		return m.setTimeframe(m.tf.Next()).withAnim(nil)
	case key.Matches(msg, k.Refresh):
		return m.refresh()
	case key.Matches(msg, k.ToggleSource):
		return m.toggleSource()
	case m.scr == screenMeter && key.Matches(msg, k.Mode):
		// Mode toggle re-cuts what drives length/sort/scale → animate the
		// new bar lengths, same as a window switch (tui/SPEC.md §3). It
		// also re-baselines the view, so a pinned manual zoom resets.
		m.mode = m.mode.Toggle()
		m.manualScale = nil
		return m.rebuilt(time.Now()).withAnim(nil)
	case m.scr == screenMeter && key.Matches(msg, k.ZoomOut):
		// `-` zooms out: next larger rung on the shared ladder (tui/SPEC.md
		// §3 manual zoom, Stage D.3).
		s := core.NextScale(m.scale(), 1, m.mode)
		m.manualScale = &s
		return m.withAnim(nil)
	case m.scr == screenMeter && key.Matches(msg, k.ZoomIn):
		// `+`/`=` zooms in: next smaller rung; bars over the new scale
		// clamp to full width rather than the ladder re-expanding.
		s := core.NextScale(m.scale(), -1, m.mode)
		m.manualScale = &s
		return m.withAnim(nil)
	case m.scr == screenMeter && key.Matches(msg, k.ZoomReset):
		m.manualScale = nil
		return m.withAnim(nil)
	case m.scr == screenMeter && key.Matches(msg, k.Up):
		m = m.moveSelection(-1)
	case m.scr == screenMeter && key.Matches(msg, k.Down):
		m = m.moveSelection(1)
	case m.scr == screenMeter && key.Matches(msg, k.Details):
		if m.selectedIndex() >= 0 {
			m.scr = screenDetails
		}
	case m.scr == screenDetails && key.Matches(msg, k.Back):
		m.scr = screenMeter
	}
	return m, nil
}

// setTimeframe switches the window: pure client-side re-aggregation —
// instant, no refetch (tui/SPEC.md §7). rebuilt handles the selection and
// scroll repair for the new model list. A window switch re-baselines the
// view, so a pinned manual zoom resets (tui/SPEC.md §3 Stage D.3).
func (m Model) setTimeframe(tf core.Timeframe) Model {
	m.tf = tf
	m.manualScale = nil
	return m.rebuilt(time.Now())
}

// refresh re-baselines the world (tui/SPEC.md §5/§7): drop all rows
// (clearing the highlight, since LatestBurst has nothing left to find),
// then refetch the baseline, today-slice, and credits. Stale data stays
// on screen until the fresh data lands. A manual zoom resets along with
// everything else this re-baselines (tui/SPEC.md §3 Stage D.3).
func (m Model) refresh() (tea.Model, tea.Cmd) {
	m.rows.Clear()
	m.accentEmphasisUntil = time.Time{}
	m.manualScale = nil
	m = m.rebuilt(time.Now())

	cmds := []tea.Cmd{m.fetchBaselineCmd(), m.fetchTodaySliceCmd()}
	if m.credits != nil {
		for _, a := range m.sched.OnManualRefresh(time.Now()) {
			if a.PollNow {
				cmds = append(cmds, m.fetchCreditsCmd())
			}
		}
	}
	return m.withAnim(tea.Batch(cmds...))
}

// toggleSource swaps the live source between realtime and poll at runtime
// (tui/SPEC.md §7). It stops the running source, builds a fresh one for the
// flipped kind — Start is once-only, so a new instance is mandatory — and
// bumps the generation so the stopped source's trailing events (including
// the liveClosedMsg its cancellation triggers) are ignored. The new source's
// first LiveJoined re-baselines, so no manual refetch is needed here.
func (m Model) toggleSource() (tea.Model, tea.Cmd) {
	m.liveCancel()

	if m.cfg.LiveSource == data.LiveSourcePoll {
		m.cfg.LiveSource = data.LiveSourceRealtime
	} else {
		m.cfg.LiveSource = data.LiveSourcePoll
	}
	m.live, _ = data.NewLiveSource(m.cfg, m.rest)
	m.liveGen++

	ctx, cancel := context.WithCancel(context.Background())
	m.liveCtx, m.liveCancel = ctx, cancel

	// Reflect the switch immediately; the new source confirms health on its
	// first join.
	m.conn = initialConn(m.live)
	return m.rebuilt(time.Now()), m.startLiveCmd()
}

// moveSelection moves the selection by delta rows, clamped, and keeps it
// on screen.
func (m Model) moveSelection(delta int) Model {
	n := len(m.snap.Models)
	if n == 0 {
		return m
	}
	idx := m.selectedIndex()
	if idx < 0 {
		idx = 0
	} else {
		idx += delta
	}
	if idx < 0 {
		idx = 0
	}
	if idx > n-1 {
		idx = n - 1
	}
	m.selected = m.snap.Models[idx].Name
	return m.ensureSelectionVisible()
}

// ensureSelectionVisible adjusts the scroll offset so the selected block
// is on screen when the list scrolls.
func (m Model) ensureSelectionVisible() Model {
	l := m.currentLayout()
	if !l.scrolling {
		m.scroll = 0
		return m
	}
	idx := m.selectedIndex()
	if idx < 0 {
		return m
	}
	if idx < m.scroll {
		m.scroll = idx
	}
	if idx >= m.scroll+l.visible {
		m.scroll = idx - l.visible + 1
	}
	m.scroll = l.clampScroll(m.scroll)
	return m
}

// handleClick implements the mouse augmentation (tui/SPEC.md §2): click a
// timeframe label to activate it, click a bar to select, click the
// selected bar (or double-click — the second click lands on an already-
// selected bar) to open details.
func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}
	if m.showModal {
		m.showModal = false
		return m, nil
	}

	// Timeframe labels live on header row 2 (y=2, after the D.1 spacer row)
	// on every screen.
	if msg.Y == 2 {
		_, ranges := m.timeframeSelector()
		for _, r := range ranges {
			if msg.X >= r.x0 && msg.X < r.x1 {
				return m.setTimeframe(r.tf).withAnim(nil)
			}
		}
		return m, nil
	}

	if m.scr != screenMeter {
		return m, nil
	}

	l := m.currentLayout()
	idx := l.blockAt(msg.Y, l.clampScroll(m.scroll), len(m.snap.Models))
	if idx < 0 {
		return m, nil
	}
	name := m.snap.Models[idx].Name
	if name == m.selected {
		m.scr = screenDetails
		return m, nil
	}
	m.selected = name
	m = m.ensureSelectionVisible()
	return m, nil
}

// handleWheel scrolls the bars list when it is in scroll mode — the wheel
// moves the viewport, not the selection (tui/SPEC.md §2).
func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.scr != screenMeter || m.showHelp || m.showModal {
		return m, nil
	}
	l := m.currentLayout()
	if !l.scrolling {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		m.scroll = l.clampScroll(m.scroll - 1)
	case tea.MouseWheelDown:
		m.scroll = l.clampScroll(m.scroll + 1)
	}
	return m, nil
}
