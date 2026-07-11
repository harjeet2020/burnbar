// Semantic style tokens and the glyph set (tui/SPEC.md §5). ANSI-16 only —
// the app inherits the user's terminal color scheme; no hex anywhere.
// Monochrome (NO_COLOR / dumb terminals) stays fully readable because
// meaning is always carried twice: segments by glyph difference, selection
// by prefix + bold, connection state by symbol + word.

package ui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme maps the semantic tokens of tui/SPEC.md §5 to lipgloss styles.
// Code refers to tokens, never to colors, so a future theme file (or a
// colorblind variant) is a data change, not a code change.
type Theme struct {
	// AccentPrimary: wordmark, active timeframe, selection, input segment.
	AccentPrimary lipgloss.Style
	// BarOutput: the output segment (paired with the ▒ glyph difference).
	BarOutput lipgloss.Style
	// AccentPrimaryBright: the latest burst's input share — a brighter
	// shade of the input segment it belongs to, not a separate slice
	// (tui/SPEC.md §5).
	AccentPrimaryBright lipgloss.Style
	// BarOutputBright: the latest burst's output share, symmetric with
	// AccentPrimaryBright.
	BarOutputBright lipgloss.Style
	// FadeColor: the flat ANSI-16 color for a bar's whole interior while
	// its length animation is in flight (tui/SPEC.md §6 fade) — no real
	// segment/highlight color is computed until the bar is at rest.
	// Provisional hardcoded ANSI Yellow; intended to become a configurable
	// token under the not-yet-built Stage E.1 live theme picker (§5)
	// alongside the other tokens.
	FadeColor lipgloss.Style
	// Text: primary foreground for names and values.
	Text lipgloss.Style
	// Muted: token counts, timestamps, inactive labels, hints, the scale chip.
	Muted lipgloss.Style
	// Value: emphasized numbers (per-model cost, window spend).
	Value lipgloss.Style
	// TimeframeActive / TimeframeInactive: the header window selector.
	TimeframeActive   lipgloss.Style
	TimeframeInactive lipgloss.Style
	// StatusOK / StatusWarn / StatusError: connection dot and error banner.
	StatusOK    lipgloss.Style
	StatusWarn  lipgloss.Style
	StatusError lipgloss.Style
	// Selected: the selected model's name (paired with the ▸ prefix).
	Selected lipgloss.Style
	// OverlayBorder frames the expanded help overlay.
	OverlayBorder lipgloss.Style
	// TableRule is the details screen's provider-split table header/column
	// rule (tui/SPEC.md §2 Stage E) — same muted weight as OverlayBorder,
	// deliberately without a full box: OverlayBorder is reserved for the
	// app's one transient overlay border (modal.go/overlay.go), so
	// persistent in-content structure stays visually lighter.
	TableRule lipgloss.Style
}

// DefaultTheme builds the ANSI-16 theme from tui/SPEC.md §5's token table.
func DefaultTheme() Theme {
	muted := lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	return Theme{
		AccentPrimary:       lipgloss.NewStyle().Foreground(lipgloss.Cyan),
		BarOutput:           lipgloss.NewStyle().Foreground(lipgloss.Blue),
		AccentPrimaryBright: lipgloss.NewStyle().Foreground(lipgloss.BrightCyan),
		BarOutputBright:     lipgloss.NewStyle().Foreground(lipgloss.BrightBlue),
		FadeColor:           lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		Text:                lipgloss.NewStyle(),
		Muted:               muted,
		Value:               lipgloss.NewStyle().Bold(true),
		TimeframeActive:     lipgloss.NewStyle().Foreground(lipgloss.Cyan).Reverse(true),
		TimeframeInactive:   muted,
		StatusOK:            lipgloss.NewStyle().Foreground(lipgloss.Green),
		StatusWarn:          lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		StatusError:         lipgloss.NewStyle().Foreground(lipgloss.Red),
		Selected:            lipgloss.NewStyle().Foreground(lipgloss.Cyan).Bold(true),
		OverlayBorder:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.BrightBlack).Padding(0, 2),
		TableRule:           lipgloss.NewStyle().Foreground(lipgloss.BrightBlack),
	}
}

// Glyphs is the drawing character set. Every glyph has an ASCII fallback
// (tui/SPEC.md §5) selected once at startup — no Nerd Font anywhere.
type Glyphs struct {
	// BarInput / BarOutput fill the two bar segments (█ / ▒).
	BarInput  string
	BarOutput string
	// Select prefixes the selected model row (▸).
	Select string
	// Live / Polling / Reconnecting / Offline are the connection symbols
	// (● ◌ ○ ✗), always paired with a word.
	Live         string
	Polling      string
	Reconnecting string
	Offline      string
	// MoreDown / MoreUp are the list-edge scroll indicators (▼ ▲).
	MoreDown string
	MoreUp   string
	// Wordmark is the header brand; the bar-chart motif is dropped in
	// ASCII mode.
	Wordmark string
	// Sep joins status-row segments (" · ").
	Sep string
	// Arrow joins the compact in→out token pair at the standard
	// breakpoint ("1.2M→340K").
	Arrow string
	// Ellipsis marks truncation (… / ..).
	Ellipsis string
	// ManualMark badges the status-row scale chip while a manual zoom is
	// pinned (tui/SPEC.md §3 Stage D.3): "·manual" / "*manual".
	ManualMark string
	// FracTips are the eighths-resolution partial block glyphs used for the
	// spring-animated bar's leading tip (▏▎▍▌▋▊▉, indexed eighths-1 for
	// eighths∈[1,7]); nil in ASCII mode, which stays cell-quantized
	// (tui/SPEC.md §6).
	FracTips []string
	// FadeRamp are the density-ramp glyphs (░ ▒ ▓), stepped through when a
	// bar's fade enters/exits (tui/SPEC.md §6 fade), indexed by fadeStep.
	// nil in ASCII mode — no ASCII glyph exists for "partial density," so
	// ASCII bars go solid for the whole fade instead of ramping (same
	// accepted-degradation treatment as FracTips).
	FadeRamp []string
}

// unicodeGlyphs is the standard set — block/box glyphs only, present in
// every mainstream terminal font.
var unicodeGlyphs = Glyphs{
	BarInput:     "█",
	BarOutput:    "▒",
	Select:       "▸",
	Live:         "●",
	Polling:      "◌",
	Reconnecting: "○",
	Offline:      "✗",
	MoreDown:     "▼",
	MoreUp:       "▲",
	Wordmark:     "▂▄▆ burnbar",
	Sep:          " · ",
	Arrow:        "→",
	Ellipsis:     "…",
	ManualMark:   "·manual",
	FracTips:     []string{"▏", "▎", "▍", "▌", "▋", "▊", "▉"},
	FadeRamp:     []string{"░", "▒", "▓"},
}

// asciiGlyphs is the degradation set for TERM=dumb / non-UTF-8 locales
// (tui/SPEC.md §5: `# = > * . v` plus friends).
var asciiGlyphs = Glyphs{
	BarInput:     "#",
	BarOutput:    "=",
	Select:       ">",
	Live:         "*",
	Polling:      ".",
	Reconnecting: "o",
	Offline:      "x",
	MoreDown:     "v",
	MoreUp:       "^",
	Wordmark:     "burnbar",
	Sep:          " - ",
	Arrow:        ">",
	Ellipsis:     "..",
	ManualMark:   "*manual",
	// FracTips stays nil: ASCII mode has no fractional glyphs, an accepted
	// degradation (tui/SPEC.md §6).
	// FadeRamp stays nil for the same reason: no ASCII "partial density"
	// glyph exists, so ASCII bars fade in/out solid instead of ramping.
}

// DefaultGlyphs picks the glyph set for this terminal: ASCII when the
// terminal is dumb or the locale isn't UTF-8, Unicode otherwise.
func DefaultGlyphs() Glyphs {
	if unicodeSupported() {
		return unicodeGlyphs
	}
	return asciiGlyphs
}

// unicodeSupported applies the conservative heuristic: TERM=dumb never
// gets Unicode; an explicit locale must mention UTF-8; no locale at all
// (common on modern macOS GUIs) is assumed UTF-8-capable.
func unicodeSupported() bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(key); v != "" {
			v = strings.ToUpper(strings.ReplaceAll(v, "-", ""))
			return strings.Contains(v, "UTF8")
		}
	}
	return true
}
