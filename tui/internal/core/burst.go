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
// considered part of the same burst. Tune by feel (tui/SPEC.md §11).
const burstGap = 3 * time.Second

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
// highlight and the segment boundary never visually disagree.
func (b Burst) InputValue(mode Mode) float64 {
	if mode == ModeTokens {
		return float64(b.InputTokens)
	}
	if b.InputCost != nil {
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
	if b.OutputCost != nil {
		return *b.OutputCost
	}
	if total := b.InputTokens + b.OutputTokens; total > 0 {
		return b.Cost * float64(b.OutputTokens) / float64(total)
	}
	return 0
}

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
