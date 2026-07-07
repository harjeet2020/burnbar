// Polling LiveSource (tui/SPEC.md §7) — the manual backup that walks the
// requests table by inserted_at every 20s. Built on nothing but net/http, so
// it is the robust fallback when WebSockets are unavailable; the user selects
// it via live_source="poll" or toggles to it at runtime with `p`. The 20s
// cadence trades latency for a light footprint appropriate to a backup.

package data

import (
	"context"
	"time"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// pollInterval is the cursor-query cadence (tui/SPEC.md §7).
const pollInterval = 20 * time.Second

// pollOfflineAfter is how many consecutive failures flip the status from
// "reconnecting" to "offline" — shared by both live sources so the meaning
// stays consistent (realtime reuses it for reconnect-attempt escalation).
const pollOfflineAfter = 5

// pollSource implements LiveSource over PostgREST cursor queries.
type pollSource struct {
	rest *RESTClient
}

// newPollSource builds a polling source over the shared REST client.
func newPollSource(rest *RESTClient) *pollSource { return &pollSource{rest: rest} }

// Name identifies the source in the status row.
func (p *pollSource) Name() string { return "polling" }

// Start launches the poll loop and returns the event channel.
func (p *pollSource) Start(ctx context.Context) <-chan LiveEvent {
	out := make(chan LiveEvent, 16)
	go p.run(ctx, out)
	return out
}

// run polls until ctx is cancelled. The cursor starts at "now" so the
// first poll overlaps the baseline/today-slice fetch (subscribe before
// fetch); the RowStore dedupe absorbs the overlap (tui/SPEC.md §7).
func (p *pollSource) run(ctx context.Context, out chan<- LiveEvent) {
	defer close(out)

	cursor := time.Now()
	if !send(ctx, out, LiveEvent{Kind: LiveJoined, State: core.ConnPolling}) {
		return
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	fails := 0
	degraded := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := p.rest.FetchSince(ctx, cursor)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				fails++
				state := core.ConnReconnecting
				if fails >= pollOfflineAfter {
					state = core.ConnOffline
				}
				degraded = true
				if !send(ctx, out, LiveEvent{Kind: LiveConn, State: state}) {
					return
				}
				continue
			}
			if degraded {
				// Recovered: back to healthy polling.
				degraded = false
				if !send(ctx, out, LiveEvent{Kind: LiveConn, State: core.ConnPolling}) {
					return
				}
			}
			fails = 0
			for _, r := range rows {
				if !send(ctx, out, LiveEvent{Kind: LiveRow, Row: r}) {
					return
				}
				if r.InsertedAt.After(cursor) {
					cursor = r.InsertedAt
				}
			}
		}
	}
}
