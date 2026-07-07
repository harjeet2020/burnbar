// The meter screen: header, bars list, status row, hint row — the four
// fixed regions of tui/SPEC.md §2. All functions here are pure renderers
// over (Model, layout); geometry decisions live in layout.go and
// internal/core.

package ui

import (
	"strconv"
	"strings"
	"time"

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

// renderHeader draws the two header rows (wordmark + credits; timeframe
// selector + window spend).
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
	spendRight := th.Muted.Render("spent  ") + th.Value.Render(core.FormatCost(m.snap.Spend))
	row2 := leftRight(m.width, selector, spendRight)

	return row1 + "\n" + row2
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
// layout.listHeight rows.
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
	first, last := 0, len(models)
	if l.scrolling {
		first, last = scroll, scroll+l.visible
		above := ""
		if scroll > 0 {
			above = m.theme.Muted.Render(" " + m.glyphs.MoreUp + " " + strconv.Itoa(scroll) + " more")
		}
		rows = append(rows, above)
	}

	scale := m.scale()
	selIdx := m.selectedIndex()
	for i := first; i < last && i < len(models); i++ {
		if l.spacers {
			rows = append(rows, "") // header gap, then block separators
		}
		rows = append(rows, m.renderLabelRow(models[i], i == selIdx, l))
		rows = append(rows, m.renderBarRow(models[i], scale, l))
	}

	if l.scrolling {
		below := ""
		if remaining := len(models) - (scroll + l.visible); remaining > 0 {
			below = m.theme.Muted.Render(" " + m.glyphs.MoreDown + " " + strconv.Itoa(remaining) + " more")
		}
		rows = append(rows, below)
	}

	for len(rows) < l.listHeight {
		rows = append(rows, "")
	}
	return rows[:l.listHeight]
}

// renderLabelRow draws a model's label line: name left; tokens (per
// breakpoint) and bold cost right-aligned — the cost column is the row
// anchor (tui/SPEC.md §3).
func (m Model) renderLabelRow(st core.ModelStat, selected bool, l layout) string {
	th, g := m.theme, m.glyphs

	cost := core.FormatCost(st.Cost)
	tokens := ""
	switch l.bp {
	case bpWide:
		tokens = core.FormatTokens(st.InputTokens) + " in" + g.Sep + core.FormatTokens(st.OutputTokens) + " out"
	case bpStandard:
		tokens = core.FormatTokens(st.InputTokens) + g.Arrow + core.FormatTokens(st.OutputTokens)
	}

	right := th.Value.Render(cost)
	rightW := lipgloss.Width(cost)
	if tokens != "" {
		right = th.Muted.Render(tokens) + "  " + right
		rightW += lipgloss.Width(tokens) + 2
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

// renderBarRow draws a model's bar: length ∝ total tokens on the shared
// scale, interior split at the cost-share boundary, accent slices at the
// right end (newest usage grows the bar rightward) — tui/SPEC.md §3/§5.
func (m Model) renderBarRow(st core.ModelStat, scale int64, l layout) string {
	th, g := m.theme, m.glyphs

	// The bar's total width is spring-animated (tui/SPEC.md §6); its
	// interior is recomputed as fractions of whatever width is on screen
	// this frame. target is the steady-state width the spring pulls toward.
	target := core.BarWidth(l.contentW, st.TotalTokens(), scale)
	display := m.barDisplayCells(st.Name, target, l.contentW)

	total := st.TotalTokens()
	var acc1Frac, acc2Frac float64
	if total > 0 {
		acc1Frac = float64(st.Accent1Tokens) / float64(total)
		acc2Frac = float64(st.Accent2Tokens) / float64(total)
	}
	geo := core.SplitBar(display, core.SplitFraction(st), acc1Frac, acc2Frac, st.Accent2Tokens > 0)
	if geo.Cells == 0 {
		return ""
	}

	// The glyph pattern (input vs output) spans the whole bar; accent
	// regions recolor the tail without changing glyphs, so the split
	// stays readable in monochrome.
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

	// A freshly-arrived accent2 slice renders bold for accentEmphasis — the
	// arrival signal, and the only way a sub-cell tiny request is seen (§6).
	acc2Style := th.AccentLatest
	if st.Accent2Tokens > 0 && time.Now().Before(m.accentEmphasisUntil) {
		acc2Style = acc2Style.Bold(true)
	}

	inputEnd := geo.InputCells
	if inputEnd > geo.Base {
		inputEnd = geo.Base
	}
	var b strings.Builder
	b.WriteString(" ")
	b.WriteString(run(0, inputEnd, th.AccentPrimary))
	b.WriteString(run(inputEnd, geo.Base, th.BarOutput))
	b.WriteString(run(geo.Base, geo.Base+geo.Acc1, th.AccentSession))
	b.WriteString(run(geo.Base+geo.Acc1, geo.Cells, acc2Style))
	return b.String()
}

// renderStatus draws the status row, degrading in fixed variants until
// one fits the width: full → no lag → compact → connection only
// (tui/SPEC.md §2 content, §4 pressure).
func (m Model) renderStatus(width int) string {
	th, g := m.theme, m.glyphs

	conn := m.renderConn()
	scale := "scale " + core.FormatTokens(m.scale())
	source := "source " + m.sourceLabel()

	lastFull, lastCompact := "last request —", "last —"
	if !m.snap.LastRequestAt.IsZero() {
		clock := core.FormatClock(m.snap.LastRequestAt)
		lastFull = "last request " + clock + " " + core.FormatRelative(time.Since(m.snap.LastRequestAt))
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
		{lastCompact, scale, conn},
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

// renderHints draws the bottom row: the context hint bar, optionally with
// the connection state folded in when the status row was merged away
// under height pressure (tui/SPEC.md §4).
func (m Model) renderHints(l layout) string {
	bindings := m.keys.MeterHelp()
	if m.scr == screenDetails {
		bindings = m.keys.DetailsHelp()
	}

	h := m.help
	prefix := " "
	if l.mergedBottom {
		conn := m.renderConn()
		prefix = " " + conn + m.theme.Muted.Render(m.glyphs.Sep)
	}
	h.SetWidth(m.width - lipgloss.Width(prefix))
	return prefix + h.ShortHelpView(bindings)
}
