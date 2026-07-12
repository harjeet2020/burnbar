// The latest burst (tui/SPEC.md §7): a display-only coalescing of rapid
// same-model live arrivals into one logical event, so an agent's tool-call
// fan-out reads as a single highlight/modal subject rather than just its
// last, possibly tiny, call.

package core

import (
	"sort"
	"time"
)

// burstGap is the max arrival gap between two live rows for them to be
// considered part of the same burst. Tune by feel (tui/SPEC.md §11) — 5s
// gives a sequential agentic tool loop (each round's gap driven by that
// round's tool latency) more room to stay coalesced than a single LLM
// call's turnaround alone would need.
const burstGap = 5 * time.Second

// Burst is the most recent run of live rows for one model, coalesced for
// display (tui/SPEC.md §5/§7). It is the single input to both the
// latest-burst highlight and the recent-request modal (Stage D.4) — never
// fed into the authoritative window totals, which aggregate every row
// independently.
type Burst struct {
	Model    string
	Requests int

	// ProviderSlug is the burst's routed provider (modal §2 Stage D.4) —
	// set only when every included row reports the same slug; nil when
	// unreported or the burst spans a mid-burst provider fallback, so the
	// modal never claims more specificity than actually happened.
	ProviderSlug *string

	InputTokens, OutputTokens     int64
	CachedTokens, ReasoningTokens *int64

	Cost                  float64
	InputCost, OutputCost *float64

	// FirstRequestedAt/LastRequestedAt are the wall-time span the burst
	// covers. LastReceivedAt is the arrival time of the newest row in the
	// burst — the highlight/modal's "age" figure.
	FirstRequestedAt, LastRequestedAt time.Time
	LastReceivedAt                    time.Time
}

// InputValue/OutputValue return the burst's input/output share in the
// active mode's unit. Token mode reads the summed token counts directly;
// cost mode prefers the summed actual split costs, falling back to
// distributing the burst's total Cost by its own token ratio when the
// split costs are unreported — mirroring SplitFraction's fallback so the
// highlight and the segment boundary never visually disagree. Both split
// costs must be present to use them (matching SplitFraction's own
// requirement) rather than checking each field independently: a burst
// with only one side reported would otherwise mix a real dollar value on
// one side with a token-distributed estimate on the other, disagreeing
// with SplitFraction's all-or-nothing choice for the same row.
func (b Burst) InputValue(mode Mode) float64 {
	if mode == ModeTokens {
		return float64(b.InputTokens)
	}
	if b.InputCost != nil && b.OutputCost != nil {
		return *b.InputCost
	}
	if total := b.InputTokens + b.OutputTokens; total > 0 {
		return b.Cost * float64(b.InputTokens) / float64(total)
	}
	return 0
}

func (b Burst) OutputValue(mode Mode) float64 {
	if mode == ModeTokens {
		return float64(b.OutputTokens)
	}
	if b.InputCost != nil && b.OutputCost != nil {
		return *b.OutputCost
	}
	if total := b.InputTokens + b.OutputTokens; total > 0 {
		return b.Cost * float64(b.OutputTokens) / float64(total)
	}
	return 0
}

// TotalTokens — see ModelStat.TotalTokens; the recent-request modal's
// tokens row total (tui/SPEC.md §2 Stage D.4 refinement).
func (b Burst) TotalTokens() int64 { return b.InputTokens + b.OutputTokens }

// Value — see ModelStat.Value; the burst's own total in the active mode's
// unit, from the same authoritative fields (Cost / TotalTokens) rather
// than InputValue+OutputValue. The highlight geometry (meter.go) derives
// the bright output share as this total's fraction minus the bright input
// share, rather than computing an independent output fraction, so the two
// always sum to the burst's true total share of the bar — no residual gap
// or overlap from InputValue/OutputValue not summing back to Value.
func (b Burst) Value(mode Mode) float64 {
	if mode == ModeTokens {
		return float64(b.TotalTokens())
	}
	return b.Cost
}

// CacheHitPct — see ModelStat.CacheHitPct.
func (b Burst) CacheHitPct() *float64 { return pctOf(b.CachedTokens, b.InputTokens) }

// ReasoningPct — see ModelStat.ReasoningPct.
func (b Burst) ReasoningPct() *float64 { return pctOf(b.ReasoningTokens, b.OutputTokens) }

// EffectiveRate — see ModelStat.EffectiveRate.
func (b Burst) EffectiveRate() *float64 {
	cost := b.Cost
	return ratePerMillion(&cost, b.TotalTokens())
}

// EffectiveInputRate — see ModelStat.EffectiveInputRate.
func (b Burst) EffectiveInputRate() *float64 { return ratePerMillion(b.InputCost, b.InputTokens) }

// EffectiveOutputRate — see ModelStat.EffectiveOutputRate.
func (b Burst) EffectiveOutputRate() *float64 { return ratePerMillion(b.OutputCost, b.OutputTokens) }

// InputTokensPct — see ModelStat.InputTokensPct.
func (b Burst) InputTokensPct() *float64 { return tokenSharePct(b.InputTokens, b.TotalTokens()) }

// OutputTokensPct — see ModelStat.InputTokensPct, output's share.
func (b Burst) OutputTokensPct() *float64 { return tokenSharePct(b.OutputTokens, b.TotalTokens()) }

// InputCostPct — see ModelStat.InputCostPct.
func (b Burst) InputCostPct() *float64 { return pctOfFloat(b.InputCost, b.Cost) }

// OutputCostPct — see ModelStat.InputCostPct, output's share.
func (b Burst) OutputCostPct() *float64 { return pctOfFloat(b.OutputCost, b.Cost) }

// LatestBurst coalesces the most recent live rows for one model into a
// display-only Burst (tui/SPEC.md §7): starting from the newest live
// arrival (LatestLive), it walks backward through that model's live rows
// in arrival order, including each row while it lands within burstGap of
// the previous one. nil when no live row has been seen. Window-agnostic
// by design — it operates over every live row ever seen, not the active
// timeframe; gating the highlight to the current window is a UI-layer
// concern.
func LatestBurst(rows []RequestRow) *Burst {
	newest, ok := LatestLive(rows)
	if !ok {
		return nil
	}

	live := make([]RequestRow, 0, len(rows))
	for _, r := range rows {
		if r.Live && r.Model == newest.Model {
			live = append(live, r)
		}
	}
	// Most recent arrival first (the same deterministic ordering accent2
	// used), so the walk-back below is a simple adjacent-pair gap check.
	sort.Slice(live, func(i, j int) bool { return arrivedAfter(live[i], live[j]) })

	included := live[:1]
	for i := 1; i < len(live); i++ {
		if live[i-1].ReceivedAt.Sub(live[i].ReceivedAt) > burstGap {
			break
		}
		included = live[:i+1]
	}

	b := Burst{
		Model:            newest.Model,
		Requests:         len(included),
		ProviderSlug:     included[0].ProviderSlug,
		FirstRequestedAt: included[0].RequestedAt,
		LastRequestedAt:  included[0].RequestedAt,
		LastReceivedAt:   included[0].ReceivedAt,
	}
	slugConsistent := true
	for _, r := range included {
		b.InputTokens += r.InputTokens
		b.OutputTokens += r.OutputTokens
		b.Cost += r.CostUSD
		b.CachedTokens = addI64(b.CachedTokens, r.CachedTokens)
		b.ReasoningTokens = addI64(b.ReasoningTokens, r.ReasoningTokens)
		b.InputCost = addF64(b.InputCost, r.InputCostUSD)
		b.OutputCost = addF64(b.OutputCost, r.OutputCostUSD)
		if !sameProviderSlug(b.ProviderSlug, r.ProviderSlug) {
			slugConsistent = false
		}
		if r.RequestedAt.Before(b.FirstRequestedAt) {
			b.FirstRequestedAt = r.RequestedAt
		}
		if r.RequestedAt.After(b.LastRequestedAt) {
			b.LastRequestedAt = r.RequestedAt
		}
	}
	if !slugConsistent {
		b.ProviderSlug = nil
	}
	return &b
}

// sameProviderSlug reports whether two possibly-nil provider slugs are the
// same reported value; two unreported slugs (both nil) count as agreeing.
func sameProviderSlug(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
