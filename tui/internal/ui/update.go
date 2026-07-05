// Message handling — the only place state changes. Keyboard is primary;
// every mouse action mirrors a key binding (tui/SPEC.md §2). All hit-
// testing goes through the same computeLayout the view renders from.

package ui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// Update routes messages to the focused handler. It stays non-blocking:
// nothing here does I/O (there is none in Stage A anyway).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// All layout is re-derived from these on the next render — a
		// resize snaps, never animates (tui/SPEC.md §4).
		m.width, m.height = msg.Width, msg.Height
		m.scroll = m.currentLayout().clampScroll(m.scroll)
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		return m.handleClick(msg)
	case tea.MouseWheelMsg:
		return m.handleWheel(msg)
	}
	return m, nil
}

// currentLayout resolves geometry for the current size and model count.
func (m Model) currentLayout() layout {
	return computeLayout(m.width, m.height, len(m.snap.Models))
}

// handleKey dispatches key presses through the central keymap.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys

	// Quit and suspend always work, whatever is on screen (tui/SPEC.md §8).
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Suspend):
		return m, tea.Suspend
	}

	// The help overlay captures everything else while open.
	if m.showHelp {
		if key.Matches(msg, k.Help) || key.Matches(msg, k.Back) {
			m.showHelp = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, k.Help):
		m.showHelp = true
	case key.Matches(msg, k.Timeframe):
		m = m.setTimeframe(m.tf.Next())
	case key.Matches(msg, k.Refresh):
		// Stage C wires the real refresh (re-baseline + credits poll);
		// deliberately inert until then.
	case m.scr == screenMeter && key.Matches(msg, k.Up):
		m = m.moveSelection(-1)
	case m.scr == screenMeter && key.Matches(msg, k.Down):
		m = m.moveSelection(1)
	case m.scr == screenMeter && key.Matches(msg, k.Details):
		if m.selectedIndex() >= 0 {
			m.scr = screenDetails
		}
	case m.scr == screenDetails && key.Matches(msg, k.Back):
		m.scr = screenMeter
	}
	return m, nil
}

// setTimeframe switches the window: pure client-side re-aggregation —
// instant, no refetch (tui/SPEC.md §7). Selection follows the model name;
// when it left the window, fall back to the top model.
func (m Model) setTimeframe(tf core.Timeframe) Model {
	m.tf = tf
	m.snap = core.Fixture(tf, time.Now())
	if m.selectedIndex() < 0 {
		m.selected = ""
		if len(m.snap.Models) > 0 {
			m.selected = m.snap.Models[0].Name
		}
	}
	m.scroll = m.currentLayout().clampScroll(m.scroll)
	m = m.ensureSelectionVisible()
	return m
}

// moveSelection moves the selection by delta rows, clamped, and keeps it
// on screen.
func (m Model) moveSelection(delta int) Model {
	n := len(m.snap.Models)
	if n == 0 {
		return m
	}
	idx := m.selectedIndex()
	if idx < 0 {
		idx = 0
	} else {
		idx += delta
	}
	if idx < 0 {
		idx = 0
	}
	if idx > n-1 {
		idx = n - 1
	}
	m.selected = m.snap.Models[idx].Name
	return m.ensureSelectionVisible()
}

// ensureSelectionVisible adjusts the scroll offset so the selected block
// is on screen when the list scrolls.
func (m Model) ensureSelectionVisible() Model {
	l := m.currentLayout()
	if !l.scrolling {
		m.scroll = 0
		return m
	}
	idx := m.selectedIndex()
	if idx < 0 {
		return m
	}
	if idx < m.scroll {
		m.scroll = idx
	}
	if idx >= m.scroll+l.visible {
		m.scroll = idx - l.visible + 1
	}
	m.scroll = l.clampScroll(m.scroll)
	return m
}

// handleClick implements the mouse augmentation (tui/SPEC.md §2): click a
// timeframe label to activate it, click a bar to select, click the
// selected bar (or double-click — the second click lands on an already-
// selected bar) to open details.
func (m Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// Timeframe labels live on header row 2 (y=1) on every screen.
	if msg.Y == 1 {
		_, ranges := m.timeframeSelector()
		for _, r := range ranges {
			if msg.X >= r.x0 && msg.X < r.x1 {
				m = m.setTimeframe(r.tf)
				return m, nil
			}
		}
		return m, nil
	}

	if m.scr != screenMeter {
		return m, nil
	}

	l := m.currentLayout()
	idx := l.blockAt(msg.Y, l.clampScroll(m.scroll), len(m.snap.Models))
	if idx < 0 {
		return m, nil
	}
	name := m.snap.Models[idx].Name
	if name == m.selected {
		m.scr = screenDetails
		return m, nil
	}
	m.selected = name
	m = m.ensureSelectionVisible()
	return m, nil
}

// handleWheel scrolls the bars list when it is in scroll mode — the wheel
// moves the viewport, not the selection (tui/SPEC.md §2).
func (m Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.scr != screenMeter || m.showHelp {
		return m, nil
	}
	l := m.currentLayout()
	if !l.scrolling {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		m.scroll = l.clampScroll(m.scroll - 1)
	case tea.MouseWheelDown:
		m.scroll = l.clampScroll(m.scroll + 1)
	}
	return m, nil
}
