// Bar geometry: the shared auto-ranging scale and per-bar cell math
// (tui/SPEC.md §3). Provisional in Stage A — Stage B owns the test suite
// that formalizes these functions.

package core

import "math"

// scaleHeadroom is the fill ceiling: S is the smallest ladder value such
// that the longest bar occupies at most this fraction of the content
// width, so no bar is ever pinned at 100% (tui/SPEC.md §3).
const scaleHeadroom = 0.8

// scaleFloor is the smallest allowed scale (the ladder floor), keeping
// early-morning trickles from producing absurdly zoomed-in bars.
const scaleFloor = 10_000

// ScaleFor returns the shared bar scale S: the smallest value from the
// 1–2–5 ladder (10K, 20K, 50K, 100K, …) such that maxTokens ≤ 0.8·S.
// It is a pure function of the current window's data — no state, no
// hysteresis (tui/SPEC.md §3).
func ScaleFor(maxTokens int64) int64 {
	s := int64(scaleFloor)
	// Walk the 1–2–5 ladder: ×2, ×2.5, ×2 repeating (10 → 20 → 50 → 100).
	steps := []int64{2, 5, 10} // multipliers relative to the decade base
	base := int64(scaleFloor)
	for i := 0; float64(maxTokens) > scaleHeadroom*float64(s); i++ {
		s = base * steps[i%3]
		if i%3 == 2 {
			base *= 10
		}
	}
	return s
}

// BarWidth converts a token quantity into cells on the shared scale:
// floor(width · tokens / scale), clamped to [1, width] for any nonzero
// quantity so tiny-but-real usage never disappears (tui/SPEC.md §3).
// Zero tokens yield zero cells.
func BarWidth(width int, tokens, scale int64) int {
	if tokens <= 0 || width <= 0 || scale <= 0 {
		return 0
	}
	cells := int(math.Floor(float64(width) * float64(tokens) / float64(scale)))
	if cells < 1 {
		return 1
	}
	if cells > width {
		return width
	}
	return cells
}

// SplitFraction returns the input share of a bar's interior — the
// boundary between the input and output segments. Primary source: the
// window's summed actual split costs (cache discounts embodied). If the
// split costs are unreported (nil) or both zero (free models), fall back
// to token volumes; with no tokens either, the fraction is 0
// (tui/SPEC.md §3, zero-guarded per §8).
func SplitFraction(m ModelStat) float64 {
	if m.InputCost != nil && m.OutputCost != nil {
		if total := *m.InputCost + *m.OutputCost; total > 0 {
			return *m.InputCost / total
		}
	}
	if total := m.InputTokens + m.OutputTokens; total > 0 {
		return float64(m.InputTokens) / float64(total)
	}
	return 0
}
