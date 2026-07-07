// Realtime LiveSource (tui/SPEC.md §7) — an opt-in Phoenix-channel WebSocket
// to Supabase Realtime carrying postgres_changes INSERT events on
// public.requests. NOT the default: the live go/no-go spike confirmed
// realtime-go v0.1.1 cannot recover from a dropped socket (see below), so
// live_source defaults to "poll" (root SPEC §6). This path is kept for when
// the library is fixed or replaced; select it with live_source="realtime".
//
// Confirmed no-go — realtime-go v0.1.1 has two disqualifying defects:
//   - Self-deadlocking reconnect: on any socket close its handleMessages()
//     calls reconnect(), which sets isReconnecting=true and then loops
//     calling Connect() — but Connect() rejects with "client is already
//     reconnecting" whenever that flag is set. So every retry fails
//     instantly and it gives up after MaxRetries, permanently. Recovery is
//     structurally impossible and cannot be fixed through the public API.
//   - It logs through log.Default() (stderr); in an alt-screen TUI that
//     paints over the UI. We neutralize this defensively by discarding the
//     standard logger in main(), but the broken reconnect remains.
//
// The happy path — connect, receive INSERTs, reconnect on connect failure
// with backoff — is implemented here and works until the first drop.

package data

import (
	"context"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/supabase-community/realtime-go/realtime"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// realtimeTopic is the channel topic for public.requests. realtime-go
// expects the "realtime:{schema}:{table}" form.
const realtimeTopic = "realtime:public:requests"

// Backoff bounds for our own connect-retry loop (tui/SPEC.md §7: 1s → 30s
// cap, jittered).
const (
	rtInitialBackoff = 1 * time.Second
	rtMaxBackoff     = 30 * time.Second
	rtConnectTimeout = 30 * time.Second
)

// realtimeSource implements LiveSource over realtime-go.
type realtimeSource struct {
	ref     string
	anonKey string
}

// newRealtimeSource builds a realtime source, deriving the project ref
// from the config URL. Errors when the host isn't addressable by
// realtime-go (the factory then falls back to polling).
func newRealtimeSource(cfg Config) (*realtimeSource, error) {
	ref, err := projectRef(cfg.SupabaseURL)
	if err != nil {
		return nil, err
	}
	return &realtimeSource{ref: ref, anonKey: cfg.SupabaseAnonKey}, nil
}

// Name identifies the source in the status row.
func (r *realtimeSource) Name() string { return "realtime" }

// Start launches the supervised connect loop and returns the channel.
func (r *realtimeSource) Start(ctx context.Context) <-chan LiveEvent {
	out := make(chan LiveEvent, 16)
	go r.run(ctx, out)
	return out
}

// run supervises the connection: connect, subscribe (emitting LiveJoined),
// and stay up until ctx ends. On a connect/subscribe failure it emits a
// reconnecting/offline state and retries with jittered backoff.
func (r *realtimeSource) run(ctx context.Context, out chan<- LiveEvent) {
	defer close(out)

	backoff := rtInitialBackoff
	attempts := 0
	for {
		if ctx.Err() != nil {
			return
		}
		if r.connectOnce(ctx, out) {
			// connectOnce blocks until ctx is done once subscribed.
			return
		}
		attempts++
		state := core.ConnReconnecting
		if attempts >= pollOfflineAfter {
			state = core.ConnOffline
		}
		if !send(ctx, out, LiveEvent{Kind: LiveConn, State: state}) {
			return
		}
		if !sleepWithJitter(ctx, backoff) {
			return
		}
		if backoff *= 2; backoff > rtMaxBackoff {
			backoff = rtMaxBackoff
		}
	}
}

// connectOnce runs one connection lifecycle. It returns true when the
// context ended (a clean stop — the caller should exit) and false when the
// attempt failed before/at subscribe (the caller should back off and
// retry).
func (r *realtimeSource) connectOnce(ctx context.Context, out chan<- LiveEvent) (done bool) {
	client := realtime.NewRealtimeClient(r.ref, r.anonKey)
	// SetAuth adds the Authorization: Bearer header alongside apikey so
	// RLS on requests authorizes the anon subscriber.
	_ = client.SetAuth(r.anonKey)

	channel := client.Channel(realtimeTopic, &realtime.ChannelConfig{})
	if err := channel.OnPostgresChange("INSERT", func(change realtime.PostgresChangeEvent) {
		r.emitRow(ctx, out, change)
	}); err != nil {
		return false
	}

	connCtx, cancel := context.WithTimeout(ctx, rtConnectTimeout)
	err := client.Connect(connCtx)
	cancel()
	if err != nil {
		return false
	}

	subErr := channel.Subscribe(ctx, func(state realtime.SubscribeState, e error) {
		if e == nil && state == realtime.SubscribeStateSubscribed {
			// Subscribe fires this optimistically once the join is sent;
			// it's our signal to (re)baseline (tui/SPEC.md §7).
			send(ctx, out, LiveEvent{Kind: LiveJoined, State: core.ConnLive})
		}
	})
	if subErr != nil {
		_ = client.Disconnect()
		return false
	}

	// Stay connected until the app exits. The library owns socket-level
	// heartbeat and (best-effort) reconnect from here; mid-session
	// recovery is the spike's concern (see the file header).
	<-ctx.Done()
	_ = client.Disconnect()
	return true
}

// emitRow decodes an INSERT payload's new row and forwards it as a live
// event. A malformed payload is dropped (logged upstream via the debug
// path), mirroring the ingest parser's tolerance (tui/SPEC.md §8).
func (r *realtimeSource) emitRow(ctx context.Context, out chan<- LiveEvent, change realtime.PostgresChangeEvent) {
	var payload realtimeInsertPayload
	if err := json.Unmarshal(change.Payload, &payload); err != nil {
		return
	}
	row, err := payload.Record.toRequestRow()
	if err != nil {
		return
	}
	row.Live = true
	row.ReceivedAt = time.Now()
	send(ctx, out, LiveEvent{Kind: LiveRow, Row: row})
}

// sleepWithJitter waits for d ± up to 50% jitter, or returns false if ctx
// ends first.
func sleepWithJitter(ctx context.Context, d time.Duration) bool {
	jitter := time.Duration(rand.Int63n(int64(d)/2 + 1))
	select {
	case <-time.After(d + jitter):
		return true
	case <-ctx.Done():
		return false
	}
}
