// The central keymap (tui/SPEC.md §2): one source of truth that both
// dispatches key events (key.Matches in update.go) and renders the hint
// row + expanded help overlay via the help bubble — bindings and their
// documentation can never drift apart.

package ui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// KeyMap declares every binding in the app. Suspend is deliberately
// excluded from all help output: ctrl+z is terminal muscle memory, not a
// feature to advertise.
type KeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Details   key.Binding
	Back      key.Binding
	Timeframe key.Binding
	Refresh   key.Binding
	Help      key.Binding
	Quit      key.Binding
	Suspend   key.Binding

	// selectHint is display-only: it folds Up+Down into the single
	// "↑/↓ select" entry the hint row shows (the real bindings stay
	// separate for dispatch).
	selectHint key.Binding
}

// newKeyMap builds the bindings; glyphs supply the ↑/↓ vs j/k hint label.
func newKeyMap(g Glyphs) KeyMap {
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
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
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
			key.WithHelp(g.UpDown, "select"),
		),
	}
}

// MeterHelp is the hint row on the main screen (tui/SPEC.md §2 mock:
// "↑/↓ select · enter details · t window · r refresh · ? help · q quit").
func (k KeyMap) MeterHelp() []key.Binding {
	return []key.Binding{k.selectHint, k.Details, k.Timeframe, k.Refresh, k.Help, k.Quit}
}

// DetailsHelp is the context hint row on the details screen
// (tui/SPEC.md §2: "esc back · t window · r refresh · q quit").
func (k KeyMap) DetailsHelp() []key.Binding {
	return []key.Binding{k.Back, k.Timeframe, k.Refresh, k.Quit}
}

// FullHelp feeds the `?` overlay: every binding, grouped navigate /
// act / app.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.selectHint, k.Details, k.Back},
		{k.Timeframe, k.Refresh},
		{k.Help, k.Quit},
	}
}

// newHelpModel configures the help bubble to render entirely in muted
// tones — the hint row is chrome, not content (tui/SPEC.md §5 token table
// puts hints under text.muted).
func newHelpModel(th Theme, g Glyphs) help.Model {
	h := help.New()
	h.ShortSeparator = g.Sep
	h.FullSeparator = "   "
	h.Styles = help.Styles{
		Ellipsis:       th.Muted,
		ShortKey:       th.Muted,
		ShortDesc:      th.Muted,
		ShortSeparator: th.Muted,
		FullKey:        th.Text,
		FullDesc:       th.Muted,
		FullSeparator:  th.Muted,
	}
	return h
}
