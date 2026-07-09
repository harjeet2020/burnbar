// The central keymap (tui/SPEC.md §2): one source of truth that both
// dispatches key events (key.Matches in update.go) and renders the hint
// row + expanded help overlay — bindings and their documentation can
// never drift apart.

package ui

import (
	"charm.land/bubbles/v2/key"
)

// KeyMap declares every binding in the app. Suspend is deliberately
// excluded from all help output: ctrl+z is terminal muscle memory, not a
// feature to advertise.
type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Details      key.Binding
	Back         key.Binding
	Timeframe    key.Binding
	Mode         key.Binding
	Refresh      key.Binding
	ToggleSource key.Binding
	Help         key.Binding
	Quit         key.Binding
	Suspend      key.Binding

	// selectHint is display-only: it folds Up+Down into the single
	// "j/k select" entry the hint row shows (the real bindings stay
	// separate for dispatch).
	selectHint key.Binding
}

// newKeyMap builds the bindings. The select hint label is always "j/k"
// (tui/SPEC.md §2 Stage D.1) — arrows stay bound but undocumented.
func newKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("j", "down"),
		),
		Details: key.NewBinding(
			key.WithKeys("enter", "l"),
			key.WithHelp("enter", "details"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "h"),
			key.WithHelp("esc", "back"),
		),
		Timeframe: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "window"),
		),
		Mode: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "mode"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		ToggleSource: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "source"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Suspend: key.NewBinding(
			key.WithKeys("ctrl+z"),
		),
		selectHint: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("j/k", "select"),
		),
	}
}

// MeterHints splits the main screen's hint row into a protected core
// (always rendered) and extra bindings added back in priority order as
// width allows (tui/SPEC.md §2 Stage D.1).
func (k KeyMap) MeterHints() (core, extra []key.Binding) {
	return []key.Binding{k.selectHint, k.Help, k.Quit},
		[]key.Binding{k.Details, k.Timeframe, k.Mode, k.Refresh, k.ToggleSource}
}

// DetailsHints is the details-screen equivalent of MeterHints.
func (k KeyMap) DetailsHints() (core, extra []key.Binding) {
	return []key.Binding{k.Back, k.Quit},
		[]key.Binding{k.Timeframe, k.Refresh, k.ToggleSource}
}

// FullHelp feeds the `?` overlay: every binding, grouped navigate /
// act / app.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.selectHint, k.Details, k.Back},
		{k.Timeframe, k.Mode, k.Refresh, k.ToggleSource},
		{k.Help, k.Quit},
	}
}
