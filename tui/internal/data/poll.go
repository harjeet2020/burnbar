// Polling LiveSource (tui/SPEC.md §7) — the pre-approved fallback that
// walks the requests table by inserted_at every 2s. Same rows and latency
// budget as realtime, just chattier, and built on nothing but net/http,
// so it is the robust default when WebSockets are unavailable or the
// pre-v1 realtime SDK proves unreliable.

package data

import (
	"context"
	"time"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// pollInterval is the cursor-query cadence (tui/SPEC.md §7).
const pollInterval = 2 * time.Second

// pollOfflineAfter is how many consecutive failed polls flip the status
// from "reconnecting" to "offline". At 2s a poll, ~10s of failures reads
// as a real outage rather than a blip.
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
