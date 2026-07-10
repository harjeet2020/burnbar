// Aggregation — the heart of the app and the most-tested code in it
// (tui/SPEC.md §7): one pure function from the stores to the sorted
// []ModelStat both screens render. All ratios are derived from window
// sums (never averaged across rows), every division is zero-guarded,
// and nil sums stay nil so unreported values render "—" (root SPEC §2).

package core

import (
	"sort"
	"time"
)

// AggregateInput carries the spec's four aggregate() arguments (baseline,
// rows, window, now — tui/SPEC.md §7) plus the local-today cut input and
// the active display mode, which decides sort order.
type AggregateInput struct {
	// Baseline is the last usage_daily fetch (31 days, UTC-day grain).
	Baseline []DailyRow
	// Rows are the deduped raw request rows (RowStore.Rows()):
	// today-slice fetch ∪ live events.
	Rows []RequestRow
	// Window is the active timeframe.
	Window Timeframe
	// Now anchors the window bounds.
	Now time.Time
	// Loc is the zone for the local-today cut; the UI passes
	// time.Local, tests pass pinned zones (core never reads the
	// system zone itself).
	Loc *time.Location
	// Mode is the active display mode; it decides the sort key (cost desc
	// vs total-tokens desc, tui/SPEC.md §3). Zero value is ModeCost.
	Mode Mode
}

// Aggregate computes the per-model window aggregates, including the
// per-(model, provider_slug) grain.
//
// Source selection (tui/SPEC.md §7): today sums raw rows only, cut at
// local midnight — the raw slice can be re-cut to any local offset
// exactly. Week/month sum baseline view rows plus ONLY live-sourced
// rows layered on top: fetched today-slice rows are already inside the
// baseline's current UTC day, so adding them would double-count
// permanently rather than the spec's self-healing brief overlap.
func Aggregate(in AggregateInput) []ModelStat {
	start := WindowStart(in.Window, in.Now, in.Loc)
	inWindow := func(at time.Time) bool { return !at.Before(start) }

	acc := newAccumulator()
	if in.Window == TimeframeToday {
		for _, r := range in.Rows {
			if inWindow(r.RequestedAt) {
				acc.addRequest(r)
			}
		}
	} else {
		for _, d := range in.Baseline {
			if inWindow(d.Day) {
				acc.addDaily(d)
			}
		}
		for _, r := range in.Rows {
			if r.Live && inWindow(r.RequestedAt) {
				acc.addRequest(r)
			}
		}
	}

	models := acc.fold()

	out := make([]ModelStat, 0, len(models))
	for _, m := range models {
		out = append(out, *m)
	}
	sortModels(out, in.Mode)
	return out
}

// provKey identifies one (model, provider_slug) bucket; an unreported
// slug is its own bucket, distinct from any named slug.
type provKey struct {
	model   string
	slug    string
	hasSlug bool
}

// accumulator gathers provider-grain sums; the model grain folds out of
// it afterwards, so both grains come from one pass (tui/SPEC.md §7).
type accumulator struct {
	provs map[provKey]*ProviderStat
}

func newAccumulator() *accumulator {
	return &accumulator{provs: make(map[provKey]*ProviderStat)}
}

// bucket returns the ProviderStat for (model, slug), creating it on
// first sight.
func (a *accumulator) bucket(model string, slug *string) *ProviderStat {
	k := provKey{model: model}
	if slug != nil {
		k.slug, k.hasSlug = *slug, true
	}
	p, ok := a.provs[k]
	if !ok {
		p = &ProviderStat{}
		if slug != nil {
			s := *slug
			p.Slug = &s
		}
		a.provs[k] = p
	}
	return p
}

// addDaily folds one usage_daily row into its provider bucket.
func (a *accumulator) addDaily(d DailyRow) {
	p := a.bucket(d.Model, d.ProviderSlug)
	p.Requests += d.RequestCount
	p.InputTokens += d.InputTokens
	p.OutputTokens += d.OutputTokens
	p.Cost += d.CostUSD
	p.CachedTokens = addI64(p.CachedTokens, d.CachedTokens)
	p.ReasoningTokens = addI64(p.ReasoningTokens, d.ReasoningTokens)
	p.InputCost = addF64(p.InputCost, d.InputCostUSD)
	p.OutputCost = addF64(p.OutputCost, d.OutputCostUSD)
	p.DurationMSSum = addI64(p.DurationMSSum, d.DurationMSSum)
	p.TimedRequests += d.TimedRequestCount
}

// addRequest folds one raw request row into its provider bucket.
func (a *accumulator) addRequest(r RequestRow) {
	p := a.bucket(r.Model, r.ProviderSlug)
	p.Requests++
	p.InputTokens += r.InputTokens
	p.OutputTokens += r.OutputTokens
	p.Cost += r.CostUSD
	p.CachedTokens = addI64(p.CachedTokens, r.CachedTokens)
	p.ReasoningTokens = addI64(p.ReasoningTokens, r.ReasoningTokens)
	p.InputCost = addF64(p.InputCost, r.InputCostUSD)
	p.OutputCost = addF64(p.OutputCost, r.OutputCostUSD)
	p.DurationMSSum = addI64(p.DurationMSSum, r.DurationMS)
	if r.DurationMS != nil {
		p.TimedRequests++
	}
}

// fold collapses provider buckets into per-model stats with sorted
// provider splits.
func (a *accumulator) fold() map[string]*ModelStat {
	models := make(map[string]*ModelStat)
	for k, p := range a.provs {
		m, ok := models[k.model]
		if !ok {
			m = &ModelStat{Name: k.model}
			models[k.model] = m
		}
		m.Requests += p.Requests
		m.InputTokens += p.InputTokens
		m.OutputTokens += p.OutputTokens
		m.Cost += p.Cost
		m.CachedTokens = addI64(m.CachedTokens, p.CachedTokens)
		m.ReasoningTokens = addI64(m.ReasoningTokens, p.ReasoningTokens)
		m.InputCost = addF64(m.InputCost, p.InputCost)
		m.OutputCost = addF64(m.OutputCost, p.OutputCost)
		m.DurationMSSum = addI64(m.DurationMSSum, p.DurationMSSum)
		m.TimedRequests += p.TimedRequests
		m.Providers = append(m.Providers, *p)
	}
	for _, m := range models {
		sortProviders(m.Providers)
	}
	return models
}

// arrivedAfter orders live rows by arrival: ReceivedAt, ties broken by
// RequestedAt, then by the dedupe key — fully deterministic so the
// latest-live/burst picks (tui/SPEC.md §7) never flip-flop between equal
// timestamps.
func arrivedAfter(a, b RequestRow) bool {
	if !a.ReceivedAt.Equal(b.ReceivedAt) {
		return a.ReceivedAt.After(b.ReceivedAt)
	}
	if !a.RequestedAt.Equal(b.RequestedAt) {
		return a.RequestedAt.After(b.RequestedAt)
	}
	if a.TraceID != b.TraceID {
		return a.TraceID > b.TraceID
	}
	return a.SpanID > b.SpanID
}

// sortModels applies the binding list order in the active mode: window
// cost descending in cost mode, total tokens descending in token mode,
// ties by name ascending either way — stable, jitter-free, and the
// longest bar always sits on top (tui/SPEC.md §2/§3).
func sortModels(models []ModelStat, mode Mode) {
	sort.Slice(models, func(i, j int) bool {
		vi, vj := models[i].Value(mode), models[j].Value(mode)
		if vi != vj {
			return vi > vj
		}
		return models[i].Name < models[j].Name
	})
}

// sortProviders orders a provider split by cost descending, ties by
// slug ascending with the unreported (nil) slug last.
func sortProviders(ps []ProviderStat) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Cost != ps[j].Cost {
			return ps[i].Cost > ps[j].Cost
		}
		a, b := ps[i].Slug, ps[j].Slug
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return *a < *b
		}
	})
}

// addI64 sums two nullable int64 sums under SQL SUM semantics: nil only
// when both sides are nil, so "reported as zero" survives merging while
// "never reported" stays nil (root SPEC §2).
func addI64(a, b *int64) *int64 {
	if a == nil && b == nil {
		return nil
	}
	var s int64
	if a != nil {
		s += *a
	}
	if b != nil {
		s += *b
	}
	return &s
}

// addF64 is addI64 for float sums.
func addF64(a, b *float64) *float64 {
	if a == nil && b == nil {
		return nil
	}
	var s float64
	if a != nil {
		s += *a
	}
	if b != nil {
		s += *b
	}
	return &s
}

// pctOf derives a percentage from a nullable numerator sum and an
// integer denominator; nil numerator or zero denominator yields nil
// (render "—", never a fake 0 — root SPEC §2).
func pctOf(num *int64, den int64) *float64 {
	if num == nil || den == 0 {
		return nil
	}
	v := float64(*num) / float64(den) * 100
	return &v
}

// ratePerMillion derives an effective $/1M-token rate from a nullable
// cost sum: cost/tokens × 1e6 — reflects discounts actually received
// (tui/SPEC.md §2).
func ratePerMillion(cost *float64, tokens int64) *float64 {
	if cost == nil || tokens == 0 {
		return nil
	}
	v := *cost / float64(tokens) * 1e6
	return &v
}

// avgOf derives a mean from a nullable sum and its count.
func avgOf(sum *int64, count int64) *float64 {
	if sum == nil || count == 0 {
		return nil
	}
	v := float64(*sum) / float64(count)
	return &v
}

// CacheHitPct is cached/input as a percentage; nil when cached tokens
// were never reported or no input tokens exist.
func (m ModelStat) CacheHitPct() *float64 { return pctOf(m.CachedTokens, m.InputTokens) }

// ReasoningPct is reasoning/output as a percentage.
func (m ModelStat) ReasoningPct() *float64 { return pctOf(m.ReasoningTokens, m.OutputTokens) }

// EffectiveInputRate is the realized input $/1M tokens.
func (m ModelStat) EffectiveInputRate() *float64 { return ratePerMillion(m.InputCost, m.InputTokens) }

// EffectiveOutputRate is the realized output $/1M tokens.
func (m ModelStat) EffectiveOutputRate() *float64 {
	return ratePerMillion(m.OutputCost, m.OutputTokens)
}

// AvgDurationMS is duration_ms_sum / timed_request_count (root SPEC §2).
func (m ModelStat) AvgDurationMS() *float64 { return avgOf(m.DurationMSSum, m.TimedRequests) }

// CacheHitPct — see ModelStat.CacheHitPct.
func (p ProviderStat) CacheHitPct() *float64 { return pctOf(p.CachedTokens, p.InputTokens) }

// ReasoningPct — see ModelStat.ReasoningPct.
func (p ProviderStat) ReasoningPct() *float64 { return pctOf(p.ReasoningTokens, p.OutputTokens) }

// EffectiveInputRate — see ModelStat.EffectiveInputRate.
func (p ProviderStat) EffectiveInputRate() *float64 {
	return ratePerMillion(p.InputCost, p.InputTokens)
}

// EffectiveOutputRate — see ModelStat.EffectiveOutputRate.
func (p ProviderStat) EffectiveOutputRate() *float64 {
	return ratePerMillion(p.OutputCost, p.OutputTokens)
}

// AvgDurationMS — see ModelStat.AvgDurationMS.
func (p ProviderStat) AvgDurationMS() *float64 { return avgOf(p.DurationMSSum, p.TimedRequests) }

// SharePct is this provider's share of the model's window cost; nil
// when the model cost is zero (free models — no meaningful share).
func (p ProviderStat) SharePct(modelCost float64) *float64 {
	if modelCost == 0 {
		return nil
	}
	v := p.Cost / modelCost * 100
	return &v
}
