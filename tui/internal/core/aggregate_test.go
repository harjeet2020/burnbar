package core

import (
	"testing"
	"time"
)

// i64 and str are pointer-literal helpers (f64 lives in fixture.go).
func i64(v int64) *int64   { return &v }
func str(v string) *string { return &v }

// req builds a RequestRow for aggregate tests. Live rows receive their
// event two seconds after the span start unless the test overrides
// ReceivedAt afterwards.
func req(trace, span, model string, in, out int64, cost float64, at time.Time, live bool) RequestRow {
	r := RequestRow{
		TraceID:      trace,
		SpanID:       span,
		Model:        model,
		InputTokens:  in,
		OutputTokens: out,
		CostUSD:      cost,
		RequestedAt:  at,
		Live:         live,
	}
	if live {
		r.ReceivedAt = at.Add(2 * time.Second)
	}
	return r
}

// statFor finds a model in Aggregate output or fails the test.
func statFor(t *testing.T, stats []ModelStat, name string) ModelStat {
	t.Helper()
	for _, s := range stats {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("model %q not in output (have %d models)", name, len(stats))
	return ModelStat{}
}

func TestAggregateTodayUsesLocalCut(t *testing.T) {
	// Kolkata (+05:30): rows between 00:00 and 05:30 local belong to
	// the local day but to the PREVIOUS UTC day — a UTC cut would
	// wrongly drop them. The rows at/after local midnight sit exactly
	// in that region, so their inclusion proves the cut is local.
	loc := zoneMust("Asia/Kolkata")
	midnight := time.Date(2026, 7, 6, 0, 0, 0, 0, loc)
	now := midnight.Add(10 * time.Hour)

	rows := []RequestRow{
		req("t1", "s1", "m/a", 100, 10, 0.01, midnight.Add(-time.Nanosecond), false),
		req("t2", "s1", "m/a", 100, 10, 0.01, midnight, false),
		req("t3", "s1", "m/a", 100, 10, 0.01, midnight.Add(time.Nanosecond), false),
	}
	out := Aggregate(AggregateInput{Rows: rows, Window: TimeframeToday, Now: now, Loc: loc})

	if len(out) != 1 {
		t.Fatalf("len = %d, want 1 model", len(out))
	}
	m := out[0]
	if m.Requests != 2 || m.InputTokens != 200 {
		t.Errorf("Requests=%d InputTokens=%d; want 2 and 200 (midnight row in, pre-midnight out)", m.Requests, m.InputTokens)
	}
}

func TestAggregateTodayIgnoresBaseline(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	baseline := []DailyRow{{
		Day: UTCDayStart(now), Model: "m/a", RequestCount: 5,
		InputTokens: 1000, CostUSD: 0.5,
	}}
	out := Aggregate(AggregateInput{Baseline: baseline, Window: TimeframeToday, Now: now, Loc: time.UTC})
	if len(out) != 0 {
		t.Errorf("today must aggregate rows only; got %d models from baseline", len(out))
	}
}

func TestAggregateWeekMonthBaselineEdges(t *testing.T) {
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	baseline := []DailyRow{
		{Day: day(2026, 6, 29), Model: "m/d6", RequestCount: 1, InputTokens: 10, CostUSD: 0.1}, // D-6
		{Day: day(2026, 6, 28), Model: "m/d7", RequestCount: 1, InputTokens: 10, CostUSD: 0.1}, // D-7
		{Day: day(2026, 6, 6), Model: "m/d29", RequestCount: 1, InputTokens: 10, CostUSD: 0.1}, // D-29
		{Day: day(2026, 6, 5), Model: "m/d30", RequestCount: 1, InputTokens: 10, CostUSD: 0.1}, // D-30
	}

	week := Aggregate(AggregateInput{Baseline: baseline, Window: TimeframeWeek, Now: now, Loc: time.UTC})
	if len(week) != 1 || week[0].Name != "m/d6" {
		t.Errorf("week: got %d models %v, want exactly m/d6", len(week), names(week))
	}

	month := Aggregate(AggregateInput{Baseline: baseline, Window: TimeframeMonth, Now: now, Loc: time.UTC})
	if len(month) != 3 {
		t.Errorf("month: got %d models %v, want m/d6+m/d7+m/d29 (D-30 excluded)", len(month), names(month))
	}
	for _, s := range month {
		if s.Name == "m/d30" {
			t.Error("month must exclude the D-30 day")
		}
	}
}

// names extracts model names for failure messages.
func names(stats []ModelStat) []string {
	out := make([]string, len(stats))
	for i, s := range stats {
		out[i] = s.Name
	}
	return out
}

func TestAggregateWeekExcludesFetchedRowsButLayersLive(t *testing.T) {
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour)
	baseline := []DailyRow{{
		Day: UTCDayStart(now), Model: "m/a", ProviderSlug: str("prov"),
		RequestCount: 1, InputTokens: 100, CostUSD: 0.1,
	}}
	rows := []RequestRow{
		// The fetched today-slice row duplicates data already inside
		// the baseline's current UTC day — it must NOT be added again.
		func() RequestRow {
			r := req("tf", "s1", "m/a", 100, 0, 0.1, at, false)
			r.ProviderSlug = str("prov")
			return r
		}(),
		// The live row is genuinely newer than the baseline fetch — it
		// layers on top.
		func() RequestRow {
			r := req("tl", "s1", "m/a", 50, 0, 0.05, at, true)
			r.ProviderSlug = str("prov")
			return r
		}(),
		// A live row for a model the baseline has never seen.
		req("tn", "s1", "m/new", 30, 0, 0.03, at, true),
	}

	out := Aggregate(AggregateInput{Baseline: baseline, Rows: rows, Window: TimeframeWeek, Now: now, Loc: time.UTC})

	a := statFor(t, out, "m/a")
	if a.InputTokens != 150 || a.Requests != 2 {
		t.Errorf("m/a InputTokens=%d Requests=%d; want 150 and 2 (baseline 100 + live 50, fetched row excluded)", a.InputTokens, a.Requests)
	}
	if len(a.Providers) != 1 {
		t.Fatalf("m/a providers = %d, want 1 (baseline + live fold into the same slug)", len(a.Providers))
	}
	if got := a.Providers[0].InputTokens; got != 150 {
		t.Errorf("provider grain InputTokens = %d, want 150", got)
	}
	if n := statFor(t, out, "m/new"); n.InputTokens != 30 {
		t.Errorf("m/new InputTokens = %d, want 30 (live row creates unseen model)", n.InputTokens)
	}
}

func TestAggregateNullVsZeroMerge(t *testing.T) {
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	d1, d2 := UTCDayStart(now), UTCDayStart(now).AddDate(0, 0, -1)
	baseline := []DailyRow{
		{
			Day: d1, Model: "m/a", RequestCount: 1, CostUSD: 0.1,
			CachedTokens:    nil,    // nil ⊕ nil → nil
			ReasoningTokens: nil,    // nil ⊕ 0 → 0
			InputCostUSD:    nil,    // nil ⊕ 0.05 → 0.05
			DurationMSSum:   i64(2), // 2 ⊕ 3 → 5
		},
		{
			Day: d2, Model: "m/a", RequestCount: 1, CostUSD: 0.1,
			CachedTokens:    nil,
			ReasoningTokens: i64(0),
			InputCostUSD:    f64(0.05),
			DurationMSSum:   i64(3),
		},
	}
	out := Aggregate(AggregateInput{Baseline: baseline, Window: TimeframeWeek, Now: now, Loc: time.UTC})
	m := statFor(t, out, "m/a")

	if m.CachedTokens != nil {
		t.Errorf("CachedTokens = %v, want nil (never reported)", *m.CachedTokens)
	}
	if m.ReasoningTokens == nil || *m.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %v, want pointer to 0 (reported zero survives)", m.ReasoningTokens)
	}
	if m.InputCost == nil || *m.InputCost != 0.05 {
		t.Errorf("InputCost = %v, want pointer to 0.05", m.InputCost)
	}
	if m.DurationMSSum == nil || *m.DurationMSSum != 5 {
		t.Errorf("DurationMSSum = %v, want pointer to 5", m.DurationMSSum)
	}
}

func TestAggregateProviderGrain(t *testing.T) {
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	d := UTCDayStart(now)
	baseline := []DailyRow{
		{Day: d, Model: "m/a", ProviderSlug: str("novita/fp8"), RequestCount: 2, InputTokens: 100, CostUSD: 0.2},
		{Day: d, Model: "m/a", ProviderSlug: str("deepinfra"), RequestCount: 1, InputTokens: 50, CostUSD: 0.1},
		{Day: d, Model: "m/a", ProviderSlug: nil, RequestCount: 1, InputTokens: 25, CostUSD: 0.1}, // ties deepinfra; nil sorts last
	}
	out := Aggregate(AggregateInput{Baseline: baseline, Window: TimeframeWeek, Now: now, Loc: time.UTC})
	m := statFor(t, out, "m/a")

	if m.InputTokens != 175 || m.Requests != 4 || m.Cost != 0.1+0.1+0.2 {
		t.Errorf("model fold: InputTokens=%d Requests=%d Cost=%v", m.InputTokens, m.Requests, m.Cost)
	}
	if len(m.Providers) != 3 {
		t.Fatalf("providers = %d, want 3", len(m.Providers))
	}
	if m.Providers[0].Slug == nil || *m.Providers[0].Slug != "novita/fp8" {
		t.Errorf("providers[0] = %v, want novita/fp8 (highest cost)", m.Providers[0].Slug)
	}
	if m.Providers[1].Slug == nil || *m.Providers[1].Slug != "deepinfra" {
		t.Errorf("providers[1] = %v, want deepinfra (tie: named slug before nil)", m.Providers[1].Slug)
	}
	if m.Providers[2].Slug != nil {
		t.Errorf("providers[2].Slug = %v, want nil last on ties", *m.Providers[2].Slug)
	}
}

func TestAggregateSortOrder(t *testing.T) {
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	d := UTCDayStart(now)
	baseline := []DailyRow{
		{Day: d, Model: "m/cheap", RequestCount: 1, CostUSD: 0.1},
		{Day: d, Model: "m/big", RequestCount: 1, CostUSD: 0.9},
		{Day: d, Model: "b/tie", RequestCount: 1, CostUSD: 0.5},
		{Day: d, Model: "a/tie", RequestCount: 1, CostUSD: 0.5},
	}
	out := Aggregate(AggregateInput{Baseline: baseline, Window: TimeframeWeek, Now: now, Loc: time.UTC})
	want := []string{"m/big", "a/tie", "b/tie", "m/cheap"}
	for i, name := range want {
		if out[i].Name != name {
			t.Fatalf("order = %v, want %v (cost desc, ties name asc)", names(out), want)
		}
	}
}

func TestAggregateAccents(t *testing.T) {
	loc := time.UTC
	midnight := time.Date(2026, 7, 5, 0, 0, 0, 0, loc)
	now := midnight.Add(10 * time.Hour)
	anchor := midnight.Add(9*time.Hour + time.Second) // 09:00:01

	// rowA: live, arrived 09:00:02 — inside the anchor window.
	rowA := req("ta", "s1", "m/a", 100, 0, 0.1, midnight.Add(9*time.Hour), true)
	// rowB: long-running request — OLD span start (08:00) but the
	// NEWEST arrival (09:30). This pins accent2 to arrival order.
	rowB := req("tb", "s1", "m/b", 50, 0, 0.05, midnight.Add(8*time.Hour), true)
	rowB.ReceivedAt = midnight.Add(9*time.Hour + 30*time.Minute)
	// rowC: fetched, newest span start of all — must never accent.
	rowC := req("tc", "s1", "m/c", 30, 0, 0.03, midnight.Add(9*time.Hour+45*time.Minute), false)
	// rowD: live, arrived exactly AT the anchor — excluded (strictly after).
	rowD := req("td", "s1", "m/a", 20, 0, 0.02, midnight.Add(8*time.Hour+30*time.Minute), true)
	rowD.ReceivedAt = anchor

	in := AggregateInput{
		Rows:   []RequestRow{rowA, rowB, rowC, rowD},
		Window: TimeframeToday, Now: now, Loc: loc, Anchor: anchor,
	}
	out := Aggregate(in)

	b := statFor(t, out, "m/b")
	if b.Accent2Tokens != 50 {
		t.Errorf("m/b Accent2Tokens = %d, want 50 (newest by ReceivedAt wins)", b.Accent2Tokens)
	}
	a := statFor(t, out, "m/a")
	if a.Accent1Tokens != 100 {
		t.Errorf("m/a Accent1Tokens = %d, want 100 (rowA after anchor; rowD exactly at anchor excluded)", a.Accent1Tokens)
	}
	if a.Accent2Tokens != 0 {
		t.Errorf("m/a Accent2Tokens = %d, want 0", a.Accent2Tokens)
	}
	c := statFor(t, out, "m/c")
	if c.Accent1Tokens != 0 || c.Accent2Tokens != 0 {
		t.Errorf("fetched row must never accent; got acc1=%d acc2=%d", c.Accent1Tokens, c.Accent2Tokens)
	}
	// accent1 must exclude the accent2 row itself.
	if b.Accent1Tokens != 0 {
		t.Errorf("m/b Accent1Tokens = %d, want 0 (accent2 row excluded from accent1)", b.Accent1Tokens)
	}

	t.Run("zero anchor disables accent1, keeps accent2", func(t *testing.T) {
		in := in
		in.Anchor = time.Time{}
		out := Aggregate(in)
		if got := statFor(t, out, "m/a").Accent1Tokens; got != 0 {
			t.Errorf("Accent1Tokens = %d with zero anchor, want 0", got)
		}
		if got := statFor(t, out, "m/b").Accent2Tokens; got != 50 {
			t.Errorf("Accent2Tokens = %d with zero anchor, want 50 (accent2 is anchor-independent)", got)
		}
	})
}

func TestAggregateOutOfWindowLiveRowAccentsNothing(t *testing.T) {
	loc := time.UTC
	midnight := time.Date(2026, 7, 5, 0, 0, 0, 0, loc)
	now := midnight.Add(10 * time.Hour)

	// The globally most recent arrival is a PRE-midnight row: outside
	// the today window, so it has no bar to slice — no accent2
	// anywhere, and no fallback to the older in-window row.
	old := req("to", "s1", "m/old", 200, 0, 0.2, midnight.Add(-time.Hour), true)
	old.ReceivedAt = now.Add(-time.Minute)
	cur := req("tc", "s1", "m/cur", 100, 0, 0.1, midnight.Add(9*time.Hour), true)
	cur.ReceivedAt = midnight.Add(9*time.Hour + 2*time.Second)

	out := Aggregate(AggregateInput{
		Rows:   []RequestRow{old, cur},
		Window: TimeframeToday, Now: now, Loc: loc,
		Anchor: midnight.Add(8 * time.Hour),
	})

	if len(out) != 1 || out[0].Name != "m/cur" {
		t.Fatalf("models = %v, want only m/cur (pre-midnight row out of window)", names(out))
	}
	if out[0].Accent2Tokens != 0 {
		t.Errorf("Accent2Tokens = %d, want 0 (no fallback when the newest arrival is out of window)", out[0].Accent2Tokens)
	}
	if out[0].Accent1Tokens != 100 {
		t.Errorf("Accent1Tokens = %d, want 100 (in-window live row after anchor still accents)", out[0].Accent1Tokens)
	}
}

func TestAggregateEmptyInputs(t *testing.T) {
	out := Aggregate(AggregateInput{
		Window: TimeframeToday,
		Now:    time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		Loc:    time.UTC,
	})
	if len(out) != 0 {
		t.Errorf("len = %d, want 0 for empty inputs", len(out))
	}
}

func TestModelStatRatios(t *testing.T) {
	t.Run("CacheHitPct", func(t *testing.T) {
		if got := (ModelStat{InputTokens: 200, CachedTokens: i64(50)}).CacheHitPct(); got == nil || *got != 25 {
			t.Errorf("got %v, want 25", got)
		}
		if got := (ModelStat{InputTokens: 200}).CacheHitPct(); got != nil {
			t.Errorf("nil CachedTokens: got %v, want nil", *got)
		}
		if got := (ModelStat{InputTokens: 200, CachedTokens: i64(0)}).CacheHitPct(); got == nil || *got != 0 {
			t.Errorf("reported-zero cached: got %v, want pointer to 0", got)
		}
		if got := (ModelStat{CachedTokens: i64(50)}).CacheHitPct(); got != nil {
			t.Errorf("zero InputTokens: got %v, want nil (division guard)", *got)
		}
	})

	t.Run("ReasoningPct", func(t *testing.T) {
		if got := (ModelStat{OutputTokens: 100, ReasoningTokens: i64(25)}).ReasoningPct(); got == nil || *got != 25 {
			t.Errorf("got %v, want 25", got)
		}
		if got := (ModelStat{OutputTokens: 100}).ReasoningPct(); got != nil {
			t.Errorf("nil ReasoningTokens: got %v, want nil", *got)
		}
	})

	t.Run("EffectiveRates", func(t *testing.T) {
		m := ModelStat{InputTokens: 2_000_000, OutputTokens: 500_000, InputCost: f64(1.0), OutputCost: f64(2.0)}
		if got := m.EffectiveInputRate(); got == nil || *got != 0.5 {
			t.Errorf("input rate = %v, want 0.5 $/1M", got)
		}
		if got := m.EffectiveOutputRate(); got == nil || *got != 4.0 {
			t.Errorf("output rate = %v, want 4.0 $/1M", got)
		}
		if got := (ModelStat{InputTokens: 1000}).EffectiveInputRate(); got != nil {
			t.Errorf("nil InputCost: got %v, want nil", *got)
		}
		if got := (ModelStat{InputCost: f64(1)}).EffectiveInputRate(); got != nil {
			t.Errorf("zero tokens: got %v, want nil (division guard)", *got)
		}
	})

	t.Run("AvgDurationMS", func(t *testing.T) {
		if got := (ModelStat{DurationMSSum: i64(3000), TimedRequests: 2}).AvgDurationMS(); got == nil || *got != 1500 {
			t.Errorf("got %v, want 1500", got)
		}
		if got := (ModelStat{TimedRequests: 2}).AvgDurationMS(); got != nil {
			t.Errorf("nil sum: got %v, want nil", *got)
		}
		if got := (ModelStat{DurationMSSum: i64(3000)}).AvgDurationMS(); got != nil {
			t.Errorf("zero timed requests: got %v, want nil (division guard)", *got)
		}
	})

	t.Run("ProviderStat SharePct", func(t *testing.T) {
		p := ProviderStat{Cost: 0.05}
		if got := p.SharePct(0.2); got == nil || *got != 25 {
			t.Errorf("got %v, want 25", got)
		}
		if got := p.SharePct(0); got != nil {
			t.Errorf("zero model cost: got %v, want nil (division guard)", *got)
		}
	})

	t.Run("ProviderStat carries the same ratio set", func(t *testing.T) {
		p := ProviderStat{
			InputTokens: 1_000_000, OutputTokens: 100,
			InputCost: f64(0.25), CachedTokens: i64(250_000),
			DurationMSSum: i64(900), TimedRequests: 3,
		}
		if got := p.EffectiveInputRate(); got == nil || *got != 0.25 {
			t.Errorf("EffectiveInputRate = %v, want 0.25 $/1M", got)
		}
		if got := p.CacheHitPct(); got == nil || *got != 25 {
			t.Errorf("CacheHitPct = %v, want 25", got)
		}
		if got := p.AvgDurationMS(); got == nil || *got != 300 {
			t.Errorf("AvgDurationMS = %v, want 300", got)
		}
		if got := p.EffectiveOutputRate(); got != nil {
			t.Errorf("nil OutputCost: got %v, want nil", *got)
		}
		if got := p.ReasoningPct(); got != nil {
			t.Errorf("nil ReasoningTokens: got %v, want nil", *got)
		}
	})
}
