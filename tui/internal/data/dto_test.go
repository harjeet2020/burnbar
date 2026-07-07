package data

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeRequestsNullVsZero(t *testing.T) {
	// Two rows: one fully reported (with a zero cached_tokens), one with
	// the nullable attributes absent. The zero must survive as 0, the
	// nulls must stay nil — never conflated (root SPEC §2).
	body := []byte(`[
	  {
	    "trace_id": "t1", "span_id": "s1", "model": "deepseek/deepseek-v4",
	    "author": "deepseek", "provider": "Novita", "provider_slug": "novita/fp8",
	    "input_tokens": 1200, "output_tokens": 340,
	    "cached_tokens": 0, "reasoning_tokens": 50,
	    "cost_usd": 0.4821, "input_cost_usd": 0.19, "output_cost_usd": 0.29,
	    "input_unit_price": 0.00000016, "output_unit_price": 0.0000006,
	    "requested_at": "2026-07-06T11:59:00.123Z",
	    "duration_ms": 2100,
	    "inserted_at": "2026-07-06T11:59:02.500000+00:00"
	  },
	  {
	    "trace_id": "t2", "span_id": "s2", "model": "meta/llama-free",
	    "author": null, "provider": null, "provider_slug": null,
	    "input_tokens": 88, "output_tokens": 12,
	    "cached_tokens": null, "reasoning_tokens": null,
	    "cost_usd": 0, "input_cost_usd": null, "output_cost_usd": null,
	    "input_unit_price": null, "output_unit_price": null,
	    "requested_at": "2026-07-06T10:00:00Z",
	    "duration_ms": null,
	    "inserted_at": "2026-07-06T10:00:01Z"
	  }
	]`)

	rows, err := decodeRequests(body)
	if err != nil {
		t.Fatalf("decodeRequests: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	r0 := rows[0]
	if r0.CachedTokens == nil || *r0.CachedTokens != 0 {
		t.Errorf("row0 CachedTokens = %v, want pointer to 0 (reported zero)", r0.CachedTokens)
	}
	if r0.ProviderSlug == nil || *r0.ProviderSlug != "novita/fp8" {
		t.Errorf("row0 ProviderSlug = %v, want novita/fp8", r0.ProviderSlug)
	}
	if r0.DurationMS == nil || *r0.DurationMS != 2100 {
		t.Errorf("row0 DurationMS = %v, want 2100", r0.DurationMS)
	}
	wantReq := time.Date(2026, 7, 6, 11, 59, 0, int(123*time.Millisecond), time.UTC)
	if !r0.RequestedAt.Equal(wantReq) {
		t.Errorf("row0 RequestedAt = %v, want %v", r0.RequestedAt, wantReq)
	}

	r1 := rows[1]
	if r1.CachedTokens != nil {
		t.Errorf("row1 CachedTokens = %v, want nil (never reported)", *r1.CachedTokens)
	}
	if r1.ProviderSlug != nil {
		t.Errorf("row1 ProviderSlug = %v, want nil", *r1.ProviderSlug)
	}
	if r1.DurationMS != nil {
		t.Errorf("row1 DurationMS = %v, want nil", *r1.DurationMS)
	}
	if r1.CostUSD != 0 {
		t.Errorf("row1 CostUSD = %v, want 0", r1.CostUSD)
	}
	if r1.Live {
		t.Error("decoded rows must not be flagged Live (caller owns that)")
	}
}

func TestDecodeDailyAllNullSums(t *testing.T) {
	// A day where no row reported cache/reasoning/split-cost/duration:
	// every SUM comes back null and must stay nil, but count(*) and the
	// not-null token sums are real values.
	body := []byte(`[
	  {
	    "day": "2026-07-06", "model": "openai/gpt-5.2-mini", "provider_slug": null,
	    "request_count": 41, "input_tokens": 402300, "output_tokens": 51800,
	    "cached_tokens": null, "reasoning_tokens": null,
	    "cost_usd": 0.1204, "input_cost_usd": null, "output_cost_usd": null,
	    "duration_ms_sum": null, "timed_request_count": 0
	  }
	]`)

	rows, err := decodeDaily(body)
	if err != nil {
		t.Fatalf("decodeDaily: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	d := rows[0]
	if d.CachedTokens != nil || d.ReasoningTokens != nil ||
		d.InputCostUSD != nil || d.OutputCostUSD != nil || d.DurationMSSum != nil {
		t.Error("all-NULL SUM columns must decode to nil pointers, not 0")
	}
	if d.RequestCount != 41 || d.InputTokens != 402300 {
		t.Errorf("non-null counts wrong: %+v", d)
	}
	wantDay := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	if !d.Day.Equal(wantDay) {
		t.Errorf("Day = %v, want %v", d.Day, wantDay)
	}
}

func TestRealtimeInsertPayload(t *testing.T) {
	// The postgres_changes INSERT payload nests the new row under "record".
	payload := []byte(`{
	  "record": {
	    "id": 42, "trace_id": "tr", "span_id": "sp", "model": "qwen/qwen3-coder",
	    "input_tokens": 955000, "output_tokens": 82300, "cost_usd": 0.0214,
	    "requested_at": "2026-07-06T12:00:00Z", "inserted_at": "2026-07-06T12:00:01Z"
	  },
	  "old_record": {}
	}`)

	var p realtimeInsertPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	row, err := p.Record.toRequestRow()
	if err != nil {
		t.Fatalf("toRequestRow: %v", err)
	}
	if row.TraceID != "tr" || row.SpanID != "sp" || row.Model != "qwen/qwen3-coder" {
		t.Errorf("decoded record wrong: %+v", row)
	}
	if row.InputTokens != 955000 || row.CostUSD != 0.0214 {
		t.Errorf("decoded sums wrong: %+v", row)
	}
}

func TestParseTimestampVariants(t *testing.T) {
	cases := []string{
		"2026-07-06T11:59:00.123Z",
		"2026-07-06T11:59:00Z",
		"2026-07-06T11:59:00.500000+00:00",
		"2026-07-06T04:59:00-07:00", // offset normalizes to 11:59 UTC
	}
	want := time.Date(2026, 7, 6, 11, 59, 0, 0, time.UTC)
	for _, s := range cases {
		got, err := parseTimestamp(s)
		if err != nil {
			t.Errorf("parseTimestamp(%q): %v", s, err)
			continue
		}
		if got.Truncate(time.Second) != want {
			t.Errorf("parseTimestamp(%q) = %v, want %v (to the second)", s, got, want)
		}
		if got.Location() != time.UTC {
			t.Errorf("parseTimestamp(%q) location = %v, want UTC", s, got.Location())
		}
	}

	if _, err := parseTimestamp("not-a-time"); err == nil {
		t.Error("parseTimestamp(garbage) should error")
	}
}
