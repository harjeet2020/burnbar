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

// hintEntry pairs a binding with its removal rank for the hint row's
// priority collapse: hintCore marks the protected core, never dropped;
// rank >= 1 entries drop in ascending order (lowest first) as the row
// runs out of width. Display order is always the entries' order in the
// slice, independent of rank — only presence is decided by rank, which
// is what lets a protected entry (refresh) sit in the middle of the
// display order without forcing every removable entry to its right
// (tui/SPEC.md §2 Stage D.1).
type hintEntry struct {
	binding key.Binding
	rank    int
}

// hintCore marks a hintEntry as always-shown.
const hintCore = -1

// MeterHints returns the main screen's hint-row entries in their fixed
// display order: j/k select · enter details · r refresh · t window · m
// mode · p source · ? help · q quit. The protected core (select,
// refresh, help, quit) always renders; the rest drop by ascending rank —
// details first, then source, then mode, then window last — until the
// row fits (tui/SPEC.md §2).
func (k KeyMap) MeterHints() []hintEntry {
	return []hintEntry{
		{k.selectHint, hintCore},
		{k.Details, 1},
		{k.Refresh, hintCore},
		{k.Timeframe, 4},
		{k.Mode, 3},
		{k.ToggleSource, 2},
		{k.Help, hintCore},
		{k.Quit, hintCore},
	}
}

// DetailsHints is the details-screen equivalent of MeterHints: back and
// quit are always shown; source drops first, then refresh, then window
// last — unchanged from the pre-existing behavior, just expressed in the
// same rank form as MeterHints.
func (k KeyMap) DetailsHints() []hintEntry {
	return []hintEntry{
		{k.Back, hintCore},
		{k.Timeframe, 3},
		{k.Refresh, 2},
		{k.ToggleSource, 1},
		{k.Quit, hintCore},
	}
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
