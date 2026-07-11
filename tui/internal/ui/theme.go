// The theme picker screen (tui/SPEC.md §5/§10 Stage E.1): an 8-row token
// list plus a cursor-dependent live preview/legend, both rendered through
// a Theme built from the *draft* palette so the preview is truthful —
// never the committed m.theme. Content taller than listHeight scrolls via
// the same flat-line viewport details.go uses (layout.go). Mirrors
// details.go's split between pure content-builders and the render/
// windowing entry point.

package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// themeLabelWidth is the fixed column width every token row's label pads
// to — the longest label, "bar.output.bright", is 18 cells.
const themeLabelWidth = 19

// themeSampleName is the synthetic model name shown in preview bars,
// picked purely for a recognizable, roughly-average-length label.
const themeSampleName = "gpt-4.1"

// renderTheme draws region 2 of the theme screen as exactly listHeight
// rows (view.go's screenTheme case), windowed the same way renderDetails
// windows the details screen's content.
func (m Model) renderTheme(l layout) []string {
	lines := m.themeContent(l)
	vp := computeViewport(len(lines), l.listHeight)
	scroll := vp.clampScroll(m.themeScroll)
	return renderViewport(lines, vp, scroll, l.listHeight, m.theme, m.glyphs)
}

// themeContent builds the full flat content-line list: 8 token rows, a
// blank separator, then the cursor-dependent preview/legend block, and
// finally an optional colorsHint line. Row chrome (cursor arrow, labels,
// default marker) renders through the live m.theme, exactly like every
// other screen's UI chrome — only each row's inline color-name swatch and
// the preview block render through a Theme freshly built from
// m.themeDraft, so what you see is exactly the candidate you're picking.
// Centered the same way detailsContent centers its block within the full
// terminal width.
func (m Model) themeContent(l layout) []string {
	g := m.glyphs
	draft := buildTheme(m.themeDraft)

	lines := make([]string, 0, len(themeTokens)+8)
	for i := range themeTokens {
		lines = append(lines, m.renderThemeRow(i, draft))
	}
	lines = append(lines, "")
	lines = append(lines, m.renderThemePreview(l, draft)...)
	if m.colorsHint != "" {
		lines = append(lines, "", m.theme.Muted.Render(" "+m.colorsHint))
	}

	margin := max(0, (m.width-maxLineWidth(lines))/2)
	pad := strings.Repeat(" ", margin)
	for i, r := range lines {
		lines[i] = truncate(pad+r, m.width, g.Ellipsis)
	}
	return lines
}

// renderThemeRow draws one token row: cursor glyph, label, the token's
// current color name (styled through the draft palette, so it's a live
// swatch), and a "· default" marker when unedited.
func (m Model) renderThemeRow(idx int, draft Theme) string {
	th, g := m.theme, m.glyphs
	info := themeTokens[idx]

	prefix := " "
	labelStyled := th.Text.Render(info.label)
	if idx == m.themeCursor {
		prefix = th.Selected.Render(g.Select)
		labelStyled = th.Selected.Render(info.label)
	}

	pad := themeLabelWidth - lipgloss.Width(info.label)
	if pad < 1 {
		pad = 1
	}

	swatchName := strings.ReplaceAll(info.get(m.themeDraft).tomlName(), "_", " ")
	swatch := themeTokenStyle(idx, draft).Render(swatchName)

	marker := ""
	if m.themeDraft.isDefault(idx) {
		marker = th.Muted.Render(g.Sep + "default")
	}

	return " " + prefix + " " + labelStyled + strings.Repeat(" ", pad) + swatch + marker
}

// themeTokenStyle maps a token row index to the Theme field its swatch
// (and the real preview) should render through — the single place row
// index ties to Theme field, mirroring themeTokens' TOML/getter wiring.
func themeTokenStyle(idx int, th Theme) lipgloss.Style {
	switch idx {
	case 0: // accent
		return th.AccentPrimary
	case 1: // bar.input
		return th.BarInput
	case 2: // bar.input.bright
		return th.AccentPrimaryBright
	case 3: // bar.output
		return th.BarOutput
	case 4: // bar.output.bright
		return th.BarOutputBright
	case 5: // bar.transition
		return th.FadeColor
	case 6: // text.primary
		return th.Text
	default: // text.muted
		return th.Muted
	}
}

// renderThemePreview builds the cursor-dependent preview/legend block,
// swapped in place rather than shown as separate static rows. It reuses
// the exact production bar-drawing code (renderResolvedBarStyled /
// renderFadingBarStyled, factored out of meter.go for this purpose) so
// the preview is never a reimplementation that could drift from what the
// real bars actually look like.
func (m Model) renderThemePreview(l layout, draft Theme) []string {
	th, g := m.theme, m.glyphs

	width := min(l.contentW-4, 36)
	if width < 8 {
		width = 8
	}

	legend := func(lines ...string) []string {
		out := make([]string, len(lines))
		for i, s := range lines {
			out[i] = th.Muted.Render(" " + s)
		}
		return out
	}

	// A synthetic 50/50 sample in token mode — deterministic and
	// independent of the app's live mode/data, purely illustrative.
	sample := core.ModelStat{Name: themeSampleName, InputTokens: 500, OutputTokens: 500, Cost: 0.42}

	switch m.themeCursor {
	case 1, 3: // bar.input, bar.output
		bar := renderResolvedBarStyled(draft, g, sample, width, core.ModeTokens, 0, 0, false)
		out := legend(
			"bar.input / bar.output — the two segments of a bar at",
			"rest, split by input vs output tokens.",
		)
		return append(out, "", " "+themeSampleName, bar)
	case 2: // bar.input.bright
		bar := renderResolvedBarStyled(draft, g, sample, width, core.ModeTokens, 0.2, 0, false)
		out := legend(
			"bar.input.bright — the brightened tail marking the",
			"input share of the most recent request.",
		)
		return append(out, "", " "+themeSampleName, bar)
	case 4: // bar.output.bright
		bar := renderResolvedBarStyled(draft, g, sample, width, core.ModeTokens, 0, 0.2, false)
		out := legend(
			"bar.output.bright — the brightened tail marking the",
			"output share of the most recent request.",
		)
		return append(out, "", " "+themeSampleName, bar)
	case 5: // bar.transition
		bar := renderFadingBarStyled(draft, g, &barAnim{fade: fadeHeld}, width, 0)
		out := legend(
			"bar.transition — the flat color shown while a bar's",
			"length is animating to a new value.",
		)
		return append(out, "", " "+themeSampleName, bar)
	default: // accent, text.primary, text.muted
		sampleLine := " " + draft.AccentPrimary.Bold(true).Render(g.Wordmark) +
			"  " + draft.TimeframeActive.Render("[today]") +
			"  " + draft.Text.Render(themeSampleName) +
			"  " + draft.Muted.Render("2m ago")
		out := legend(
			"accent / text.primary / text.muted — the wordmark and",
			"active timeframe; a value; and a muted label/timestamp.",
		)
		return append(out, "", sampleLine)
	}
}
