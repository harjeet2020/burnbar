package core

import (
	"testing"
	"time"
)

func TestLatestBurstNoLiveRows(t *testing.T) {
	rows := []RequestRow{
		req("t1", "s1", "m/a", 100, 10, 0.01, time.Now(), false),
	}
	if got := LatestBurst(rows); got != nil {
		t.Errorf("got %+v, want nil (no live row seen)", got)
	}
}

func TestLatestBurstSingleRow(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	r := req("t1", "s1", "m/a", 100, 50, 0.05, base, true)

	b := LatestBurst([]RequestRow{r})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.Model != "m/a" || b.Requests != 1 {
		t.Errorf("Model/Requests = %s/%d, want m/a/1", b.Model, b.Requests)
	}
	if b.InputTokens != 100 || b.OutputTokens != 50 || b.Cost != 0.05 {
		t.Errorf("sums = %d/%d/%v, want 100/50/0.05", b.InputTokens, b.OutputTokens, b.Cost)
	}
	if !b.FirstRequestedAt.Equal(base) || !b.LastRequestedAt.Equal(base) {
		t.Errorf("span = %v..%v, want both %v", b.FirstRequestedAt, b.LastRequestedAt, base)
	}
	if !b.LastReceivedAt.Equal(r.ReceivedAt) {
		t.Errorf("LastReceivedAt = %v, want %v", b.LastReceivedAt, r.ReceivedAt)
	}
}

func TestLatestBurstChainsRowsWithinGap(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	// Three same-model live rows, each arrival 1s after the previous —
	// well within burstGap (3s) — must coalesce into one burst.
	r1 := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	r1.ReceivedAt = base.Add(1 * time.Second)
	r2 := req("t2", "s1", "m/a", 200, 20, 0.02, base.Add(time.Second), true)
	r2.ReceivedAt = base.Add(2 * time.Second)
	r3 := req("t3", "s1", "m/a", 300, 30, 0.03, base.Add(2*time.Second), true)
	r3.ReceivedAt = base.Add(3 * time.Second)

	b := LatestBurst([]RequestRow{r1, r2, r3})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.Requests != 3 {
		t.Errorf("Requests = %d, want 3", b.Requests)
	}
	if b.InputTokens != 600 || b.OutputTokens != 60 {
		t.Errorf("sums = %d/%d, want 600/60", b.InputTokens, b.OutputTokens)
	}
	wantCost := 0.01 + 0.02 + 0.03
	if diff := b.Cost - wantCost; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Cost = %v, want %v", b.Cost, wantCost)
	}
}

func TestLatestBurstBreaksOnGapBeyondBurstGap(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	old := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	old.ReceivedAt = base
	recent := req("t2", "s1", "m/a", 200, 20, 0.02, base.Add(10*time.Second), true)
	recent.ReceivedAt = base.Add(burstGap + time.Second) // > burstGap after `old`

	b := LatestBurst([]RequestRow{old, recent})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.Requests != 1 {
		t.Errorf("Requests = %d, want 1 (gap beyond burstGap excludes the older row)", b.Requests)
	}
	if b.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200 (only the newest row)", b.InputTokens)
	}
}

func TestLatestBurstExcludesOtherModels(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	a := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	a.ReceivedAt = base.Add(1 * time.Second)
	other := req("t2", "s1", "m/other", 200, 20, 0.02, base.Add(time.Second), true)
	other.ReceivedAt = base.Add(2 * time.Second) // newest arrival, different model

	b := LatestBurst([]RequestRow{a, other})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.Model != "m/other" || b.Requests != 1 {
		t.Errorf("Model/Requests = %s/%d, want m/other/1 (cross-model rows never chain)", b.Model, b.Requests)
	}
}

func TestLatestBurstExcludesFetchedRows(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	fetched := req("t1", "s1", "m/a", 999, 999, 9.99, base.Add(time.Hour), false) // newest span, but not live
	live := req("t2", "s1", "m/a", 100, 10, 0.01, base, true)
	live.ReceivedAt = base.Add(1 * time.Second)

	b := LatestBurst([]RequestRow{fetched, live})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.Requests != 1 || b.InputTokens != 100 {
		t.Errorf("Requests/InputTokens = %d/%d, want 1/100 (fetched row never included)", b.Requests, b.InputTokens)
	}
}

func TestLatestBurstDeterministicTieBreak(t *testing.T) {
	// Two rows with identical ReceivedAt: arrivedAfter's deterministic
	// tie-break must make LatestBurst's model choice repeatable.
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	a := req("ta", "s1", "m/a", 100, 10, 0.01, base, true)
	a.ReceivedAt = base
	b := req("tb", "s1", "m/b", 200, 20, 0.02, base, true)
	b.ReceivedAt = base

	got1 := LatestBurst([]RequestRow{a, b})
	got2 := LatestBurst([]RequestRow{b, a})
	if got1 == nil || got2 == nil {
		t.Fatal("got nil, want a burst")
	}
	if got1.Model != got2.Model {
		t.Errorf("input order changed the pick: %s vs %s, want the same model both times", got1.Model, got2.Model)
	}
}

func TestLatestBurstNullableSums(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	r1 := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	r1.ReceivedAt = base.Add(1 * time.Second)
	r1.CachedTokens = i64(5)
	r1.InputCostUSD = f64(0.005)
	r2 := req("t2", "s1", "m/a", 200, 20, 0.02, base.Add(time.Second), true)
	r2.ReceivedAt = base.Add(2 * time.Second)
	// r2 leaves CachedTokens/InputCostUSD nil.

	b := LatestBurst([]RequestRow{r1, r2})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.CachedTokens == nil || *b.CachedTokens != 5 {
		t.Errorf("CachedTokens = %v, want pointer to 5 (SQL SUM semantics: nil ⊕ 5 → 5)", b.CachedTokens)
	}
	if b.InputCost == nil || *b.InputCost != 0.005 {
		t.Errorf("InputCost = %v, want pointer to 0.005", b.InputCost)
	}
}

func TestLatestBurstProviderSlugConsistentSetsSlug(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	r1 := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	r1.ReceivedAt = base.Add(1 * time.Second)
	r1.ProviderSlug = str("novita/fp8")
	r2 := req("t2", "s1", "m/a", 200, 20, 0.02, base.Add(time.Second), true)
	r2.ReceivedAt = base.Add(2 * time.Second)
	r2.ProviderSlug = str("novita/fp8")

	b := LatestBurst([]RequestRow{r1, r2})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.ProviderSlug == nil || *b.ProviderSlug != "novita/fp8" {
		t.Errorf("ProviderSlug = %v, want pointer to novita/fp8", b.ProviderSlug)
	}
}

func TestLatestBurstProviderSlugMismatchOmits(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	r1 := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	r1.ReceivedAt = base.Add(1 * time.Second)
	r1.ProviderSlug = str("novita/fp8")
	r2 := req("t2", "s1", "m/a", 200, 20, 0.02, base.Add(time.Second), true)
	r2.ReceivedAt = base.Add(2 * time.Second)
	r2.ProviderSlug = str("together/bf16")

	b := LatestBurst([]RequestRow{r1, r2})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.ProviderSlug != nil {
		t.Errorf("ProviderSlug = %v, want nil (rows disagree on routed provider)", *b.ProviderSlug)
	}
}

func TestLatestBurstProviderSlugUnreportedOmits(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	r1 := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)
	r1.ReceivedAt = base.Add(1 * time.Second)
	r2 := req("t2", "s1", "m/a", 200, 20, 0.02, base.Add(time.Second), true)
	r2.ReceivedAt = base.Add(2 * time.Second)

	b := LatestBurst([]RequestRow{r1, r2})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.ProviderSlug != nil {
		t.Errorf("ProviderSlug = %v, want nil (never reported)", *b.ProviderSlug)
	}
}

func TestLatestBurstProviderSlugSingleRowUnreported(t *testing.T) {
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	r := req("t1", "s1", "m/a", 100, 10, 0.01, base, true)

	b := LatestBurst([]RequestRow{r})
	if b == nil {
		t.Fatal("got nil, want a burst")
	}
	if b.ProviderSlug != nil {
		t.Errorf("ProviderSlug = %v, want nil (degenerate single-row, unreported)", *b.ProviderSlug)
	}
}

func TestBurstInputOutputValue(t *testing.T) {
	t.Run("token mode reads token sums directly", func(t *testing.T) {
		b := Burst{InputTokens: 300, OutputTokens: 100}
		if got := b.InputValue(ModeTokens); got != 300 {
			t.Errorf("InputValue = %v, want 300", got)
		}
		if got := b.OutputValue(ModeTokens); got != 100 {
			t.Errorf("OutputValue = %v, want 100", got)
		}
	})

	t.Run("cost mode prefers reported split costs", func(t *testing.T) {
		b := Burst{InputCost: f64(0.03), OutputCost: f64(0.07), Cost: 0.10}
		if got := b.InputValue(ModeCost); got != 0.03 {
			t.Errorf("InputValue = %v, want 0.03", got)
		}
		if got := b.OutputValue(ModeCost); got != 0.07 {
			t.Errorf("OutputValue = %v, want 0.07", got)
		}
	})

	t.Run("cost mode falls back to distributing total cost by token ratio", func(t *testing.T) {
		b := Burst{InputTokens: 300, OutputTokens: 100, Cost: 0.08}
		if got := b.InputValue(ModeCost); got != 0.06 { // 0.08 * 300/400
			t.Errorf("InputValue = %v, want 0.06", got)
		}
		if got := b.OutputValue(ModeCost); got != 0.02 { // 0.08 * 100/400
			t.Errorf("OutputValue = %v, want 0.02", got)
		}
	})

	t.Run("cost mode with no tokens and no split costs yields 0", func(t *testing.T) {
		b := Burst{Cost: 0.08}
		if got := b.InputValue(ModeCost); got != 0 {
			t.Errorf("InputValue = %v, want 0", got)
		}
	})
}
