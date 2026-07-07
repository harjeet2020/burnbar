// The live layer (tui/SPEC.md §7). A LiveSource delivers request-row
// events and connection-state changes over a single channel; the UI reads
// them as tea.Msgs. Two implementations sit behind the interface — the
// realtime WebSocket (default, a phx Phoenix socket to Supabase Realtime)
// and the 20s PostgREST poll (a manual backup, toggled at runtime). Choosing
// between them is a one-line factory change plus the `live_source` config
// override.

package data

import (
	"context"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// LiveKind tags what a LiveEvent carries.
type LiveKind int

const (
	// LiveJoined is emitted on the first successful subscribe/connect and
	// on every rejoin. It carries the healthy State (ConnLive or
	// ConnPolling) and tells the UI to (re)fetch the baseline + today-slice
	// and clear stale live rows — the "subscribe before fetch" and
	// "re-baseline on rejoin" contracts (tui/SPEC.md §7).
	LiveJoined LiveKind = iota
	// LiveRow is one request INSERT. Row is live (flagged, stamped).
	LiveRow
	// LiveConn is a connection-state change with no data — drives the
	// status chip (reconnecting/offline/recovered).
	LiveConn
)

// LiveEvent is one message from a LiveSource.
type LiveEvent struct {
	// Kind selects which fields are meaningful.
	Kind LiveKind
	// Row is set when Kind == LiveRow.
	Row core.RequestRow
	// State is set when Kind == LiveJoined or LiveConn.
	State core.ConnState
}

// LiveSource is a running feed of live events. Start launches the feed's
// goroutines and returns the read channel; the feed runs until ctx is
// cancelled, after which the channel is closed.
type LiveSource interface {
	// Start begins delivering events and returns the channel to read them
	// from. It must be called at most once.
	Start(ctx context.Context) <-chan LiveEvent
	// Name reports "realtime" or "polling" for the status row and logs.
	Name() string
}

// NewLiveSource builds the configured LiveSource. It falls back to polling
// when realtime is requested but cannot be constructed from this project
// URL — the status row then honestly shows "polling", which beats failing to
// start. The returned bool reports whether that fallback happened, so the
// caller can log it. (Realtime construction only fails on an unparseable
// supabase_url, which config validation already rejects, so the fallback is
// a defensive backstop.)
func NewLiveSource(cfg Config, rest *RESTClient) (src LiveSource, fellBack bool) {
	if cfg.LiveSource == LiveSourcePoll {
		return newPollSource(rest), false
	}
	rt, err := newRealtimeSource(cfg)
	if err != nil {
		return newPollSource(rest), true
	}
	return rt, false
}

// send delivers an event unless ctx is cancelled first; the bool reports
// whether the send happened (false means the source should stop).
func send(ctx context.Context, out chan<- LiveEvent, ev LiveEvent) bool {
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
