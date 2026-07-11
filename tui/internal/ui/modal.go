// The recent-request modal (`i`, tui/SPEC.md §2 Stage D.4): a centered
// overlay showing full detail on the most recent coalesced burst (§7) —
// same chrome as the `?` help overlay, the app's one transient border.
// Display-only: it never feeds the authoritative window totals.

package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// renderRequestModal draws region 2 as exactly layout.listHeight rows,
// mirroring renderHelpOverlay's box-or-fallback shape.
func (m Model) renderRequestModal(l layout) []string {
	th := m.theme

	var lines []string
	if m.burst == nil {
		lines = append(lines,
			th.AccentPrimary.Bold(true).Render("recent request"),
			"",
			th.Muted.Render("no requests seen yet"),
		)
	} else {
		lines = m.renderBurstLines(l)
	}
	lines = append(lines, "", th.Muted.Render("i or esc to close"))

	box := th.OverlayBorder.Render(strings.Join(lines, "\n"))
	if lipgloss.Height(box) > l.listHeight {
		return padRows(lines, l.listHeight)
	}
	placed := lipgloss.Place(m.width, l.listHeight, lipgloss.Center, lipgloss.Center, box)
	return strings.Split(placed, "\n")
}

// renderBurstLines formats the modal's body for a non-nil m.burst, in the
// order requests → tokens → cost → effective cost → cache hit →
// reasoning → last request → meter lag (tui/SPEC.md §2 Stage D.4). The
// tokens/cost/effective-cost rows share the same total·in(%)·out(%)
// mini-table alignment as the details screen's stats grid (details.go's
// tripleValue/tripleWidths), computed here over just these three rows
// since the modal mixes them into one kv-style list rather than a
// two-column grid.
func (m Model) renderBurstLines(l layout) []string {
	th, g := m.theme, m.glyphs
	b := *m.burst

	kv := func(k, v string) string {
		pad := 16 - lipgloss.Width(k)
		if pad < 1 {
			pad = 1
		}
		return th.Muted.Render(k) + strings.Repeat(" ", pad) + th.Text.Render(v)
	}

	title := th.AccentPrimary.Bold(true).Render(truncateName(b.Model, l.contentW-1, g.Ellipsis))
	if b.ProviderSlug != nil {
		title += "  " + th.Muted.Render(*b.ProviderSlug)
	}

	var lines []string
	lines = append(lines, title, "")

	requests := fmt.Sprintf("%d", b.Requests)
	if b.Requests > 1 {
		span := core.FormatDuration(b.LastRequestedAt.Sub(b.FirstRequestedAt))
		requests += " over " + span
	}

	triad := []statGridRow{
		{label: "tokens", total: core.FormatTokens(b.TotalTokens()), in: withPct(core.FormatTokens(b.InputTokens), b.InputTokensPct()), out: withPct(core.FormatTokens(b.OutputTokens), b.OutputTokensPct())},
		{label: "cost", total: core.FormatCost(b.Cost), in: withPct(fmtCostPtr(b.InputCost), b.InputCostPct()), out: withPct(fmtCostPtr(b.OutputCost), b.OutputCostPct())},
		{label: "eff. price/1M", total: core.FormatRate(b.EffectiveRate()), in: core.FormatRate(b.EffectiveInputRate()), out: core.FormatRate(b.EffectiveOutputRate())},
	}
	totalW, inW, outW := tripleWidths(triad)

	wallTime := core.FormatClock(b.LastRequestedAt) + " " + core.FormatRelative(time.Since(b.LastRequestedAt))

	lag := "—"
	if m.snap.LagSeconds != nil {
		lag = core.FormatDuration(time.Duration(*m.snap.LagSeconds * float64(time.Second)))
	}

	lines = append(lines,
		kv("requests", requests),
		kv(triad[0].label, tripleValue(g, triad[0], totalW, inW, outW, true)),
		kv(triad[1].label, tripleValue(g, triad[1], totalW, inW, outW, true)),
		kv(triad[2].label, tripleValue(g, triad[2], totalW, inW, outW, true)),
		kv("cache hit", core.FormatPct(b.CacheHitPct())),
		kv("reasoning", core.FormatPct(b.ReasoningPct())),
		kv("last request", wallTime),
		kv("meter lag", lag),
	)

	return lines
}
