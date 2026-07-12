// Realtime LiveSource (tui/SPEC.md §7) — a Phoenix-channel WebSocket to
// Supabase Realtime carrying postgres_changes INSERT events on
// public.requests. This is the default source (ARCHITECTURE.md §5): a real
// request lights a bar in ~1–2s, versus the 20s worst case of the poll
// backup.
//
// Transport is github.com/nshafer/phx, a maintained Phoenix Channels client
// that owns the socket lifecycle — heartbeat, and (unlike the abandoned
// realtime-go v0.1.1 this replaced) working auto-reconnect with backoff. We
// drive the socket directly rather than through phx's Channel helper: phx's
// Channel join params are map[string]string, but Supabase's join needs a
// nested config object (postgres_changes[], access_token), so we send the
// phx_join ourselves in the OnOpen callback (which fires on the first connect
// AND every reconnect, giving us automatic rejoin) and read replies/changes
// off the raw OnMessage stream. phx logs through a pluggable Logger, so we
// point it at a no-op sink and never touch the alt-screen.
//
// Concurrency: phx invokes every callback on its own goroutine, so the
// callbacks funnel events into an internal channel and run() is the sole
// goroutine that sends on (and closes) the outbound channel — the same
// single-writer invariant poll.go relies on, which keeps the "close on ctx
// cancel" contract panic-free under the runtime source toggle.

package data

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/nshafer/phx"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// realtimeTopic is the Phoenix channel topic for public.requests, in
// Supabase's "realtime:{schema}:{table}" form.
const realtimeTopic = "realtime:public:requests"

// changeEvent is the server event name for a postgres_changes notification.
const changeEvent = "postgres_changes"

// Socket tuning (tui/SPEC.md §7): heartbeat keeps the connection alive, the
// connect timeout bounds a single dial, and reconnect backoff runs 1s → 30s.
const (
	rtHeartbeat      = 15 * time.Second
	rtConnectTimeout = 30 * time.Second
	rtInitialBackoff = 1 * time.Second
	rtMaxBackoff     = 30 * time.Second
)

// realtimeSource implements LiveSource over a phx Phoenix socket.
type realtimeSource struct {
	supabaseURL string
	anonKey     string
}

// newRealtimeSource builds a realtime source from the config. It can only
// fail when supabase_url is unparseable — already ruled out by config
// validation — so in practice the factory never falls back for realtime.
func newRealtimeSource(cfg Config) (*realtimeSource, error) {
	if _, err := realtimeEndpoint(cfg.SupabaseURL, cfg.SupabaseAnonKey); err != nil {
		return nil, err
	}
	return &realtimeSource{supabaseURL: cfg.SupabaseURL, anonKey: cfg.SupabaseAnonKey}, nil
}

// Name identifies the source in the status row.
func (r *realtimeSource) Name() string { return "realtime" }

// Start launches the socket goroutine and returns the event channel.
func (r *realtimeSource) Start(ctx context.Context) <-chan LiveEvent {
	out := make(chan LiveEvent, 16)
	go r.run(ctx, out)
	return out
}

// run owns the socket for the lifetime of ctx. phx handles connect,
// heartbeat, and reconnect; our callbacks translate its lifecycle into
// LiveEvents onto an internal channel, and this goroutine is the only one
// that forwards them to out (and closes it), so the toggle's ctx-cancel
// close can never race a callback send.
func (r *realtimeSource) run(ctx context.Context, out chan<- LiveEvent) {
	defer close(out)

	endpoint, err := realtimeEndpoint(r.supabaseURL, r.anonKey)
	if err != nil {
		send(ctx, out, LiveEvent{Kind: LiveConn, State: core.ConnOffline})
		return
	}

	// Callbacks fan into events; run() fans them out to the caller. events is
	// never closed, so a late callback after ctx-cancel just selects ctx.Done
	// and returns rather than panicking on a closed channel.
	events := make(chan LiveEvent, 16)
	emit := func(ev LiveEvent) {
		select {
		case events <- ev:
		case <-ctx.Done():
		}
	}

	socket := phx.NewSocket(endpoint)
	socket.Logger = phx.NewNoopLogger() // never stderr — the terminal is the UI
	socket.HeartbeatInterval = rtHeartbeat
	socket.ConnectTimeout = rtConnectTimeout
	socket.ReconnectAfterFunc = func(tries int) time.Duration {
		// Called on each failed dial. A brief blip reads as "reconnecting";
		// a sustained outage escalates to "offline" (tui/SPEC.md §7). phx
		// counts tries from process start, so this is best-effort — the
		// state self-corrects to live on the next successful rejoin.
		state := core.ConnReconnecting
		if tries >= pollOfflineAfter {
			state = core.ConnOffline
		}
		emit(LiveEvent{Kind: LiveConn, State: state})
		return rtBackoff(tries)
	}

	// joinRef ties our phx_join to its phx_reply. Written in OnOpen, read in
	// OnMessage — both phx goroutines — so it is guarded by an atomic.
	var joinRef atomic.Uint64

	socket.OnOpen(func() {
		// Fires on the first connect and on every reconnect: (re)join the
		// channel. join_ref == ref is the Phoenix convention for a join.
		ref := socket.MakeRef()
		joinRef.Store(uint64(ref))
		_ = socket.PushMessage(phx.Message{
			Topic:   realtimeTopic,
			Event:   string(phx.JoinEvent),
			Payload: joinPayload(r.anonKey),
			Ref:     ref,
			JoinRef: ref,
		})
	})
	socket.OnClose(func() {
		emit(LiveEvent{Kind: LiveConn, State: core.ConnReconnecting})
	})
	socket.OnError(func(error) {
		emit(LiveEvent{Kind: LiveConn, State: core.ConnReconnecting})
	})
	socket.OnMessage(func(msg phx.Message) {
		if msg.Topic != realtimeTopic {
			return
		}
		switch msg.Event {
		case string(phx.ReplyEvent):
			// The reply to our join: subscription is live. Signal a
			// (re)baseline exactly as the old path did (tui/SPEC.md §7).
			if uint64(msg.Ref) == joinRef.Load() && replyIsOK(msg.Payload) {
				emit(LiveEvent{Kind: LiveJoined, State: core.ConnLive})
			}
		case changeEvent:
			if row, ok := insertRow(msg.Payload); ok {
				emit(LiveEvent{Kind: LiveRow, Row: row})
			}
		}
	})

	if err := socket.Connect(); err != nil {
		send(ctx, out, LiveEvent{Kind: LiveConn, State: core.ConnOffline})
		return
	}
	defer socket.Disconnect() //nolint:errcheck // best-effort teardown

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-events:
			if !send(ctx, out, ev) {
				return
			}
		}
	}
}

// realtimeEndpoint builds the Supabase Realtime WebSocket URL from the
// project URL: wss scheme, the /realtime/v1 path (phx appends /websocket and
// the vsn query param itself), and the anon key as the apikey. Deriving from
// the configured host keeps this working for self-hosted Supabase, not just
// *.supabase.co.
func realtimeEndpoint(supabaseURL, anonKey string) (*url.URL, error) {
	u, err := url.Parse(supabaseURL)
	if err != nil || u.Host == "" {
		return nil, err
	}
	ws := *u
	ws.Scheme = "wss"
	ws.Path = "/realtime/v1"
	ws.RawQuery = url.Values{"apikey": {anonKey}}.Encode()
	return &ws, nil
}

// joinPayload is the Supabase phx_join body: subscribe to INSERTs on
// public.requests, no presence, and the anon key as the access_token so RLS
// on requests authorizes the subscriber.
func joinPayload(anonKey string) map[string]any {
	return map[string]any{
		"config": map[string]any{
			"postgres_changes": []map[string]any{
				{"event": "INSERT", "schema": "public", "table": "requests"},
			},
			"presence": map[string]any{"enabled": false},
			"private":  false,
		},
		"access_token": anonKey,
	}
}

// replyIsOK reports whether a phx_reply payload carries status "ok".
func replyIsOK(payload any) bool {
	m, ok := payload.(map[string]any)
	if !ok {
		return false
	}
	return m["status"] == "ok"
}

// insertRow decodes a postgres_changes INSERT payload into a live-stamped
// row. phx hands us the payload as decoded JSON (any), so we re-marshal it
// into the typed envelope; a non-INSERT or malformed body is dropped,
// mirroring the ingest parser's tolerance (tui/SPEC.md §8).
func insertRow(payload any) (core.RequestRow, bool) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return core.RequestRow{}, false
	}
	var p postgresChangesPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return core.RequestRow{}, false
	}
	if p.Data.Type != "INSERT" {
		return core.RequestRow{}, false
	}
	row, err := p.Data.Record.toRequestRow()
	if err != nil {
		return core.RequestRow{}, false
	}
	row.Live = true
	row.ReceivedAt = time.Now()
	return row, true
}

// rtBackoff returns the reconnect delay for the given attempt count:
// exponential from rtInitialBackoff, capped at rtMaxBackoff, plus up to 50%
// jitter so many clients don't retry in lockstep.
func rtBackoff(tries int) time.Duration {
	if tries < 1 {
		tries = 1
	}
	d := rtInitialBackoff
	for i := 1; i < tries && d < rtMaxBackoff; i++ {
		d *= 2
	}
	if d > rtMaxBackoff {
		d = rtMaxBackoff
	}
	return d + time.Duration(rand.Int63n(int64(d)/2+1))
}
