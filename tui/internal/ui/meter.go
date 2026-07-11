// The meter screen: header, bars list, status row, hint row — the four
// fixed regions of tui/SPEC.md §2. All functions here are pure renderers
// over (Model, layout); geometry decisions live in layout.go and
// internal/core.

package ui

import (
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// leftRight composes a full-width row: left content, right content, the
// gap padded with spaces, and 1-cell side margins. When the two collide
// the right side wins (it holds values; the left holds labels).
func leftRight(width int, left, right string) string {
	pad := width - 2 - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	row := " " + left + strings.Repeat(" ", pad) + right
	return truncate(row, width, "")
}

// renderHeader draws the three header rows (wordmark + credits; a blank
// spacer; timeframe selector + window spend) — the spacer separates the
// two bands visually (tui/SPEC.md §2 Stage D.1).
func (m Model) renderHeader() string {
	th, g := m.theme, m.glyphs

	credits := "—"
	if m.snap.Credits != nil {
		credits = core.FormatCredits(*m.snap.Credits)
	}
	creditsRight := th.Muted.Render("credits  ") + th.Text.Render(credits)
	if m.snap.Credits != nil {
		creditsRight += th.Muted.Render(g.Sep + core.FormatAge(time.Since(m.snap.CreditsAt)))
	}
	if m.creditsHint != "" {
		creditsRight += th.Muted.Render(g.Sep + m.creditsHint)
	}
	row1 := leftRight(m.width, th.AccentPrimary.Bold(true).Render(g.Wordmark), creditsRight)

	selector, _ := m.timeframeSelector()
	spendLabel, spendValue := "spent  ", core.FormatCost(m.snap.Spend)
	if m.mode == core.ModeTokens {
		spendLabel, spendValue = "used  ", core.FormatTokens(m.snap.TotalTokens)
	}
	spendRight := th.Muted.Render(spendLabel) + th.Value.Render(spendValue)
	row2 := leftRight(m.width, selector, spendRight)

	return row1 + "\n\n" + row2
}

// timeframeSelector renders "[today]  week  month" (active label reverse
// video + brackets, so state survives monochrome) and reports each
// label's clickable cell span on header row 2.
func (m Model) timeframeSelector() (string, []tfRange) {
	th := m.theme
	var styled []string
	var ranges []tfRange
	x := 1 // after the left margin
	for i, tf := range core.Timeframes {
		if i > 0 {
			x += 2
		}
		text := tf.Label()
		if tf == m.tf {
			text = "[" + text + "]"
			styled = append(styled, th.TimeframeActive.Render(text))
		} else {
			styled = append(styled, th.TimeframeInactive.Render(text))
		}
		w := lipgloss.Width(text)
		ranges = append(ranges, tfRange{tf: tf, x0: x, x1: x + w})
		x += w
	}
	return strings.Join(styled, "  "), ranges
}

// renderBars draws region 2 of the meter screen as exactly
// layout.listHeight rows. The list always reserves one row at each end
// for "N more" indicators (blank when nothing overflows in that
// direction) and always separates blocks with a blank row — a fixed
// rhythm that never jitters as the window resizes (tui/SPEC.md §2/§4).
func (m Model) renderBars(l layout) []string {
	rows := make([]string, 0, l.listHeight)
	models := m.snap.Models

	if len(models) == 0 {
		// Distinguish "still loading the first baseline" from a genuinely
		// empty window (tui/SPEC.md §7 startup, §8 empty state).
		text := "no usage in this window yet"
		if m.loading {
			text = "connecting…"
			if m.glyphs.Arrow == ">" { // ASCII mode
				text = "connecting..."
			}
		}
		msg := m.theme.Muted.Render(text)
		block := lipgloss.Place(m.width, l.listHeight, lipgloss.Center, lipgloss.Center, msg)
		return strings.Split(block, "\n")
	}

	scroll := l.clampScroll(m.scroll)
	first := scroll
	last := scroll + l.visible
	if last > len(models) {
		last = len(models)
	}

	above := ""
	if scroll > 0 {
		above = m.theme.Muted.Render(" " + m.glyphs.MoreUp + " " + strconv.Itoa(scroll) + " more")
	}
	rows = append(rows, above)

	scale := m.scale()
	selIdx := m.selectedIndex()
	for i := first; i < last; i++ {
		if i > first {
			rows = append(rows, "") // spacer between blocks, always on
		}
		rows = append(rows, m.renderLabelRow(models[i], i == selIdx, l))
		rows = append(rows, m.renderBarRow(models[i], scale, l))
	}

	below := ""
	if remaining := len(models) - last; remaining > 0 {
		below = m.theme.Muted.Render(" " + m.glyphs.MoreDown + " " + strconv.Itoa(remaining) + " more")
	}
	rows = append(rows, below)

	for len(rows) < l.listHeight {
		rows = append(rows, "")
	}
	return rows[:l.listHeight]
}

// renderLabelRow draws a model's label line: name left; the other
// denomination and the active mode's bold value right-aligned — the active
// mode's value is the row anchor, mirroring the sort order (tui/SPEC.md §3).
func (m Model) renderLabelRow(st core.ModelStat, selected bool, l layout) string {
	th, g := m.theme, m.glyphs

	var anchor, secondary string
	if m.mode == core.ModeTokens {
		anchor = core.FormatTokens(st.TotalTokens())
		switch l.bp {
		case bpWide, bpStandard:
			secondary = core.FormatCost(st.Cost)
		}
	} else {
		anchor = core.FormatCost(st.Cost)
		switch l.bp {
		case bpWide:
			secondary = core.FormatTokens(st.InputTokens) + " in" + g.Sep + core.FormatTokens(st.OutputTokens) + " out"
		case bpStandard:
			secondary = core.FormatTokens(st.TotalTokens())
		}
	}

	right := th.Value.Render(anchor)
	rightW := lipgloss.Width(anchor)
	if secondary != "" {
		right = th.Muted.Render(secondary) + "  " + right
		rightW += lipgloss.Width(secondary) + 2
	}

	maxName := l.contentW - rightW - 2
	name := truncateName(st.Name, maxName, g.Ellipsis)

	prefix := " "
	nameStyled := th.Text.Render(name)
	if selected {
		prefix = th.Selected.Render(g.Select)
		nameStyled = th.Selected.Render(name)
	}

	pad := m.width - 1 - lipgloss.Width(name) - rightW - 1
	if pad < 1 {
		pad = 1
	}
	return prefix + nameStyled + strings.Repeat(" ", pad) + right
}

// renderBarRow draws a model's bar. While its length animation is in flight
// (entering/held/exiting a fade, tui/SPEC.md §6) it renders as one uniform
// run in the fade color with no split and no burst highlight; only once the
// bar is at rest does it compute and render the real split/highlight
// geometry. This also means core.SplitBar is never called against a
// moving width anymore — the highlight boundary can no longer be
// recomputed against a live, changing whole, which is what made it flicker
// before the fade existed.
func (m Model) renderBarRow(st core.ModelStat, scale float64, l layout) string {
	target := core.BarWidth(l.contentW, st.Value(m.mode), scale)

	a := m.anim[st.Name]
	if a != nil && (a.fade != fadeNone || !a.settledAt(float64(target))) {
		whole, eighths := m.barDisplayEighths(st.Name, target, l.contentW)
		return m.renderFadingBar(a, whole, eighths)
	}
	return m.renderResolvedBar(st, target)
}

// renderFadingBar draws a bar mid-length-animation: whole solid-fill cells
// plus the existing fractional leading tip (barDisplayEighths, unchanged),
// all in one flat ANSI-16 color (th.FadeColor) — no segment split, no
// burst highlight. The entering/exiting sub-phases additionally swap the
// fill glyph through the density ramp (░▒▓) instead of the solid glyph, in
// Unicode mode only; ASCII/NO_COLOR mode has no distinct density glyphs and
// stays solid for the whole fade (an accepted degradation, matching
// FracTips's existing ASCII fallback).
func (m Model) renderFadingBar(a *barAnim, whole, eighths int) string {
	return renderFadingBarStyled(m.theme, m.glyphs, a, whole, eighths)
}

// renderFadingBarStyled is renderFadingBar's pure body, factored out
// (Stage E.1) so the theme picker's preview can call it directly against a
// draft palette instead of the live m.theme — the exact same bar-drawing
// code path, not a reimplementation.
func renderFadingBarStyled(th Theme, g Glyphs, a *barAnim, whole, eighths int) string {
	if whole == 0 && eighths == 0 {
		return ""
	}

	glyph := g.BarInput
	if a.fade != fadeHeld && g.FadeRamp != nil {
		step := a.fadeStep
		if a.fade == fadeExiting {
			step = fadeRampFrames - 1 - step
		}
		// fadeRampFrames can run longer than there are density glyphs (it
		// was doubled independently of the fixed ░▒▓ set), so scale the
		// frame index down into the glyph range — each glyph then holds
		// for an even share of the ramp instead of indexing out of range.
		glyph = g.FadeRamp[step*len(g.FadeRamp)/fadeRampFrames]
	}

	var b strings.Builder
	b.WriteString(" ")
	b.WriteString(th.FadeColor.Render(strings.Repeat(glyph, whole)))
	if eighths > 0 && g.FracTips != nil {
		b.WriteString(th.FadeColor.Render(g.FracTips[eighths-1]))
	}
	return b.String()
}

// renderResolvedBar draws a bar at rest: the real input/output split with
// the latest-burst highlight (tui/SPEC.md §3/§5). Only ever called once a
// bar's spring and fade have both fully settled, so target is always the
// final, non-moving width.
func (m Model) renderResolvedBar(st core.ModelStat, target int) string {
	var burstInput, burstOutput float64
	if m.burst != nil && m.burst.Model == st.Name {
		if denom := st.Value(m.mode); denom > 0 {
			burstInput = m.burst.InputValue(m.mode) / denom
			burstOutput = m.burst.OutputValue(m.mode) / denom
		}
	}
	pulsing := m.burst != nil && m.burst.Model == st.Name && time.Now().Before(m.accentEmphasisUntil)
	return renderResolvedBarStyled(m.theme, m.glyphs, st, target, m.mode, burstInput, burstOutput, pulsing)
}

// renderResolvedBarStyled is renderResolvedBar's pure body, factored out
// (Stage E.1) so the theme picker's preview can call it directly against a
// draft palette and a synthetic burst share — the exact same bar-drawing
// code path (glyph selection, cell-run styling, bright-region math), not a
// reimplementation. burstInputFrac/burstOutputFrac are the already-resolved
// shares (renderResolvedBar computes these from m.burst; the preview
// supplies them directly, since it has no real burst to derive from).
func renderResolvedBarStyled(th Theme, g Glyphs, st core.ModelStat, target int, mode core.Mode, burstInputFrac, burstOutputFrac float64, pulsing bool) string {
	inputFrac := core.SplitFraction(st, mode)
	geo := core.SplitBar(target, inputFrac, burstInputFrac, burstOutputFrac)
	if geo.Cells == 0 {
		return ""
	}

	// The glyph pattern (input vs output) spans the whole bar; the burst
	// highlight recolors the trailing cells of its own segment without
	// changing glyphs, so the split stays readable in monochrome.
	glyphAt := func(i int) string {
		if i < geo.InputCells {
			return g.BarInput
		}
		return g.BarOutput
	}
	run := func(from, to int, style lipgloss.Style) string {
		if to <= from {
			return ""
		}
		var b strings.Builder
		for i := from; i < to; i++ {
			b.WriteString(glyphAt(i))
		}
		return style.Render(b.String())
	}

	// A freshly-arrived burst renders bold for accentEmphasis — the arrival
	// signal, and the only way a sub-cell tiny request is seen (§6).
	brightIn, brightOut := th.AccentPrimaryBright, th.BarOutputBright
	if pulsing {
		brightIn, brightOut = brightIn.Bold(true), brightOut.Bold(true)
	}

	plainInputEnd := geo.InputCells - geo.BrightInput
	plainOutputEnd := geo.Cells - geo.BrightOutput

	var b strings.Builder
	b.WriteString(" ")
	b.WriteString(run(0, plainInputEnd, th.BarInput))
	b.WriteString(run(plainInputEnd, geo.InputCells, brightIn))
	b.WriteString(run(geo.InputCells, plainOutputEnd, th.BarOutput))
	b.WriteString(run(plainOutputEnd, geo.Cells, brightOut))
	return b.String()
}

// renderStatus draws the status row, degrading in fixed variants until
// one fits the width: full → no lag → compact → connection only
// (tui/SPEC.md §2 content, §4 pressure).
func (m Model) renderStatus(width int) string {
	th, g := m.theme, m.glyphs

	conn := m.renderConn()
	scaleVal := m.scale()
	scale := "scale " + core.FormatCost(scaleVal)
	if m.mode == core.ModeTokens {
		scale = "scale " + core.FormatTokens(int64(math.Round(scaleVal)))
	}
	if m.manualScale != nil {
		scale += " " + g.ManualMark
	}
	source := "source " + m.sourceLabel()

	lastFull, lastNoWord, lastCompact := "last request —", "last —", "last —"
	if !m.snap.LastRequestAt.IsZero() {
		clock := core.FormatClock(m.snap.LastRequestAt)
		age := core.FormatRelative(time.Since(m.snap.LastRequestAt))
		lastFull = "last request " + clock + " " + age
		lastNoWord = "last " + clock + " " + age
		lastCompact = "last " + clock
	}

	lag := "lag —"
	if m.snap.LagSeconds != nil {
		lag = "lag " + core.FormatDuration(time.Duration(*m.snap.LagSeconds*float64(time.Second)))
	}

	// When a fetch is failing but stale data is still shown, badge its age
	// so the numbers aren't mistaken for live (tui/SPEC.md §8). It rides on
	// the fullest variant and drops first under width pressure.
	first := lastFull
	if m.dataErr != "" && !m.dataAt.IsZero() {
		first = "data from " + core.FormatClock(m.dataAt) + g.Sep + lastFull
	}

	variants := [][]string{
		{first, lag, source, scale, conn},
		{lastFull, source, scale, conn},
		{lastNoWord, scale, conn},  // drop the "request" word only; clock+age stay
		{lastCompact, scale, conn}, // now shrink fully, closer to the width floor
		{conn},
	}
	for _, v := range variants {
		row := " " + th.Muted.Render(strings.Join(v[:len(v)-1], g.Sep))
		if len(v) > 1 {
			row += th.Muted.Render(g.Sep)
		}
		row += v[len(v)-1] // connection state keeps its own color
		if lipgloss.Width(row) <= width-1 {
			return row
		}
	}
	return " " + conn
}

// sourceLabel names the active live source for the status row — "realtime"
// or "poll" — so the `p` toggle's effect is visible independent of the
// connection chip (which shows health, not which source is running).
func (m Model) sourceLabel() string {
	if m.live != nil && m.live.Name() == "realtime" {
		return "realtime"
	}
	return "poll"
}

// renderConn draws the connection state — symbol + word, colored but
// never color-alone (tui/SPEC.md §2).
func (m Model) renderConn() string {
	th, g := m.theme, m.glyphs
	switch m.snap.Conn {
	case core.ConnPolling:
		return th.StatusWarn.Render(g.Polling + " polling")
	case core.ConnReconnecting:
		return th.StatusWarn.Render(g.Reconnecting + " reconnecting…")
	case core.ConnOffline:
		return th.StatusError.Render(g.Offline + " offline")
	default:
		return th.StatusOK.Render(g.Live + " live")
	}
}

// renderHints draws the bottom row: the context hint bar, in priority
// order (tui/SPEC.md §2/§4).
func (m Model) renderHints(l layout) string {
	entries := m.keys.MeterHints()
	switch m.scr {
	case screenDetails:
		entries = m.keys.DetailsHints()
	case screenTheme:
		entries = m.keys.ThemeHints()
	}
	return " " + renderPriorityHints(m.theme, m.glyphs.Sep, m.width-1, entries)
}

// renderPriorityHints renders every entry in its fixed display order,
// dropping removable entries (ascending rank — least essential first)
// until the row fits width; the protected core (rank hintCore) is never
// dropped, even if the row still overflows once only core remains.
// Replaces the help bubble's blunt "…" truncation, which could hide the
// entire hint row on a narrow terminal (tui/SPEC.md §2 Stage D.1).
func renderPriorityHints(th Theme, sep string, width int, entries []hintEntry) string {
	hint := func(b key.Binding) string {
		return th.Text.Render(b.Help().Key) + " " + th.Muted.Render(b.Help().Desc)
	}
	sepStyled := th.Muted.Render(sep)

	present := make([]bool, len(entries))
	for i := range present {
		present[i] = true
	}
	render := func() string {
		parts := make([]string, 0, len(entries))
		for i, e := range entries {
			if present[i] {
				parts = append(parts, hint(e.binding))
			}
		}
		return strings.Join(parts, sepStyled)
	}

	row := render()
	for lipgloss.Width(row) > width {
		drop := -1
		for i, e := range entries {
			if !present[i] || e.rank == hintCore {
				continue
			}
			if drop == -1 || e.rank < entries[drop].rank {
				drop = i
			}
		}
		if drop == -1 {
			break // only the protected core remains
		}
		present[drop] = false
		row = render()
	}
	return row
}
