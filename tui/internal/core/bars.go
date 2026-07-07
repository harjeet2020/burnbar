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
// The ladder tops out at the largest 1–2–5 value representable in
// int64 (5e18): absurd inputs cap there instead of overflowing the walk
// (BarWidth clamps to the content width anyway, so a capped scale still
// renders sanely).
func ScaleFor(maxTokens int64) int64 {
	s := int64(scaleFloor)
	// Walk the 1–2–5 ladder: ×2, ×2.5, ×2 repeating (10 → 20 → 50 → 100).
	steps := []int64{2, 5, 10} // multipliers relative to the decade base
	base := int64(scaleFloor)
	for i := 0; float64(maxTokens) > scaleHeadroom*float64(s); i++ {
		next := base * steps[i%3]
		if next/steps[i%3] != base { // multiplication overflowed int64
			return s
		}
		s = next
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

// BarGeometry is the cell layout of one bar row (tui/SPEC.md §3/§5).
// Invariant: Cells = Base + Acc1 + Acc2. InputCells is the input/output
// glyph boundary measured across the whole bar — accent regions recolor
// the tail without changing glyphs, so the split stays readable in
// monochrome.
type BarGeometry struct {
	// Cells is the total bar length; 0 means draw nothing.
	Cells int
	// InputCells is the glyph boundary: round(SplitFraction·Cells).
	InputCells int
	// Base is the un-accented leading region.
	Base int
	// Acc1 is the accent.session slice (usage since the anchor).
	Acc1 int
	// Acc2 is the accent.latest slice, always rightmost (the newest
	// usage grows the bar rightward).
	Acc2 int
}

// Geometry computes a model's bar layout on the shared scale. Accent
// slices are token-proportional via the same BarWidth math as the bar
// itself, then clamped so accents can never exceed the bar:
// Acc2 ≤ Cells, Acc1 ≤ Cells − Acc2 (tui/SPEC.md §5).
func Geometry(m ModelStat, width int, scale int64) BarGeometry {
	cells := BarWidth(width, m.TotalTokens(), scale)
	if cells == 0 {
		return BarGeometry{}
	}

	inputCells := int(math.Round(SplitFraction(m) * float64(cells)))
	if inputCells > cells {
		inputCells = cells
	}

	acc2 := BarWidth(width, m.Accent2Tokens, scale)
	if acc2 > cells {
		acc2 = cells
	}
	acc1 := BarWidth(width, m.Accent1Tokens, scale)
	if acc1 > cells-acc2 {
		acc1 = cells - acc2
	}

	return BarGeometry{
		Cells:      cells,
		InputCells: inputCells,
		Base:       cells - acc1 - acc2,
		Acc1:       acc1,
		Acc2:       acc2,
	}
}

// SplitBar lays out an already-decided integer bar width into its cell
// runs, given the interior proportions (tui/SPEC.md §3/§5). It is the
// render-time counterpart to Geometry: Geometry decides the steady-state
// total from token counts, while SplitBar splits whatever width the
// animation is currently showing (Stage D springs animate the total, and
// every frame the segments are recomputed as fractions of it, §6).
//
//   - displayCells: the current integer bar width (0 ⇒ draw nothing).
//   - inputFrac: the input/output glyph boundary as a share of the whole
//     bar (cost-based via SplitFraction, token-fallback embodied there).
//   - acc1Frac / acc2Frac: the accent slices as shares of the whole bar
//     (Accent{1,2}Tokens / TotalTokens). Their sum is ≤ 1 (accents are
//     subsets of the model's window tokens).
//   - liveAccent2: whether a live accent2 slice exists at all — the
//     minimum-visible floor below only applies when it does.
//
// Boundaries use cumulative proportional rounding (each boundary is
// round(cumulativeFraction · displayCells)), so Base+Acc1+Acc2 always
// equals displayCells exactly with no drift or gap. The accents sit at
// the right end (newest usage grows the bar rightward).
//
// Minimum-visible accent2 floor (tui/SPEC.md §5): a live accent2 that
// rounds to zero cells is forced to one, borrowed from the region to its
// left (Acc1 if present, else Base). This guarantees a request too small
// to lengthen the bar still paints one visible cell — the "a request
// landing is seen" rule — without changing the bar's overall length.
func SplitBar(displayCells int, inputFrac, acc1Frac, acc2Frac float64, liveAccent2 bool) BarGeometry {
	if displayCells <= 0 {
		return BarGeometry{}
	}

	// Cumulative right-end boundaries: base | acc1 | acc2.
	baseFrac := 1 - acc1Frac - acc2Frac
	if baseFrac < 0 {
		baseFrac = 0
	}
	baseEnd := clampCells(int(math.Round(baseFrac*float64(displayCells))), displayCells)
	acc1End := clampCells(int(math.Round((baseFrac+acc1Frac)*float64(displayCells))), displayCells)
	if acc1End < baseEnd {
		acc1End = baseEnd
	}

	base := baseEnd
	acc1 := acc1End - baseEnd
	acc2 := displayCells - acc1End

	// Minimum-visible floor: borrow one cell from the nearest left region.
	if liveAccent2 && acc2 == 0 {
		switch {
		case acc1 > 0:
			acc1--
		default:
			base--
		}
		acc2 = 1
	}

	inputCells := clampCells(int(math.Round(inputFrac*float64(displayCells))), displayCells)

	return BarGeometry{
		Cells:      displayCells,
		InputCells: inputCells,
		Base:       base,
		Acc1:       acc1,
		Acc2:       acc2,
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
