// The picker's color vocabulary (Stage E.1, tui/SPEC.md §5): the ANSI-16
// (hue, bright) pair, the 8-token palette it's applied to, and the
// registry that wires each token to its TOML key, display label, and
// Theme field. ANSI-16 only, never hex — a hex value would let the app
// clash with the terminal theme instead of inheriting it, the exact
// pitfall the ANSI-16 default exists to avoid.

package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/harjeet2020/burnbar/tui/internal/data"
)

// ansiColor is one of the 16 ANSI-16 colors as a (hue, bright) pair over
// the fixed ANSI order black/red/green/yellow/blue/magenta/cyan/white.
type ansiColor struct {
	hue    int
	bright bool
}

// hueNames is the ANSI order's TOML/display vocabulary.
var hueNames = [8]string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

// hueLipgloss maps [hue][bright] to lipgloss's ANSI-16 constant, written
// out explicitly (not derived by const-arithmetic across packages) so it
// stays correct even if lipgloss's own const block is ever reordered.
var hueLipgloss = [8][2]ansi.BasicColor{
	{lipgloss.Black, lipgloss.BrightBlack},
	{lipgloss.Red, lipgloss.BrightRed},
	{lipgloss.Green, lipgloss.BrightGreen},
	{lipgloss.Yellow, lipgloss.BrightYellow},
	{lipgloss.Blue, lipgloss.BrightBlue},
	{lipgloss.Magenta, lipgloss.BrightMagenta},
	{lipgloss.Cyan, lipgloss.BrightCyan},
	{lipgloss.White, lipgloss.BrightWhite},
}

func (c ansiColor) lipgloss() ansi.BasicColor {
	if c.bright {
		return hueLipgloss[c.hue][1]
	}
	return hueLipgloss[c.hue][0]
}

// tomlName is the bidirectional TOML/display encoding: "cyan" for a base
// hue, "bright_cyan" for its bright variant.
func (c ansiColor) tomlName() string {
	if c.bright {
		return "bright_" + hueNames[c.hue]
	}
	return hueNames[c.hue]
}

// parseANSIColor is tomlName's inverse; ok is false for anything else,
// including hex — the scope guard tui/SPEC.md §5 requires.
func parseANSIColor(s string) (ansiColor, bool) {
	name, bright := s, false
	if rest, ok := strings.CutPrefix(s, "bright_"); ok {
		name, bright = rest, true
	}
	for hue, n := range hueNames {
		if n == name {
			return ansiColor{hue: hue, bright: bright}, true
		}
	}
	return ansiColor{}, false
}

// cycled steps the hue by delta, wrapping both directions; bright is
// unchanged.
func (c ansiColor) cycled(delta int) ansiColor {
	return ansiColor{hue: ((c.hue+delta)%8 + 8) % 8, bright: c.bright}
}

// toggledBright flips the bright bit; hue is unchanged.
func (c ansiColor) toggledBright() ansiColor {
	return ansiColor{hue: c.hue, bright: !c.bright}
}

// themeColors is the flat, directly-editable palette — one ansiColor per
// remappable token, in the picker's fixed display order (tui/SPEC.md §5
// Stage E.1). This, not Theme itself, is what Model.themeDraft holds:
// cycling/toggling/resetting 8 plain (hue,bright) pairs is far simpler
// and less error-prone than mutating lipgloss.Style.Foreground calls
// directly. buildTheme (styles.go) turns a themeColors into the real
// Theme every render uses.
type themeColors struct {
	Accent, BarInput, BarInputBright, BarOutput, BarOutputBright,
	BarTransition, TextPrimary, TextMuted ansiColor
}

// defaultThemeColors are the built-in defaults — bit-for-bit what
// DefaultTheme() hardcoded before Stage E.1, except TextPrimary, which is
// promoted from "no explicit color" to literal white so every token has a
// concrete ANSI-16 default to seed the picker's cycle from.
func defaultThemeColors() themeColors {
	return themeColors{
		Accent:          ansiColor{hue: 6},               // cyan
		BarInput:        ansiColor{hue: 6},               // cyan
		BarInputBright:  ansiColor{hue: 6, bright: true}, // bright cyan
		BarOutput:       ansiColor{hue: 4},               // blue
		BarOutputBright: ansiColor{hue: 4, bright: true}, // bright blue
		BarTransition:   ansiColor{hue: 3},               // yellow
		TextPrimary:     ansiColor{hue: 7},               // white
		TextMuted:       ansiColor{hue: 0, bright: true}, // bright black
	}
}

// themeTokenInfo describes one of the 8 picker rows: its TOML key,
// display label, and a getter/setter pair on themeColors — the single
// place that wires "row N" to "struct field" for every consumer (picker
// rows, config read/write, the theme builder).
type themeTokenInfo struct {
	tomlKey string
	label   string
	get     func(themeColors) ansiColor
	set     func(*themeColors, ansiColor)
}

// themeTokens is the fixed picker display order (tui/SPEC.md §5 Stage
// E.1): accent, bar.input, bar.input.bright, bar.output,
// bar.output.bright, bar.transition, text.primary, text.muted.
var themeTokens = [8]themeTokenInfo{
	{"accent", "accent",
		func(c themeColors) ansiColor { return c.Accent },
		func(c *themeColors, v ansiColor) { c.Accent = v }},
	{"bar_input", "bar.input",
		func(c themeColors) ansiColor { return c.BarInput },
		func(c *themeColors, v ansiColor) { c.BarInput = v }},
	{"bar_input_bright", "bar.input.bright",
		func(c themeColors) ansiColor { return c.BarInputBright },
		func(c *themeColors, v ansiColor) { c.BarInputBright = v }},
	{"bar_output", "bar.output",
		func(c themeColors) ansiColor { return c.BarOutput },
		func(c *themeColors, v ansiColor) { c.BarOutput = v }},
	{"bar_output_bright", "bar.output.bright",
		func(c themeColors) ansiColor { return c.BarOutputBright },
		func(c *themeColors, v ansiColor) { c.BarOutputBright = v }},
	{"bar_transition", "bar.transition",
		func(c themeColors) ansiColor { return c.BarTransition },
		func(c *themeColors, v ansiColor) { c.BarTransition = v }},
	{"text_primary", "text.primary",
		func(c themeColors) ansiColor { return c.TextPrimary },
		func(c *themeColors, v ansiColor) { c.TextPrimary = v }},
	{"text_muted", "text.muted",
		func(c themeColors) ansiColor { return c.TextMuted },
		func(c *themeColors, v ansiColor) { c.TextMuted = v }},
}

// cycled returns c with token idx's hue stepped by delta.
func (c themeColors) cycled(idx, delta int) themeColors {
	themeTokens[idx].set(&c, themeTokens[idx].get(c).cycled(delta))
	return c
}

// toggledBright returns c with token idx's bright bit flipped.
func (c themeColors) toggledBright(idx int) themeColors {
	themeTokens[idx].set(&c, themeTokens[idx].get(c).toggledBright())
	return c
}

// isDefault reports whether token idx's current value in c matches its
// built-in default — the picker's "· default" row marker.
func (c themeColors) isDefault(idx int) bool {
	return themeTokens[idx].get(c) == themeTokens[idx].get(defaultThemeColors())
}

// resolveThemeColors turns a raw, on-disk ColorsConfig into a themeColors,
// falling back to the built-in default per token on a missing or invalid
// value — never a hard failure (tui/SPEC.md §7). hint names every token
// that fell back due to an invalid (not merely absent) value, "" when
// nothing was invalid.
func resolveThemeColors(raw data.ColorsConfig) (themeColors, string) {
	defaults := defaultThemeColors()
	result := defaults
	var invalid []string

	values := map[string]string{
		"accent":            raw.Accent,
		"bar_input":         raw.BarInput,
		"bar_input_bright":  raw.BarInputBright,
		"bar_output":        raw.BarOutput,
		"bar_output_bright": raw.BarOutputBright,
		"bar_transition":    raw.BarTransition,
		"text_primary":      raw.TextPrimary,
		"text_muted":        raw.TextMuted,
	}

	for _, info := range themeTokens {
		v := values[info.tomlKey]
		if v == "" {
			continue // absent — default already in place, not an error
		}
		parsed, ok := parseANSIColor(v)
		if !ok {
			invalid = append(invalid, info.tomlKey)
			continue
		}
		info.set(&result, parsed)
	}

	if len(invalid) == 0 {
		return result, ""
	}
	label := "color"
	if len(invalid) > 1 {
		label = "colors"
	}
	hint := "invalid " + label + " for " + strings.Join(invalid, ", ") + " — using default"
	return result, hint
}

// colorsConfigFrom is resolveThemeColors's inverse — used when saving.
func colorsConfigFrom(c themeColors) data.ColorsConfig {
	return data.ColorsConfig{
		Accent:          c.Accent.tomlName(),
		BarInput:        c.BarInput.tomlName(),
		BarInputBright:  c.BarInputBright.tomlName(),
		BarOutput:       c.BarOutput.tomlName(),
		BarOutputBright: c.BarOutputBright.tomlName(),
		BarTransition:   c.BarTransition.tomlName(),
		TextPrimary:     c.TextPrimary.tomlName(),
		TextMuted:       c.TextMuted.tomlName(),
	}
}
