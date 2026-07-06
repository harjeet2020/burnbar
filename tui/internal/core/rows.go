// Raw data-row types mirroring the two SQL read surfaces (requests and
// usage_daily — see supabase/migrations/*_create_requests.sql), and the
// (trace_id, span_id) dedupe store the frontends layer live events into
// (tui/SPEC.md §7).
//
// NULL vs 0 (root SPEC §2): nullable SQL columns are pointer fields —
// nil means "the payload did not report this attribute", a pointed-to
// zero means "reported as zero". Derived stats never conflate the two.

package core

import (
	"sort"
	"time"
)

// RequestRow is one raw `requests` row: a single OpenRouter Broadcast
// span, from either the today-slice fetch or a live event. Both sources
// share this shape so the dedupe store can merge them (tui/SPEC.md §7).
type RequestRow struct {
	// TraceID and SpanID form the dedupe key — unique in the database
	// and in RowStore.
	TraceID string
	SpanID  string
	// Model is the UI grouping key (alias slug, e.g.
	// "deepseek/deepseek-v4-flash").
	Model string
	// Author, Provider, and ProviderSlug describe routing; nil when
	// unreported. ProviderSlug includes the deployment variant
	// ("novita/fp8") and is the details-screen split key.
	Author       *string
	Provider     *string
	ProviderSlug *string
	// InputTokens and OutputTokens are not-null in SQL (default 0);
	// OutputTokens includes reasoning tokens.
	InputTokens  int64
	OutputTokens int64
	// CachedTokens, ReasoningTokens, and DurationMS are nullable facts.
	CachedTokens    *int64
	ReasoningTokens *int64
	DurationMS      *int64
	// CostUSD is the total request cost (not-null, default 0).
	CostUSD float64
	// Split costs and quoted unit prices; nil when unreported. The
	// split costs embody cache discounts (tui/SPEC.md §3).
	InputCostUSD    *float64
	OutputCostUSD   *float64
	InputUnitPrice  *float64
	OutputUnitPrice *float64
	// RequestedAt is the OTLP span start time (UTC) — the day-bucketing
	// and window-membership key.
	RequestedAt time.Time
	// InsertedAt is the database insertion time — the polling
	// fallback's cursor (tui/SPEC.md §7).
	InsertedAt time.Time

	// Live metadata — not database columns. Live marks rows sourced
	// from the LiveSource; only live rows participate in accents and
	// meter lag (tui/SPEC.md §5/§7). ReceivedAt is when the meter saw
	// the event; zero unless Live.
	Live       bool
	ReceivedAt time.Time
}

// TotalTokens is the bar-length contribution: input + output.
func (r RequestRow) TotalTokens() int64 { return r.InputTokens + r.OutputTokens }

// DailyRow is one `usage_daily` view row: (UTC day, model, provider_slug)
// grain with additive sums only — ratios are always derived client-side
// from these sums at the selected window (root SPEC §2).
//
// SQL SUM semantics for the pointer fields: nil only when every summed
// value in the group was NULL; if any row reported, the sum of the
// reported values comes back non-nil. Aggregate() mirrors this rule.
type DailyRow struct {
	// Day is midnight UTC of the bucketed day (requested_at::date).
	Day time.Time
	// Model and ProviderSlug are the grouping keys; slug nil when the
	// payload never reported a provider that day.
	Model        string
	ProviderSlug *string
	// RequestCount is count(*) — never nil.
	RequestCount int64
	// Token and cost sums; not-null columns sum to plain values.
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
	// Nullable sums (see SQL SUM semantics above).
	CachedTokens    *int64
	ReasoningTokens *int64
	InputCostUSD    *float64
	OutputCostUSD   *float64
	DurationMSSum   *int64
	// TimedRequestCount is count(duration_ms): how many rows reported a
	// duration — the avg-duration denominator. Never nil.
	TimedRequestCount int64
}

// rowKey is the dedupe identity of a request row.
type rowKey struct{ traceID, spanID string }

// RowStore holds deduped RequestRows from the two sources that share one
// shape: the today-slice fetch and live events (tui/SPEC.md §7). The
// merge rule is live-wins: a live row upgrades a previously fetched row
// (preserving ReceivedAt and accent eligibility), while a fetched row
// never overwrites anything — resolving the subscribe-before-fetch
// overlap in both arrival orders.
type RowStore struct {
	rows map[rowKey]RequestRow
}

// NewRowStore returns an empty store.
func NewRowStore() *RowStore {
	return &RowStore{rows: make(map[rowKey]RequestRow)}
}

// Upsert inserts or merges a row and reports whether its key was
// previously unseen — the signal Stage D's accent emphasis and the
// credits debounce key off ("is this event genuinely new?").
func (s *RowStore) Upsert(r RequestRow) bool {
	k := rowKey{r.TraceID, r.SpanID}
	existing, seen := s.rows[k]
	if !seen {
		s.rows[k] = r
		return true
	}
	// Live wins: only a live row may replace what is stored, and only
	// when the stored row isn't already live-sourced.
	if r.Live && !existing.Live {
		s.rows[k] = r
	}
	return false
}

// Rows returns the deduped rows in a deterministic order — RequestedAt
// ascending, ties by (TraceID, SpanID) — so aggregation and tests never
// depend on map iteration order.
func (s *RowStore) Rows() []RequestRow {
	out := make([]RequestRow, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].RequestedAt.Equal(out[j].RequestedAt) {
			return out[i].RequestedAt.Before(out[j].RequestedAt)
		}
		if out[i].TraceID != out[j].TraceID {
			return out[i].TraceID < out[j].TraceID
		}
		return out[i].SpanID < out[j].SpanID
	})
	return out
}

// Len reports the number of stored rows.
func (s *RowStore) Len() int { return len(s.rows) }

// Clear empties the store — a manual refresh re-baselines the world
// (tui/SPEC.md §5/§7).
func (s *RowStore) Clear() { s.rows = make(map[rowKey]RequestRow) }

// ClearLive drops live-sourced rows only — on a realtime rejoin, missed
// events are healed by refetching, so stale live rows go (tui/SPEC.md §7).
func (s *RowStore) ClearLive() { s.clearWhere(func(r RequestRow) bool { return r.Live }) }

// ClearFetched drops fetched rows only — at local midnight the stale
// today-slice is discarded before the refetch (tui/SPEC.md §7).
func (s *RowStore) ClearFetched() { s.clearWhere(func(r RequestRow) bool { return !r.Live }) }

// clearWhere removes rows matching the predicate.
func (s *RowStore) clearWhere(match func(RequestRow) bool) {
	for k, r := range s.rows {
		if match(r) {
			delete(s.rows, k)
		}
	}
}
