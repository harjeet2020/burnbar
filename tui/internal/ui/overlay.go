// The expanded help overlay (`?`): a bordered key reference centered in
// region 2. It replaces the region's content while open — the one
// transient border in the app (tui/SPEC.md §2 region 4).

package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// renderHelpOverlay draws the full keymap reference as exactly
// layout.listHeight rows.
func (m Model) renderHelpOverlay(l layout) []string {
	th := m.theme

	var lines []string
	lines = append(lines, th.AccentPrimary.Bold(true).Render("keys"))
	lines = append(lines, "")
	for gi, group := range m.keys.FullHelp() {
		if gi > 0 {
			lines = append(lines, "")
		}
		for _, b := range group {
			if !b.Enabled() || b.Help().Key == "" {
				continue
			}
			key := b.Help().Key
			pad := 8 - lipgloss.Width(key)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, th.Text.Render(key)+strings.Repeat(" ", pad)+th.Muted.Render(b.Help().Desc))
		}
	}
	lines = append(lines, "")
	lines = append(lines, th.Muted.Render("? or esc to close"))

	box := th.OverlayBorder.Render(strings.Join(lines, "\n"))
	if lipgloss.Height(box) > l.listHeight {
		// Not enough room for the border chrome: fall back to the bare
		// list, cut to fit.
		return padRows(lines, l.listHeight)
	}
	placed := lipgloss.Place(m.width, l.listHeight, lipgloss.Center, lipgloss.Center, box)
	return strings.Split(placed, "\n")
}
