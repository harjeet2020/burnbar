// Package ui is the Bubble Tea layer: the root model, message handling,
// and all rendering. It follows the MVU split — model.go holds state,
// update.go mutates it, view.go and the per-region renderers draw it.
// Everything data-shaped lives in internal/core; ui only arranges it.
package ui

import (
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/harjeet2020/burnbar/tui/internal/core"
	"github.com/harjeet2020/burnbar/tui/internal/data"
)

// screen identifies which view owns region 2 (the only region whose
// content ever changes — header, status, and hints persist, tui/SPEC.md §2).
type screen int

const (
	screenMeter screen = iota
	screenDetails
)

// Model is the single Bubble Tea state struct for the whole app.
type Model struct {
	// cfg is unused in Stage A beyond having been validated at startup;
	// Stage C's data layer reads it.
	cfg data.Config

	theme  Theme
	glyphs Glyphs
	keys   KeyMap
	help   help.Model

	// width/height cache the latest WindowSizeMsg — all layout derives
	// from them on every render (tui/SPEC.md §4).
	width  int
	height int

	// tf is the active window; snap is the (fixture) snapshot for it.
	tf   core.Timeframe
	snap core.Snapshot

	// selected tracks the selected model by *name*, not row index, so
	// selection survives re-sorts and window switches (tui/SPEC.md §2).
	selected string
	// scroll is the index of the first visible model block when the list
	// is in scroll mode.
	scroll int

	scr      screen
	showHelp bool
}

// New assembles the initial model from a validated config: theme, glyphs,
// keymap, and the today fixture with the top model preselected.
func New(cfg data.Config) Model {
	glyphs := DefaultGlyphs()
	theme := DefaultTheme()
	snap := core.Fixture(core.TimeframeToday, time.Now())

	m := Model{
		cfg:    cfg,
		theme:  theme,
		glyphs: glyphs,
		keys:   newKeyMap(glyphs),
		help:   newHelpModel(theme, glyphs),
		tf:     core.TimeframeToday,
		snap:   snap,
	}
	if len(snap.Models) > 0 {
		m.selected = snap.Models[0].Name
	}
	return m
}

// Init performs no startup work in Stage A — the fixture is already in
// memory. Stage C wires the subscribe-then-fetch startup sequence here.
func (m Model) Init() tea.Cmd { return nil }

// selectedIndex resolves the selected model name to its position in the
// current snapshot; -1 when the selection isn't in this window.
func (m Model) selectedIndex() int {
	for i, st := range m.snap.Models {
		if st.Name == m.selected {
			return i
		}
	}
	return -1
}

// scale is the shared bar scale S for the current snapshot (tui/SPEC.md §3).
func (m Model) scale() int64 {
	var max int64
	for _, st := range m.snap.Models {
		if t := st.TotalTokens(); t > max {
			max = t
		}
	}
	return core.ScaleFor(max)
}
