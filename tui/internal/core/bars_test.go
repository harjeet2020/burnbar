package core

import (
	"math"
	"testing"
)

func TestScaleForLadder(t *testing.T) {
	tokenCases := []struct {
		max, want float64
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
	for _, tt := range tokenCases {
		if got := ScaleFor(tt.max, ModeTokens); got != tt.want {
			t.Errorf("ScaleFor(%v, ModeTokens) = %v, want %v", tt.max, got, tt.want)
		}
	}

	// Cost mode: same 1-2-5 ladder at the $0.01 floor (tui/SPEC.md §3/§11).
	// Cases stay comfortably clear of the 0.8·S boundary to avoid float
	// rounding flakiness at the floor's non-exact binary representation.
	costCases := []struct {
		max, want float64
	}{
		{0, 0.01},
		{-1, 0.01},
		{0.005, 0.01},
		{0.01, 0.02},
		{0.03, 0.05},
		{0.06, 0.1},
		{0.15, 0.2},
		{0.3, 0.5},
		{0.7, 1},
		{1.5, 2},
		{3, 5},
		{7, 10},
		{15, 20},
	}
	for _, tt := range costCases {
		if got := ScaleFor(tt.max, ModeCost); got != tt.want {
			t.Errorf("ScaleFor(%v, ModeCost) = %v, want %v", tt.max, got, tt.want)
		}
	}
}

func TestScaleForHugeInputTerminates(t *testing.T) {
	// Absurd inputs must not hang the ladder walk — scaleMaxIterations
	// caps it (float64 has no integer overflow to detect, unlike the old
	// int64 walk). BarWidth clamps to the content width regardless, so
	// the returned scale value itself isn't load-bearing here; a hung
	// test run means the cap regressed.
	for _, mode := range []Mode{ModeTokens, ModeCost} {
		got := ScaleFor(math.MaxFloat64, mode)
		if math.IsNaN(got) || math.IsInf(got, 0) || got <= 0 {
			t.Errorf("ScaleFor(MaxFloat64, %v) = %v, want a finite positive scale", mode, got)
		}
	}
}

func TestBarWidth(t *testing.T) {
	tests := []struct {
		name         string
		width        int
		value, scale float64
		want         int
	}{
		{"zero value draws nothing", 50, 0, 100_000, 0},
		{"negative value draws nothing", 50, -10, 100_000, 0},
		{"zero width", 0, 1_000, 100_000, 0},
		{"zero scale", 50, 1_000, 0, 0},
		{"tiny nonzero clamps to 1 cell", 50, 1, 100_000, 1},
		{"value at scale fills the width", 50, 100_000, 100_000, 50},
		{"value beyond scale clamps to width", 50, 200_000, 100_000, 50},
		{"floors fractional cells", 50, 49_900, 100_000, 24}, // 24.95 → 24
		{"fractional cost value", 50, 0.004821, 0.01, 24},    // 24.105 → 24
		{"tiny fractional cost clamps to 1 cell", 50, 0.00001, 0.01, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BarWidth(tt.width, tt.value, tt.scale); got != tt.want {
				t.Errorf("BarWidth(%d, %v, %v) = %d, want %d", tt.width, tt.value, tt.scale, got, tt.want)
			}
		})
	}
}

func TestSplitFraction(t *testing.T) {
	tests := []struct {
		name string
		m    ModelStat
		mode Mode
		want float64
	}{
		{
			name: "cost mode: cost share when both split costs reported",
			m:    ModelStat{InputTokens: 10, OutputTokens: 990, InputCost: f64(0.25), OutputCost: f64(0.75)},
			mode: ModeCost,
			want: 0.25, // costs, NOT the 0.01 token share
		},
		{
			name: "cost mode: token fallback when both costs nil",
			m:    ModelStat{InputTokens: 75, OutputTokens: 25},
			mode: ModeCost,
			want: 0.75,
		},
		{
			name: "cost mode: token fallback when both costs zero (free model)",
			m:    ModelStat{InputTokens: 60, OutputTokens: 40, InputCost: f64(0), OutputCost: f64(0)},
			mode: ModeCost,
			want: 0.6,
		},
		{
			name: "cost mode: token fallback when only one cost reported",
			m:    ModelStat{InputTokens: 50, OutputTokens: 50, InputCost: f64(0.5)},
			mode: ModeCost,
			want: 0.5,
		},
		{
			name: "cost mode: no costs and no tokens yields 0",
			m:    ModelStat{},
			mode: ModeCost,
			want: 0,
		},
		{
			name: "token mode: always the token ratio, cost fields ignored",
			m:    ModelStat{InputTokens: 10, OutputTokens: 990, InputCost: f64(0.25), OutputCost: f64(0.75)},
			mode: ModeTokens,
			want: 0.01,
		},
		{
			name: "token mode: no tokens yields 0 even with costs reported",
			m:    ModelStat{InputCost: f64(0.25), OutputCost: f64(0.75)},
			mode: ModeTokens,
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitFraction(tt.m, tt.mode); got != tt.want {
				t.Errorf("SplitFraction = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitBar(t *testing.T) {
	t.Run("zero width draws nothing", func(t *testing.T) {
		if g := SplitBar(0, 0.5, 0.2, 0.1); g != (BarGeometry{}) {
			t.Errorf("got %+v, want zero value", g)
		}
	})

	t.Run("no highlight: input/output split with no bright regions", func(t *testing.T) {
		g := SplitBar(30, 0.5, 0, 0)
		if g.Cells != 30 || g.InputCells != 15 || g.BrightInput != 0 || g.BrightOutput != 0 {
			t.Errorf("got %+v, want Cells=30 InputCells=15 BrightInput=0 BrightOutput=0", g)
		}
	})

	t.Run("highlight proportional inside its own segment", func(t *testing.T) {
		// 20-cell bar, 50/50 split: input zone [0,10), output zone [10,20).
		// 25% bright-input, 10% bright-output of the whole bar.
		g := SplitBar(20, 0.5, 0.25, 0.10)
		if g.InputCells != 10 || g.BrightInput != 5 || g.BrightOutput != 2 {
			t.Errorf("got %+v, want InputCells=10 BrightInput=5 BrightOutput=2", g)
		}
	})

	t.Run("bright region clamps to its own zone, never crosses into the other", func(t *testing.T) {
		// input zone is 18 cells; a bright-input frac that would compute to
		// 19 cells must clamp to 18, not bleed into the output zone.
		g := SplitBar(20, 0.9, 0.95, 0.5)
		if g.InputCells != 18 {
			t.Fatalf("InputCells = %d, want 18", g.InputCells)
		}
		if g.BrightInput != 18 {
			t.Errorf("BrightInput = %d, want 18 (clamped to the input zone)", g.BrightInput)
		}
		if g.BrightOutput != 2 {
			t.Errorf("BrightOutput = %d, want 2 (clamped to the 2-cell output zone)", g.BrightOutput)
		}
	})

	t.Run("cell counts stay in bounds across widths and fractions", func(t *testing.T) {
		fracs := []struct{ in, bIn, bOut float64 }{
			{0.5, 0, 0}, {0.5, 0.333, 0.333}, {0.1, 0.05, 0.05}, {0.9, 0.49, 0.49}, {0.3, 0.7, 0.29},
		}
		for w := 1; w <= 60; w++ {
			for _, f := range fracs {
				g := SplitBar(w, f.in, f.bIn, f.bOut)
				if g.Cells != w {
					t.Fatalf("w=%d f=%+v: Cells = %d, want %d", w, f, g.Cells, w)
				}
				if g.InputCells < 0 || g.InputCells > w {
					t.Fatalf("w=%d f=%+v: InputCells = %d out of [0,%d]", w, f, g.InputCells, w)
				}
				if g.BrightInput < 0 || g.BrightInput > g.InputCells {
					t.Fatalf("w=%d f=%+v: BrightInput = %d out of [0,%d]", w, f, g.BrightInput, g.InputCells)
				}
				outputCells := w - g.InputCells
				if g.BrightOutput < 0 || g.BrightOutput > outputCells {
					t.Fatalf("w=%d f=%+v: BrightOutput = %d out of [0,%d]", w, f, g.BrightOutput, outputCells)
				}
			}
		}
	})

	t.Run("min-visible floor: a tiny nonzero highlight still paints one cell in its own zone", func(t *testing.T) {
		g := SplitBar(20, 0.5, 0.001, 0.001)
		if g.BrightInput != 1 {
			t.Errorf("BrightInput = %d, want 1 (min-visible floor)", g.BrightInput)
		}
		if g.BrightOutput != 1 {
			t.Errorf("BrightOutput = %d, want 1 (min-visible floor)", g.BrightOutput)
		}
	})

	t.Run("floor is skipped when the input zone has zero cells", func(t *testing.T) {
		g := SplitBar(20, 0, 0.001, 0)
		if g.InputCells != 0 {
			t.Fatalf("InputCells = %d, want 0", g.InputCells)
		}
		if g.BrightInput != 0 {
			t.Errorf("BrightInput = %d, want 0 (no input zone to borrow from)", g.BrightInput)
		}
	})

	t.Run("floor is skipped when the output zone has zero cells", func(t *testing.T) {
		g := SplitBar(20, 1, 0, 0.001)
		if g.InputCells != 20 {
			t.Fatalf("InputCells = %d, want 20", g.InputCells)
		}
		if g.BrightOutput != 0 {
			t.Errorf("BrightOutput = %d, want 0 (no output zone to borrow from)", g.BrightOutput)
		}
	})

	t.Run("no floor at exactly zero fraction", func(t *testing.T) {
		g := SplitBar(20, 0.5, 0, 0)
		if g.BrightInput != 0 || g.BrightOutput != 0 {
			t.Errorf("got BrightInput=%d BrightOutput=%d, want 0/0 (frac exactly 0, no floor)", g.BrightInput, g.BrightOutput)
		}
	})
}
