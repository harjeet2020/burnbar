package core

import (
	"testing"
	"time"
)

// liveRow builds a live RequestRow with an explicit arrival and duration
// so the lag math is exercised precisely.
func liveRow(trace string, requestedAt time.Time, durationMS *int64, receivedAt time.Time) RequestRow {
	return RequestRow{
		TraceID:     trace,
		SpanID:      "s",
		Model:       "test/model",
		InputTokens: 100,
		RequestedAt: requestedAt,
		DurationMS:  durationMS,
		Live:        true,
		ReceivedAt:  receivedAt,
	}
}

func TestSnapshotMetaLastRequest(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, loc)

	t.Run("empty window yields the zero time", func(t *testing.T) {
		last, lag := SnapshotMeta(nil, TimeframeToday, now, loc)
		if !last.IsZero() {
			t.Errorf("lastRequestAt = %v, want zero", last)
		}
		if lag != nil {
			t.Errorf("lag = %v, want nil", *lag)
		}
	})

	t.Run("newest RequestedAt wins across fetched and live", func(t *testing.T) {
		older := time.Date(2026, 7, 6, 9, 0, 0, 0, loc)
		newer := time.Date(2026, 7, 6, 11, 30, 0, 0, loc)
		rows := []RequestRow{
			{TraceID: "a", RequestedAt: older, Live: false},
			{TraceID: "b", RequestedAt: newer, Live: true, ReceivedAt: newer.Add(time.Second)},
		}
		last, _ := SnapshotMeta(rows, TimeframeToday, now, loc)
		if !last.Equal(newer) {
			t.Errorf("lastRequestAt = %v, want %v", last, newer)
		}
	})

	t.Run("rows before the window start are ignored", func(t *testing.T) {
		// Yesterday, outside the local-today window.
		yesterday := time.Date(2026, 7, 5, 23, 0, 0, 0, loc)
		rows := []RequestRow{{TraceID: "a", RequestedAt: yesterday, Live: false}}
		last, _ := SnapshotMeta(rows, TimeframeToday, now, loc)
		if !last.IsZero() {
			t.Errorf("lastRequestAt = %v, want zero (row is out of window)", last)
		}
	})
}

func TestSnapshotMetaLag(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, loc)
	reqAt := time.Date(2026, 7, 6, 11, 59, 0, 0, loc)

	t.Run("lag is receivedAt minus concluded", func(t *testing.T) {
		// duration 3s, seen 2.1s after conclusion → lag 2.1s.
		concluded := reqAt.Add(3 * time.Second)
		rows := []RequestRow{liveRow("a", reqAt, i64(3000), concluded.Add(2100*time.Millisecond))}
		_, lag := SnapshotMeta(rows, TimeframeToday, now, loc)
		if lag == nil {
			t.Fatal("lag = nil, want ~2.1")
		}
		if got := *lag; got < 2.09 || got > 2.11 {
			t.Errorf("lag = %v, want ~2.1", got)
		}
	})

	t.Run("missing duration yields nil lag", func(t *testing.T) {
		rows := []RequestRow{liveRow("a", reqAt, nil, reqAt.Add(5*time.Second))}
		_, lag := SnapshotMeta(rows, TimeframeToday, now, loc)
		if lag != nil {
			t.Errorf("lag = %v, want nil (no duration)", *lag)
		}
	})

	t.Run("negative lag from clock skew clamps to nil", func(t *testing.T) {
		// Seen before the request even concluded → skew, clamp to nil.
		rows := []RequestRow{liveRow("a", reqAt, i64(10000), reqAt.Add(2*time.Second))}
		_, lag := SnapshotMeta(rows, TimeframeToday, now, loc)
		if lag != nil {
			t.Errorf("lag = %v, want nil (negative clamps)", *lag)
		}
	})

	t.Run("fetched-only rows produce no lag", func(t *testing.T) {
		rows := []RequestRow{{TraceID: "a", RequestedAt: reqAt, DurationMS: i64(1000), Live: false}}
		_, lag := SnapshotMeta(rows, TimeframeToday, now, loc)
		if lag != nil {
			t.Errorf("lag = %v, want nil (row is not live)", *lag)
		}
	})

	t.Run("lag follows the most recent live arrival", func(t *testing.T) {
		early := liveRow("a", reqAt, i64(1000), reqAt.Add(1*time.Second)) // lag 0s-ish
		lateReq := reqAt.Add(10 * time.Second)
		late := liveRow("b", lateReq, i64(2000), lateReq.Add(2*time.Second).Add(5*time.Second)) // lag 5s
		_, lag := SnapshotMeta([]RequestRow{early, late}, TimeframeToday, now, loc)
		if lag == nil || *lag < 4.9 || *lag > 5.1 {
			t.Errorf("lag = %v, want ~5 (newest arrival)", lag)
		}
	})
}

func TestLatestLive(t *testing.T) {
	base := time.Date(2026, 7, 6, 11, 0, 0, 0, time.UTC)

	t.Run("no live rows", func(t *testing.T) {
		rows := []RequestRow{{TraceID: "a", RequestedAt: base, Live: false}}
		if _, ok := LatestLive(rows); ok {
			t.Error("LatestLive found a row where none is live")
		}
	})

	t.Run("picks the latest arrival", func(t *testing.T) {
		rows := []RequestRow{
			liveRow("a", base, i64(0), base.Add(1*time.Second)),
			liveRow("b", base, i64(0), base.Add(9*time.Second)),
			liveRow("c", base, i64(0), base.Add(4*time.Second)),
		}
		got, ok := LatestLive(rows)
		if !ok || got.TraceID != "b" {
			t.Errorf("LatestLive = %q ok=%v, want b", got.TraceID, ok)
		}
	})
}
