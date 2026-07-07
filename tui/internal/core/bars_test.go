package core

import (
	"math"
	"testing"
)

func TestScaleForLadder(t *testing.T) {
	tests := []struct {
		maxTokens int64
		want      int64
	}{
		{0, 10_000},  // empty window sits on the floor
		{-5, 10_000}, // defensive: negative input
		{1, 10_000},
		{8_000, 10_000}, // exactly 0.8·S stays (≤, not <)
		{8_001, 20_000}, // one past the headroom steps up
		{16_000, 20_000},
		{16_001, 50_000},
		{40_000, 50_000},
		{40_001, 100_000},
		{80_001, 200_000},
		{160_001, 500_000},
		{400_001, 1_000_000},
		{800_001, 2_000_000},
		{1_600_001, 5_000_000},
		{4_000_000, 5_000_000},
		{4_000_001, 10_000_000},
		{8_000_001, 20_000_000},
	}
	for _, tt := range tests {
		if got := ScaleFor(tt.maxTokens); got != tt.want {
			t.Errorf("ScaleFor(%d) = %d, want %d", tt.maxTokens, got, tt.want)
		}
	}
}

func TestScaleForHugeInputTerminates(t *testing.T) {
	// The 1–2–5 ladder tops out at 5e18 inside int64; beyond it the
	// walk must cap rather than overflow and spin forever. A hung test
	// run here means the cap regressed.
	const topLadder = int64(5_000_000_000_000_000_000)
	if got := ScaleFor(math.MaxInt64); got != topLadder {
		t.Errorf("ScaleFor(MaxInt64) = %d, want capped top ladder value %d", got, topLadder)
	}
	if got := ScaleFor(topLadder); got != topLadder {
		t.Errorf("ScaleFor(top) = %d, want %d (no headroom left above the cap)", got, topLadder)
	}
}

func TestBarWidth(t *testing.T) {
	tests := []struct {
		name          string
		width         int
		tokens, scale int64
		want          int
	}{
		{"zero tokens draw nothing", 50, 0, 100_000, 0},
		{"negative tokens draw nothing", 50, -10, 100_000, 0},
		{"zero width", 0, 1_000, 100_000, 0},
		{"zero scale", 50, 1_000, 0, 0},
		{"tiny nonzero clamps to 1 cell", 50, 1, 100_000, 1},
		{"tokens at scale fill the width", 50, 100_000, 100_000, 50},
		{"tokens beyond scale clamp to width", 50, 200_000, 100_000, 50},
		{"floors fractional cells", 50, 49_900, 100_000, 24}, // 24.95 → 24
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BarWidth(tt.width, tt.tokens, tt.scale); got != tt.want {
				t.Errorf("BarWidth(%d, %d, %d) = %d, want %d", tt.width, tt.tokens, tt.scale, got, tt.want)
			}
		})
	}
}

func TestSplitFraction(t *testing.T) {
	tests := []struct {
		name string
		m    ModelStat
		want float64
	}{
		{
			name: "cost share when both split costs reported",
			m:    ModelStat{InputTokens: 10, OutputTokens: 990, InputCost: f64(0.25), OutputCost: f64(0.75)},
			want: 0.25, // costs, NOT the 0.01 token share
		},
		{
			name: "token fallback when both costs nil",
			m:    ModelStat{InputTokens: 75, OutputTokens: 25},
			want: 0.75,
		},
		{
			name: "token fallback when both costs zero (free model)",
			m:    ModelStat{InputTokens: 60, OutputTokens: 40, InputCost: f64(0), OutputCost: f64(0)},
			want: 0.6,
		},
		{
			name: "token fallback when only one cost reported",
			m:    ModelStat{InputTokens: 50, OutputTokens: 50, InputCost: f64(0.5)},
			want: 0.5,
		},
		{
			name: "no costs and no tokens yields 0",
			m:    ModelStat{},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitFraction(tt.m); got != tt.want {
				t.Errorf("SplitFraction = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGeometry(t *testing.T) {
	// Fixed frame for most cases: width 50, scale 100K → 1 cell = 2K
	// tokens.
	const width, scale = 50, 100_000

	t.Run("no accents: base spans the whole bar", func(t *testing.T) {
		m := ModelStat{InputTokens: 30_000, OutputTokens: 30_000, InputCost: f64(1), OutputCost: f64(1)}
		g := Geometry(m, width, scale)
		if g.Cells != 30 { // 60K/100K of 50
			t.Errorf("Cells = %d, want 30", g.Cells)
		}
		if g.Base != g.Cells || g.Acc1 != 0 || g.Acc2 != 0 {
			t.Errorf("Base/Acc1/Acc2 = %d/%d/%d, want 30/0/0", g.Base, g.Acc1, g.Acc2)
		}
		if g.InputCells != 15 { // 0.5 · 30
			t.Errorf("InputCells = %d, want 15", g.InputCells)
		}
	})

	t.Run("accent2 wider than the bar clamps to it", func(t *testing.T) {
		// A 1-cell bar (min-width clamp) whose accent2 tokens alone
		// would span the full width: geometry must never let accents
		// exceed the bar.
		m := ModelStat{InputTokens: 1_000, Accent2Tokens: 100_000}
		g := Geometry(m, width, scale)
		if g.Cells != 1 || g.Acc2 != 1 || g.Acc1 != 0 || g.Base != 0 {
			t.Errorf("Cells/Acc2/Acc1/Base = %d/%d/%d/%d, want 1/1/0/0", g.Cells, g.Acc2, g.Acc1, g.Base)
		}
	})

	t.Run("accent1 clamps to what accent2 leaves", func(t *testing.T) {
		m := ModelStat{InputTokens: 10_000, Accent1Tokens: 10_000, Accent2Tokens: 6_000}
		g := Geometry(m, width, scale)
		// Cells = 5, Acc2 = 3, Acc1 wants 5 but only 2 remain.
		if g.Cells != 5 || g.Acc2 != 3 || g.Acc1 != 2 || g.Base != 0 {
			t.Errorf("Cells/Acc2/Acc1/Base = %d/%d/%d/%d, want 5/3/2/0", g.Cells, g.Acc2, g.Acc1, g.Base)
		}
	})

	t.Run("tiny accents keep their 1-cell minimum", func(t *testing.T) {
		m := ModelStat{InputTokens: 20_000, Accent1Tokens: 1, Accent2Tokens: 1}
		g := Geometry(m, width, scale)
		if g.Acc1 != 1 || g.Acc2 != 1 {
			t.Errorf("Acc1/Acc2 = %d/%d, want 1/1 (min-width accent slices)", g.Acc1, g.Acc2)
		}
		if g.Base != g.Cells-2 {
			t.Errorf("Base = %d, want Cells−2 = %d", g.Base, g.Cells-2)
		}
	})

	t.Run("input boundary rounds half away from zero", func(t *testing.T) {
		// 5 cells at a 0.5 cost split: round(2.5) = 3.
		m := ModelStat{InputTokens: 5_000, OutputTokens: 5_000, InputCost: f64(1), OutputCost: f64(1)}
		g := Geometry(m, width, 100_000)
		if g.Cells != 5 || g.InputCells != 3 {
			t.Errorf("Cells/InputCells = %d/%d, want 5/3", g.Cells, g.InputCells)
		}
	})

	t.Run("zero-token model is an all-zero geometry", func(t *testing.T) {
		if g := Geometry(ModelStat{}, width, scale); g != (BarGeometry{}) {
			t.Errorf("got %+v, want zero value", g)
		}
	})
}

func TestSplitBar(t *testing.T) {
	t.Run("zero width draws nothing", func(t *testing.T) {
		if g := SplitBar(0, 0.5, 0.2, 0.1, true); g != (BarGeometry{}) {
			t.Errorf("got %+v, want zero value", g)
		}
	})

	t.Run("no accents: base spans the whole bar", func(t *testing.T) {
		g := SplitBar(30, 0.5, 0, 0, false)
		if g.Cells != 30 || g.Base != 30 || g.Acc1 != 0 || g.Acc2 != 0 {
			t.Errorf("Cells/Base/Acc1/Acc2 = %d/%d/%d/%d, want 30/30/0/0", g.Cells, g.Base, g.Acc1, g.Acc2)
		}
		if g.InputCells != 15 { // round(0.5·30)
			t.Errorf("InputCells = %d, want 15", g.InputCells)
		}
	})

	t.Run("accents sit at the right end, proportional to their share", func(t *testing.T) {
		// 20-cell bar, 25% accent1, 10% accent2 → base 13, acc1 5, acc2 2.
		g := SplitBar(20, 0.5, 0.25, 0.10, true)
		if g.Base != 13 || g.Acc1 != 5 || g.Acc2 != 2 {
			t.Errorf("Base/Acc1/Acc2 = %d/%d/%d, want 13/5/2", g.Base, g.Acc1, g.Acc2)
		}
	})

	t.Run("cumulative rounding keeps the segments summing to the width", func(t *testing.T) {
		// The sum invariant is what makes the animated bar never gap or
		// overflow; check it across widths and awkward fractions.
		fracs := []struct{ a1, a2 float64 }{
			{0.0, 0.0}, {0.333, 0.333}, {0.1, 0.05}, {0.49, 0.49}, {0.7, 0.29},
		}
		for w := 1; w <= 60; w++ {
			for _, f := range fracs {
				g := SplitBar(w, 0.5, f.a1, f.a2, false)
				if g.Base+g.Acc1+g.Acc2 != w {
					t.Fatalf("w=%d a1=%v a2=%v: Base+Acc1+Acc2 = %d, want %d",
						w, f.a1, f.a2, g.Base+g.Acc1+g.Acc2, w)
				}
				if g.Base < 0 || g.Acc1 < 0 || g.Acc2 < 0 {
					t.Fatalf("w=%d a1=%v a2=%v: negative region %+v", w, f.a1, f.a2, g)
				}
			}
		}
	})

	t.Run("min-visible floor forces a live accent2 to 1 cell, borrowed from accent1", func(t *testing.T) {
		// acc2Frac rounds to 0 on a 20-cell bar, but a live accent2 must show.
		g := SplitBar(20, 0.5, 0.30, 0.001, true)
		if g.Acc2 != 1 {
			t.Errorf("Acc2 = %d, want 1 (min-visible floor)", g.Acc2)
		}
		if g.Base+g.Acc1+g.Acc2 != 20 {
			t.Errorf("sum = %d, want 20 (length unchanged by the floor)", g.Base+g.Acc1+g.Acc2)
		}
		// The borrowed cell comes from accent1 (present here), not base.
		plain := SplitBar(20, 0.5, 0.30, 0.001, false)
		if g.Acc1 != plain.Acc1-1 {
			t.Errorf("Acc1 = %d, want one less than the un-floored %d", g.Acc1, plain.Acc1)
		}
	})

	t.Run("floor borrows from base when there is no accent1", func(t *testing.T) {
		g := SplitBar(20, 0.5, 0, 0.001, true)
		if g.Acc2 != 1 || g.Acc1 != 0 || g.Base != 19 {
			t.Errorf("Base/Acc1/Acc2 = %d/%d/%d, want 19/0/1", g.Base, g.Acc1, g.Acc2)
		}
	})

	t.Run("a 1-cell bar with a tiny live accent2 is entirely the accent", func(t *testing.T) {
		g := SplitBar(1, 0.5, 0, 0.001, true)
		if g.Base != 0 || g.Acc1 != 0 || g.Acc2 != 1 {
			t.Errorf("Base/Acc1/Acc2 = %d/%d/%d, want 0/0/1", g.Base, g.Acc1, g.Acc2)
		}
	})

	t.Run("no floor when there is no live accent2", func(t *testing.T) {
		g := SplitBar(20, 0.5, 0.30, 0.001, false)
		if g.Acc2 != 0 {
			t.Errorf("Acc2 = %d, want 0 (no live accent2, no floor)", g.Acc2)
		}
	})
}
