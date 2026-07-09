// View composition: the four fixed regions stacked top to bottom, the
// too-small state, and the frame-level declarations (alt screen, mouse
// mode) that Bubble Tea v2 reads off the returned tea.View.

package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// View renders the whole frame and declares the terminal modes: full
// screen on the alternate buffer with cell-motion mouse reporting
// (tui/SPEC.md §1, §2 mouse augmentation).
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// render assembles the frame: header, region 2 (bars, details, or the
// help overlay), status row, hint row. Regions never move or reorder
// (tui/SPEC.md §2).
func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "" // first WindowSizeMsg hasn't arrived yet
	}

	l := computeLayout(m.width, m.height, len(m.snap.Models))
	if l.tooSmall {
		msg := m.theme.Muted.Render("terminal too small (min 40×10)")
		if m.glyphs.Arrow == ">" { // ASCII mode
			msg = m.theme.Muted.Render("terminal too small (min 40x10)")
		}
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	var region2 []string
	switch {
	case m.showHelp:
		region2 = m.renderHelpOverlay(l)
	case m.scr == screenDetails:
		region2 = m.renderDetails(l)
	default:
		region2 = m.renderBars(l)
	}

	rows := make([]string, 0, m.height)
	rows = append(rows, m.renderHeader())
	rows = append(rows, region2...)
	if !l.mergedBottom {
		rows = append(rows, m.renderStatus(m.width))
	}
	rows = append(rows, m.renderHints(l))
	return strings.Join(rows, "\n")
}
