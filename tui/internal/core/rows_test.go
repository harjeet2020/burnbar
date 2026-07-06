package core

import (
	"testing"
	"time"
)

// reqRow builds a minimal RequestRow for store tests; the store only
// cares about the key and the live metadata.
func reqRow(trace, span string, live bool, requestedAt time.Time) RequestRow {
	r := RequestRow{
		TraceID:     trace,
		SpanID:      span,
		Model:       "test/model",
		InputTokens: 100,
		RequestedAt: requestedAt,
		Live:        live,
	}
	if live {
		r.ReceivedAt = requestedAt.Add(2 * time.Second)
	}
	return r
}

func TestRequestRowTotalTokens(t *testing.T) {
	r := RequestRow{InputTokens: 1200, OutputTokens: 340}
	if got := r.TotalTokens(); got != 1540 {
		t.Errorf("TotalTokens() = %d, want 1540", got)
	}
}

func TestRowStoreUpsertDedupes(t *testing.T) {
	s := NewRowStore()
	at := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	if !s.Upsert(reqRow("t1", "s1", false, at)) {
		t.Fatal("first Upsert of a new key should report unseen (true)")
	}
	if s.Upsert(reqRow("t1", "s1", false, at)) {
		t.Error("duplicate key should report seen (false)")
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d after duplicate Upsert, want 1", s.Len())
	}
	// Same trace, different span is a distinct request.
	if !s.Upsert(reqRow("t1", "s2", false, at)) {
		t.Error("same trace with a new span should be unseen (true)")
	}
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}
}

func TestRowStoreLiveWinsMerge(t *testing.T) {
	at := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	t.Run("live then fetched stays live", func(t *testing.T) {
		s := NewRowStore()
		live := reqRow("t1", "s1", true, at)
		s.Upsert(live)
		if s.Upsert(reqRow("t1", "s1", false, at)) {
			t.Error("fetched duplicate should report seen (false)")
		}
		got := s.Rows()[0]
		if !got.Live {
			t.Error("fetched row overwrote the live row; live must win")
		}
		if !got.ReceivedAt.Equal(live.ReceivedAt) {
			t.Errorf("ReceivedAt = %v, want the live row's %v", got.ReceivedAt, live.ReceivedAt)
		}
	})

	t.Run("fetched then live upgrades to live", func(t *testing.T) {
		s := NewRowStore()
		s.Upsert(reqRow("t1", "s1", false, at))
		live := reqRow("t1", "s1", true, at)
		if s.Upsert(live) {
			t.Error("live duplicate should still report seen (false)")
		}
		got := s.Rows()[0]
		if !got.Live {
			t.Error("live row must upgrade a previously fetched row")
		}
		if !got.ReceivedAt.Equal(live.ReceivedAt) {
			t.Errorf("ReceivedAt = %v, want the live row's %v", got.ReceivedAt, live.ReceivedAt)
		}
		if s.Len() != 1 {
			t.Errorf("Len() = %d, want 1", s.Len())
		}
	})
}

func TestRowStoreClears(t *testing.T) {
	at := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	fill := func() *RowStore {
		s := NewRowStore()
		s.Upsert(reqRow("tf", "s1", false, at))
		s.Upsert(reqRow("tl", "s1", true, at))
		return s
	}

	t.Run("ClearLive keeps fetched rows", func(t *testing.T) {
		s := fill()
		s.ClearLive()
		rows := s.Rows()
		if len(rows) != 1 || rows[0].Live {
			t.Errorf("after ClearLive rows = %+v, want the single fetched row", rows)
		}
	})

	t.Run("ClearFetched keeps live rows", func(t *testing.T) {
		s := fill()
		s.ClearFetched()
		rows := s.Rows()
		if len(rows) != 1 || !rows[0].Live {
			t.Errorf("after ClearFetched rows = %+v, want the single live row", rows)
		}
	})

	t.Run("Clear empties the store", func(t *testing.T) {
		s := fill()
		s.Clear()
		if s.Len() != 0 {
			t.Errorf("Len() = %d after Clear, want 0", s.Len())
		}
	})
}

func TestRowStoreRowsDeterministicOrder(t *testing.T) {
	base := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	// Insert in scrambled order; two rows share a RequestedAt so the
	// (TraceID, SpanID) tiebreak is exercised too.
	rows := []RequestRow{
		reqRow("tb", "s1", false, base.Add(time.Minute)),
		reqRow("ta", "s2", false, base),
		reqRow("ta", "s1", false, base),
		reqRow("tc", "s1", true, base.Add(2*time.Minute)),
	}
	want := []string{"ta/s1", "ta/s2", "tb/s1", "tc/s1"}

	// Two independently filled stores must agree despite Go's map
	// iteration randomization.
	for run := 0; run < 2; run++ {
		s := NewRowStore()
		for _, r := range rows {
			s.Upsert(r)
		}
		got := s.Rows()
		if len(got) != len(want) {
			t.Fatalf("run %d: len = %d, want %d", run, len(got), len(want))
		}
		for i, r := range got {
			if key := r.TraceID + "/" + r.SpanID; key != want[i] {
				t.Errorf("run %d: Rows()[%d] = %s, want %s", run, i, key, want[i])
			}
		}
	}
}
