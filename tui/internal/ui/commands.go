// tea.Cmd builders — the side-effecting bridge from the model to the data
// layer. Each returns a command Bubble Tea runs on its own goroutine,
// funnelling the result back as one of the messages.go types. Nothing
// here mutates the model (commands can't); they only start work.

package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
	"github.com/harjeet2020/burnbar/tui/internal/data"
)

// fetchTimeout bounds the baseline/today-slice fetches — generous for a
// cold link, short enough to fail visibly rather than hang.
const fetchTimeout = 20 * time.Second

// creditsFetchTimeout bounds a credits poll.
const creditsFetchTimeout = 12 * time.Second

// creditsHeartbeat is the idle re-poll cadence for spend from other
// machines (tui/SPEC.md §7).
const creditsHeartbeat = 5 * time.Minute

// Stage D live-UX timing constants (tui/SPEC.md §5/§6/§7). All "tune by
// feel" — sensible defaults now, adjust once the app has seen real use.
const (
	// accentEmphasis is how long a freshly-arrived burst renders bold —
	// the primary "a request landed" signal for tiny requests
	// (tui/SPEC.md §6).
	accentEmphasis = 1 * time.Second
	// relativeTick is the slow steady-state wakeup that refreshes relative
	// timestamps (tui/SPEC.md §6).
	relativeTick = 15 * time.Second
	// animFPS is the bar-animation frame rate; the tick loop runs at this
	// cadence ONLY while a spring is unsettled, then stops (tui/SPEC.md §6).
	animFPS = 30
)

// baselineBackoff / sliceBackoff are the retry delays after a failed
// fetch — fixed for Stage C; keeps stale data on screen meanwhile.
const (
	baselineBackoff = 5 * time.Second
	sliceBackoff    = 5 * time.Second
)

// startLiveCmd launches the LiveSource and returns its event channel, tagged
// with the current source generation so the toggle can discard a superseded
// source's late start.
func (m Model) startLiveCmd() tea.Cmd {
	live, ctx, gen := m.live, m.liveCtx, m.liveGen
	return func() tea.Msg {
		return liveStartedMsg{ch: live.Start(ctx), gen: gen}
	}
}

// waitLiveCmd blocks on the live channel for the next event, re-issued after
// each event to keep the feed flowing. A closed channel ends it. gen is
// echoed onto every message so a toggle-superseded source is ignored.
func waitLiveCmd(ch <-chan data.LiveEvent, gen int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return liveClosedMsg{gen: gen}
		}
		return liveMsg{ev: ev, gen: gen}
	}
}

// fetchBaselineCmd fetches the 31-day usage_daily baseline.
func (m Model) fetchBaselineCmd() tea.Cmd {
	rest := m.rest
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		rows, err := rest.FetchBaseline(ctx)
		return baselineMsg{rows: rows, err: err}
	}
}

// fetchTodaySliceCmd fetches the raw requests since local midnight — the
// exact local-today source (tui/SPEC.md §7).
func (m Model) fetchTodaySliceCmd() tea.Cmd {
	rest, loc := m.rest, m.loc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		since := core.LocalMidnight(time.Now(), loc)
		rows, err := rest.FetchTodaySlice(ctx, since)
		return todaySliceMsg{rows: rows, err: err}
	}
}

// fetchCreditsCmd polls the OpenRouter balance.
func (m Model) fetchCreditsCmd() tea.Cmd {
	credits := m.credits
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), creditsFetchTimeout)
		defer cancel()
		bal, err := credits.Fetch(ctx)
		return creditsMsg{balance: bal, at: time.Now(), err: err}
	}
}

// armCreditTick schedules a credits-debounce tick for the given action.
func armCreditTick(a data.Action) tea.Cmd {
	d := time.Until(a.ArmTickAt)
	if d < 0 {
		d = 0
	}
	id := a.ID
	return tea.Tick(d, func(time.Time) tea.Msg { return creditTickMsg{id: id} })
}

// armHeartbeat schedules the idle credits heartbeat, tagged with the
// current generation so a since-reset timer is ignored when it fires.
func armHeartbeat(gen int) tea.Cmd {
	return tea.Tick(creditsHeartbeat, func(time.Time) tea.Msg { return heartbeatMsg{gen: gen} })
}

// delayMsg emits msg after d — used to schedule fetch retries.
func delayMsg(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

// armRelativeTick schedules the next steady-state timestamp refresh
// (tui/SPEC.md §6). Re-armed by its own handler to run for the app's life.
func armRelativeTick() tea.Cmd {
	return tea.Tick(relativeTick, func(time.Time) tea.Msg { return relativeTickMsg{} })
}

// armLocalRollover schedules the today-window rollover for the next local
// midnight. Recomputed from now each time so DST shifts are handled for
// free (tui/SPEC.md §7).
func armLocalRollover(now time.Time, loc *time.Location) tea.Cmd {
	d := time.Until(core.NextLocalMidnight(now, loc))
	if d < 0 {
		d = 0
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return localRolloverMsg{} })
}

// armUTCRollover schedules the week/month slide for the next UTC midnight.
func armUTCRollover(now time.Time) tea.Cmd {
	d := time.Until(core.NextUTCMidnight(now))
	if d < 0 {
		d = 0
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return utcRolloverMsg{} })
}

// emphasisEndCmd schedules the redraw that drops a fresh burst's bold
// emphasis once accentEmphasis elapses (tui/SPEC.md §6).
func emphasisEndCmd() tea.Cmd {
	return tea.Tick(accentEmphasis, func(time.Time) tea.Msg { return emphasisEndMsg{} })
}

// truncateErr shortens an error for the one-line status badge.
func truncateErr(err error) string {
	const max = 60
	s := err.Error()
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
