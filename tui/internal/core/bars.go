// Bar geometry: the shared auto-ranging scale and per-bar cell math
// (tui/SPEC.md §3). Generic over a Mode's value (cost or tokens) — one
// code path serves both display modes.

package core

import "math"

// scaleHeadroom is the fill ceiling: S is the smallest ladder value such
// that the longest bar occupies at most this fraction of the content
// width, so no bar is ever pinned at 100% (tui/SPEC.md §3).
const scaleHeadroom = 0.8

// scaleFloorTokens/scaleFloorCost are the smallest allowed scale per mode
// (the ladder floor), keeping early-morning trickles from producing
// absurdly zoomed-in bars. scaleFloorCost is a "tune by feel" constant
// (tui/SPEC.md §11).
const (
	scaleFloorTokens = 10_000
	scaleFloorCost   = 0.01
)

// scaleMaxIterations bounds the ladder walk for absurd inputs: BarWidth
// clamps to the content width regardless, so this is purely a "never loop
// forever" guarantee, not a rendering one.
const scaleMaxIterations = 300

// ScaleFor returns the shared bar scale S: the smallest value from the
// active mode's 1–2–5 ladder such that maxValue ≤ 0.8·S. It is a pure
// function of the current window's data — no state, no hysteresis
// (tui/SPEC.md §3).
func ScaleFor(maxValue float64, mode Mode) float64 {
	floor := float64(scaleFloorTokens)
	if mode == ModeCost {
		floor = scaleFloorCost
	}
	s := floor
	// Walk the 1–2–5 ladder: ×2, ×2.5, ×2 repeating (10 → 20 → 50 → 100).
	steps := []float64{2, 5, 10} // multipliers relative to the decade base
	base := floor
	for i := 0; maxValue > scaleHeadroom*s && i < scaleMaxIterations; i++ {
		s = base * steps[i%3]
		if i%3 == 2 {
			base *= 10
		}
	}
	return s
}

// BarWidth converts a value quantity into cells on the shared scale:
// floor(width · value / scale), clamped to [1, width] for any nonzero
// quantity so tiny-but-real usage never disappears (tui/SPEC.md §3).
// Zero (or negative) value yields zero cells.
func BarWidth(width int, value, scale float64) int {
	if value <= 0 || width <= 0 || scale <= 0 {
		return 0
	}
	cells := int(math.Floor(float64(width) * value / scale))
	if cells < 1 {
		return 1
	}
	if cells > width {
		return width
	}
	return cells
}

// BarGeometry is the cell layout of one bar row (tui/SPEC.md §3/§5).
// InputCells is the input/output glyph boundary measured across the whole
// bar. BrightInput/BrightOutput are the trailing cells of their own zone
// painted the latest-burst's brighter shade — a subset of that zone, never
// a separate slice, so the glyph pattern never changes for the highlight.
type BarGeometry struct {
	// Cells is the total bar length; 0 means draw nothing.
	Cells int
	// InputCells is the glyph boundary: round(SplitFraction·Cells). The
	// input zone is [0, InputCells); the output zone is [InputCells, Cells).
	InputCells int
	// BrightInput is the trailing cell count of the input zone painted
	// accent.primary.bright (the latest burst's input share).
	BrightInput int
	// BrightOutput is the trailing cell count of the output zone (the
	// bar's tail) painted bar.output.bright (the latest burst's output
	// share).
	BrightOutput int
}

// SplitBar lays out an already-decided integer bar width into its cell
// runs, given the interior proportions (tui/SPEC.md §3/§5/§6). It is the
// render-time counterpart to the shared scale: the spring animates the
// total, and every frame the segments are recomputed as fractions of it.
//
//   - displayCells: the current integer bar width (0 ⇒ draw nothing).
//   - inputFrac: the input/output glyph boundary as a share of the whole
//     bar (SplitFraction, mode-aware).
//   - brightInputFrac / brightOutputFrac: the latest burst's input/output
//     share of the whole bar's active-mode value — each is clamped inside
//     its own zone below, never crossing into the other.
//
// Minimum-visible floor (tui/SPEC.md §5): a nonzero bright fraction that
// rounds to zero cells is forced to one, borrowed from within its own
// zone. This guarantees a burst too small to lengthen the bar still paints
// one visible cell — the "a request landing is seen" rule — without
// changing the bar's overall length. If a zone has no cells at all (e.g.
// InputCells == 0), the floor is skipped: there is nothing in that zone to
// borrow from, and a bright cell must never bleed into the other zone.
func SplitBar(displayCells int, inputFrac, brightInputFrac, brightOutputFrac float64) BarGeometry {
	if displayCells <= 0 {
		return BarGeometry{}
	}

	inputCells := clampCells(int(math.Round(inputFrac*float64(displayCells))), displayCells)
	outputCells := displayCells - inputCells

	brightInput := clampCells(int(math.Round(brightInputFrac*float64(displayCells))), inputCells)
	brightOutput := clampCells(int(math.Round(brightOutputFrac*float64(displayCells))), outputCells)

	if brightInputFrac > 0 && brightInput == 0 && inputCells > 0 {
		brightInput = 1
	}
	if brightOutputFrac > 0 && brightOutput == 0 && outputCells > 0 {
		brightOutput = 1
	}

	return BarGeometry{
		Cells:        displayCells,
		InputCells:   inputCells,
		BrightInput:  brightInput,
		BrightOutput: brightOutput,
	}
}

// clampCells bounds a computed cell count to [0, max].
func clampCells(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}

// SplitFraction returns the input share of a bar's interior — the
// boundary between the input and output segments — in the active mode's
// unit (tui/SPEC.md §3, zero-guarded per §8).
//
// Token mode: always the token-volume ratio, cost fields ignored entirely.
// Cost mode: primary source is the window's summed actual split costs
// (cache discounts embodied); if unreported (nil) or both zero (free
// models), falls back to token volumes; with no tokens either, 0.
func SplitFraction(m ModelStat, mode Mode) float64 {
	if mode == ModeTokens {
		if total := m.InputTokens + m.OutputTokens; total > 0 {
			return float64(m.InputTokens) / float64(total)
		}
		return 0
	}
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
