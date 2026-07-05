// The details screen — Stage A stub. It proves the drill-down navigation
// (screen swap, persistent header/status/hints, context hint row) with the
// selected model's fixture numbers; the full stats grid and provider split
// table land in Stage E (tui/SPEC.md §2, §10).

package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// renderDetails draws region 2 of the details screen as exactly
// layout.listHeight rows.
func (m Model) renderDetails(l layout) []string {
	th, g := m.theme, m.glyphs

	idx := m.selectedIndex()
	if idx < 0 {
		return padRows([]string{"", " " + th.Muted.Render("nothing selected")}, l.listHeight)
	}
	st := m.snap.Models[idx]

	label := func(s string) string { return th.Muted.Render(s) }
	kv := func(k, v string) string {
		pad := 16 - lipgloss.Width(k)
		if pad < 1 {
			pad = 1
		}
		return " " + label(k) + strings.Repeat(" ", pad) + th.Text.Render(v)
	}

	costLine := core.FormatCost(st.Cost)
	if st.InputCost != nil && st.OutputCost != nil {
		costLine += g.Sep + "in " + core.FormatCost(*st.InputCost) + g.Sep + "out " + core.FormatCost(*st.OutputCost)
	} else {
		costLine += g.Sep + "in —" + g.Sep + "out —"
	}

	rows := []string{
		th.Selected.Render(g.Select+truncateName(st.Name, l.contentW-1, g.Ellipsis)) +
			"  " + th.Muted.Render("("+m.tf.Label()+")"),
		"",
		kv("requests", core.FormatTokens(st.Requests)),
		kv("input tokens", core.FormatTokens(st.InputTokens)),
		kv("output tokens", core.FormatTokens(st.OutputTokens)),
		kv("cost", costLine),
		"",
		" " + th.Muted.Render("full stats grid & provider split arrive in Stage E"),
	}
	for i, r := range rows {
		rows[i] = truncate(r, m.width, g.Ellipsis)
	}
	return padRows(rows, l.listHeight)
}

// padRows fits a row slice to exactly h rows, padding with blanks or
// cutting from the end.
func padRows(rows []string, h int) []string {
	for len(rows) < h {
		rows = append(rows, "")
	}
	return rows[:h]
}
