// The tea.Msg types the data layer feeds into Update. Bubble Tea has no
// data messages of its own here (Stage A handled only keys/mouse/resize);
// these carry the results of the fetches, the live feed, and the credits
// timers (tui/SPEC.md §7).

package ui

import (
	"time"

	"github.com/harjeet2020/burnbar/tui/internal/core"
	"github.com/harjeet2020/burnbar/tui/internal/data"
)

// liveStartedMsg delivers the LiveSource's event channel once Start has
// launched its goroutines. gen tags which source generation produced it, so
// a message from a source the runtime toggle has since replaced is ignored.
type liveStartedMsg struct {
	ch  <-chan data.LiveEvent
	gen int
}

// liveMsg is one event from the LiveSource (a join, a row, or a
// connection-state change). gen guards against a superseded source.
type liveMsg struct {
	ev  data.LiveEvent
	gen int
}

// liveClosedMsg signals the live channel closed (the source stopped). For the
// current generation that means the feed died (treated as offline); for a
// superseded generation it is the expected result of a toggle and ignored.
type liveClosedMsg struct {
	gen int
}

// baselineMsg carries the result of a usage_daily baseline fetch.
type baselineMsg struct {
	rows []core.DailyRow
	err  error
}

// todaySliceMsg carries the result of a today-slice (raw requests) fetch.
type todaySliceMsg struct {
	rows []core.RequestRow
	err  error
}

// baselineRetryMsg / sliceRetryMsg fire after a backoff to re-attempt a
// failed fetch (tui/SPEC.md §8).
type baselineRetryMsg struct{}
type sliceRetryMsg struct{}

// creditsMsg carries the result of a credits poll.
type creditsMsg struct {
	balance float64
	at      time.Time
	err     error
}

// creditTickMsg fires when a scheduled credits-debounce tick elapses; id
// lets the scheduler ignore ticks superseded by a push or merge.
type creditTickMsg struct {
	id int
}

// heartbeatMsg fires on the idle credits heartbeat; gen guards against
// stale heartbeats left over from a since-reset timer.
type heartbeatMsg struct {
	gen int
}
