// Pure-logic tests for the theme picker's color vocabulary (tui/SPEC.md
// §5 Stage E.1): the ANSI-16 name round-trip, hue/bright wrap, and the
// per-token fallback resolveThemeColors applies against a raw
// data.ColorsConfig.

package ui

import (
	"strings"
	"testing"

	"github.com/harjeet2020/burnbar/tui/internal/data"
)

func TestAnsiColor_TomlNameRoundTrip(t *testing.T) {
	for hue := 0; hue < 8; hue++ {
		for _, bright := range []bool{false, true} {
			c := ansiColor{hue: hue, bright: bright}
			name := c.tomlName()
			got, ok := parseANSIColor(name)
			if !ok {
				t.Fatalf("parseANSIColor(%q) ok = false, want true", name)
			}
			if got != c {
				t.Fatalf("parseANSIColor(%q) = %+v, want %+v", name, got, c)
			}
		}
	}
}

func TestParseANSIColor_RejectsInvalid(t *testing.T) {
	cases := []string{"", "mauve", "#ff00ff", "bright_", "bright_mauve", "Cyan", "cyan "}
	for _, s := range cases {
		if _, ok := parseANSIColor(s); ok {
			t.Errorf("parseANSIColor(%q) ok = true, want false (scope guard: ANSI-16 only, never hex)", s)
		}
	}
}

func TestAnsiColor_CycledWrapsBothDirections(t *testing.T) {
	c := ansiColor{hue: 0}
	if got := c.cycled(-1); got.hue != 7 {
		t.Errorf("cycled(-1) from hue 0 = %d, want 7", got.hue)
	}
	c = ansiColor{hue: 7}
	if got := c.cycled(1); got.hue != 0 {
		t.Errorf("cycled(1) from hue 7 = %d, want 0", got.hue)
	}
	// bright is untouched by cycling.
	c = ansiColor{hue: 3, bright: true}
	if got := c.cycled(2); !got.bright {
		t.Errorf("cycled(2).bright = false, want true (unchanged)")
	}
}

func TestAnsiColor_ToggledBrightIndependentOfHue(t *testing.T) {
	c := ansiColor{hue: 5, bright: false}
	got := c.toggledBright()
	if !got.bright || got.hue != 5 {
		t.Fatalf("toggledBright() = %+v, want {hue:5 bright:true}", got)
	}
	got = got.toggledBright()
	if got.bright || got.hue != 5 {
		t.Fatalf("toggledBright() twice = %+v, want {hue:5 bright:false}", got)
	}
}

func TestResolveThemeColors_EmptyConfigYieldsDefaults(t *testing.T) {
	got, hint := resolveThemeColors(data.ColorsConfig{})
	if hint != "" {
		t.Errorf("hint = %q, want \"\" for an empty config (missing is not invalid)", hint)
	}
	if got != defaultThemeColors() {
		t.Errorf("resolveThemeColors(empty) = %+v, want defaults", got)
	}
}

func TestResolveThemeColors_ValidOverridesApplied(t *testing.T) {
	raw := data.ColorsConfig{
		Accent:   "red",
		BarInput: "bright_magenta",
	}
	got, hint := resolveThemeColors(raw)
	if hint != "" {
		t.Fatalf("hint = %q, want \"\" for valid overrides", hint)
	}
	if got.Accent != (ansiColor{hue: 1}) {
		t.Errorf("Accent = %+v, want red", got.Accent)
	}
	if got.BarInput != (ansiColor{hue: 5, bright: true}) {
		t.Errorf("BarInput = %+v, want bright magenta", got.BarInput)
	}
	// Untouched tokens keep their defaults.
	if got.BarOutput != defaultThemeColors().BarOutput {
		t.Errorf("BarOutput = %+v, want default (untouched by raw config)", got.BarOutput)
	}
}

func TestResolveThemeColors_OneInvalidTokenFallsBackAlone(t *testing.T) {
	raw := data.ColorsConfig{
		Accent:   "mauve", // invalid
		BarInput: "green", // valid
	}
	got, hint := resolveThemeColors(raw)
	if got.Accent != defaultThemeColors().Accent {
		t.Errorf("Accent = %+v, want default fallback for an invalid value", got.Accent)
	}
	if got.BarInput != (ansiColor{hue: 2}) {
		t.Errorf("BarInput = %+v, want green (valid value still applied)", got.BarInput)
	}
	if !strings.Contains(hint, "accent") {
		t.Errorf("hint = %q, want it to name the invalid token (accent)", hint)
	}
	if strings.Contains(hint, "bar_input") {
		t.Errorf("hint = %q, should not name bar_input (it was valid)", hint)
	}
}

func TestThemeColors_CycledAndToggledBright_TargetOnlySelectedToken(t *testing.T) {
	c := defaultThemeColors()
	idx := 3 // bar.output
	got := c.cycled(idx, 1)
	if got.BarOutput == c.BarOutput {
		t.Errorf("cycled token's value unchanged")
	}
	// Every other field must be untouched.
	got.BarOutput = c.BarOutput // reset the one field we expect to differ
	if got != c {
		t.Errorf("cycled(idx=3, 1) touched a field other than BarOutput")
	}

	got2 := c.toggledBright(idx)
	if !got2.BarOutput.bright {
		t.Errorf("toggledBright(3).BarOutput.bright = false, want true")
	}
	got2.BarOutput = c.BarOutput
	if got2 != c {
		t.Errorf("toggledBright(idx=3) touched a field other than BarOutput")
	}
}

func TestThemeColors_IsDefault(t *testing.T) {
	c := defaultThemeColors()
	for i := range themeTokens {
		if !c.isDefault(i) {
			t.Errorf("isDefault(%d) = false for an untouched default palette", i)
		}
	}
	edited := c.cycled(0, 1)
	if edited.isDefault(0) {
		t.Errorf("isDefault(0) = true after cycling token 0")
	}
	if !edited.isDefault(1) {
		t.Errorf("isDefault(1) = false — cycling token 0 should not affect token 1")
	}
}

func TestColorsConfigFrom_RoundTripsThroughResolve(t *testing.T) {
	c := defaultThemeColors().cycled(4, 3).toggledBright(6)
	raw := colorsConfigFrom(c)
	got, hint := resolveThemeColors(raw)
	if hint != "" {
		t.Fatalf("hint = %q, want \"\" round-tripping a valid palette", hint)
	}
	if got != c {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, c)
	}
}
