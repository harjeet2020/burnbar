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

// renderBurstLines formats the modal's body for a non-nil m.burst: model
// (+ provider if reported), request count/span if >1, tokens, cost split,
// wall time + age, and meter lag (tui/SPEC.md §2 Stage D.4).
func (m Model) renderBurstLines(l layout) []string {
	th, g := m.theme, m.glyphs
	b := *m.burst

	label := func(s string) string { return th.Muted.Render(s) }
	kv := func(k, v string) string {
		pad := 16 - lipgloss.Width(k)
		if pad < 1 {
			pad = 1
		}
		return label(k) + strings.Repeat(" ", pad) + th.Text.Render(v)
	}
	i64OrDash := func(v *int64) string {
		if v == nil {
			return "—"
		}
		return core.FormatTokens(*v)
	}

	title := th.AccentPrimary.Bold(true).Render(truncateName(b.Model, l.contentW-1, g.Ellipsis))
	if b.ProviderSlug != nil {
		title += "  " + th.Muted.Render(*b.ProviderSlug)
	}

	var lines []string
	lines = append(lines, title, "")

	if b.Requests > 1 {
		span := core.FormatDuration(b.LastRequestedAt.Sub(b.FirstRequestedAt))
		lines = append(lines, label(fmt.Sprintf("%d requests over %s", b.Requests, span)), "")
	}

	lines = append(lines,
		kv("input tokens", core.FormatTokens(b.InputTokens)),
		kv("output tokens", core.FormatTokens(b.OutputTokens)),
		kv("cached tokens", i64OrDash(b.CachedTokens)),
		kv("reasoning tokens", i64OrDash(b.ReasoningTokens)),
	)

	costLine := core.FormatCost(b.Cost)
	if b.InputCost != nil && b.OutputCost != nil {
		costLine += g.Sep + "in " + core.FormatCost(*b.InputCost) + g.Sep + "out " + core.FormatCost(*b.OutputCost)
	} else {
		costLine += g.Sep + "in —" + g.Sep + "out —"
	}
	lines = append(lines, kv("cost", costLine))

	wallTime := core.FormatClock(b.LastRequestedAt) + " " + core.FormatRelative(time.Since(b.LastRequestedAt))
	lines = append(lines, kv("last request", wallTime))

	lag := "—"
	if m.snap.LagSeconds != nil {
		lag = core.FormatDuration(time.Duration(*m.snap.LagSeconds * float64(time.Second)))
	}
	lines = append(lines, kv("meter lag", lag))

	return lines
}
