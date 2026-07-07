// JSON transport structs for the two PostgREST read surfaces (requests
// and usage_daily) and the realtime INSERT payload. They live here, not
// in internal/core, so that core stays free of struct tags and network
// concerns (its package doc promises pure, dependency-free math). Each
// DTO maps into the matching core type, preserving the NULL-vs-0 rule:
// a JSON null decodes to a nil pointer ("never reported"), a JSON 0 to a
// pointed-to zero ("reported as zero") — root SPEC §2.

package data

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// requestDTO mirrors one public.requests row as PostgREST serializes it.
// Money columns are numeric in SQL but arrive as JSON numbers and decode
// into float64 for display (SPEC §6 permits float aggregation); token and
// duration columns are integer. Timestamps arrive as strings and are
// parsed tolerantly (see parseTimestamp) rather than relying on
// encoding/json's stricter time.Time handling.
type requestDTO struct {
	TraceID         string   `json:"trace_id"`
	SpanID          string   `json:"span_id"`
	Model           string   `json:"model"`
	Author          *string  `json:"author"`
	Provider        *string  `json:"provider"`
	ProviderSlug    *string  `json:"provider_slug"`
	InputTokens     int64    `json:"input_tokens"`
	OutputTokens    int64    `json:"output_tokens"`
	CachedTokens    *int64   `json:"cached_tokens"`
	ReasoningTokens *int64   `json:"reasoning_tokens"`
	CostUSD         float64  `json:"cost_usd"`
	InputCostUSD    *float64 `json:"input_cost_usd"`
	OutputCostUSD   *float64 `json:"output_cost_usd"`
	InputUnitPrice  *float64 `json:"input_unit_price"`
	OutputUnitPrice *float64 `json:"output_unit_price"`
	RequestedAt     string   `json:"requested_at"`
	DurationMS      *int64   `json:"duration_ms"`
	InsertedAt      string   `json:"inserted_at"`
}

// toRequestRow converts the DTO into a core.RequestRow. The Live flag and
// ReceivedAt are left zero — the caller (a fetch marks rows fetched, the
// LiveSource marks and stamps them live) owns that metadata, keeping this
// conversion a pure, clock-free mapping. A malformed timestamp is a hard
// error: a row we can't place in time can't be windowed correctly.
func (d requestDTO) toRequestRow() (core.RequestRow, error) {
	requestedAt, err := parseTimestamp(d.RequestedAt)
	if err != nil {
		return core.RequestRow{}, fmt.Errorf("requested_at: %w", err)
	}
	// inserted_at is optional in a realtime payload edge case; tolerate an
	// empty value rather than dropping the whole row.
	var insertedAt time.Time
	if d.InsertedAt != "" {
		if insertedAt, err = parseTimestamp(d.InsertedAt); err != nil {
			return core.RequestRow{}, fmt.Errorf("inserted_at: %w", err)
		}
	}
	return core.RequestRow{
		TraceID:         d.TraceID,
		SpanID:          d.SpanID,
		Model:           d.Model,
		Author:          d.Author,
		Provider:        d.Provider,
		ProviderSlug:    d.ProviderSlug,
		InputTokens:     d.InputTokens,
		OutputTokens:    d.OutputTokens,
		CachedTokens:    d.CachedTokens,
		ReasoningTokens: d.ReasoningTokens,
		DurationMS:      d.DurationMS,
		CostUSD:         d.CostUSD,
		InputCostUSD:    d.InputCostUSD,
		OutputCostUSD:   d.OutputCostUSD,
		InputUnitPrice:  d.InputUnitPrice,
		OutputUnitPrice: d.OutputUnitPrice,
		RequestedAt:     requestedAt,
		InsertedAt:      insertedAt,
	}, nil
}

// dailyDTO mirrors one usage_daily view row. `day` is a SQL date and
// arrives as "YYYY-MM-DD" (no time), so it is parsed as a UTC midnight
// rather than an RFC3339 timestamp. The nullable SUM columns keep nil
// (the group was entirely NULL) distinct from a real zero sum.
type dailyDTO struct {
	Day               string   `json:"day"`
	Model             string   `json:"model"`
	ProviderSlug      *string  `json:"provider_slug"`
	RequestCount      int64    `json:"request_count"`
	InputTokens       int64    `json:"input_tokens"`
	OutputTokens      int64    `json:"output_tokens"`
	CachedTokens      *int64   `json:"cached_tokens"`
	ReasoningTokens   *int64   `json:"reasoning_tokens"`
	CostUSD           float64  `json:"cost_usd"`
	InputCostUSD      *float64 `json:"input_cost_usd"`
	OutputCostUSD     *float64 `json:"output_cost_usd"`
	DurationMSSum     *int64   `json:"duration_ms_sum"`
	TimedRequestCount int64    `json:"timed_request_count"`
}

// toDailyRow converts the DTO into a core.DailyRow, parsing `day` as a
// UTC calendar day (the view buckets on requested_at::date at UTC).
func (d dailyDTO) toDailyRow() (core.DailyRow, error) {
	day, err := time.ParseInLocation("2006-01-02", d.Day, time.UTC)
	if err != nil {
		return core.DailyRow{}, fmt.Errorf("day %q: %w", d.Day, err)
	}
	return core.DailyRow{
		Day:               day,
		Model:             d.Model,
		ProviderSlug:      d.ProviderSlug,
		RequestCount:      d.RequestCount,
		InputTokens:       d.InputTokens,
		OutputTokens:      d.OutputTokens,
		CachedTokens:      d.CachedTokens,
		ReasoningTokens:   d.ReasoningTokens,
		CostUSD:           d.CostUSD,
		InputCostUSD:      d.InputCostUSD,
		OutputCostUSD:     d.OutputCostUSD,
		DurationMSSum:     d.DurationMSSum,
		TimedRequestCount: d.TimedRequestCount,
	}, nil
}

// realtimeInsertPayload is the shape of a postgres_changes INSERT payload:
// the new row lives under "record" (old_record is empty for an insert).
// realtime-go hands us this as the event's raw JSON body.
type realtimeInsertPayload struct {
	Record requestDTO `json:"record"`
}

// decodeRequests unmarshals a PostgREST array of request rows into core
// rows. A single unparseable row fails the whole batch — the baseline
// fetch is all-or-nothing, and a bad row signals a contract mismatch
// worth surfacing rather than silently dropping.
func decodeRequests(body []byte) ([]core.RequestRow, error) {
	var dtos []requestDTO
	if err := json.Unmarshal(body, &dtos); err != nil {
		return nil, fmt.Errorf("decode requests: %w", err)
	}
	rows := make([]core.RequestRow, 0, len(dtos))
	for i, d := range dtos {
		r, err := d.toRequestRow()
		if err != nil {
			return nil, fmt.Errorf("requests[%d] (trace %s): %w", i, d.TraceID, err)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// decodeDaily unmarshals a PostgREST array of usage_daily rows into core
// rows, with the same all-or-nothing contract as decodeRequests.
func decodeDaily(body []byte) ([]core.DailyRow, error) {
	var dtos []dailyDTO
	if err := json.Unmarshal(body, &dtos); err != nil {
		return nil, fmt.Errorf("decode usage_daily: %w", err)
	}
	rows := make([]core.DailyRow, 0, len(dtos))
	for i, d := range dtos {
		r, err := d.toDailyRow()
		if err != nil {
			return nil, fmt.Errorf("usage_daily[%d] (model %s): %w", i, d.Model, err)
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// timestampLayouts are tried in order against timestamptz strings from
// both PostgREST (microseconds + "+00:00" offset) and the edge-function
// writer (millisecond "Z"). RFC3339Nano covers both, but the explicit
// fallbacks guard against serializer quirks in the pre-v1 realtime path.
var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999-07:00",
	"2006-01-02T15:04:05Z07:00",
}

// parseTimestamp parses a timestamptz string into a time.Time, trying the
// known layouts. The result is normalized to UTC so downstream window
// math (which compares against UTC/local bounds) is offset-agnostic.
func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}
