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
	ZoomOut      key.Binding
	ZoomIn       key.Binding
	ZoomReset    key.Binding
	Modal        key.Binding
	Refresh      key.Binding
	ToggleSource key.Binding
	Help         key.Binding
	Quit         key.Binding
	Suspend      key.Binding

	// Theme opens the live color picker (tui/SPEC.md §5 Stage E.1);
	// ThemeCycleLeft/Right, ThemeToggleBright, ThemeReset, and ThemeSave
	// only ever dispatch while screenTheme is active (update.go).
	Theme             key.Binding
	ThemeCycleLeft    key.Binding
	ThemeCycleRight   key.Binding
	ThemeToggleBright key.Binding
	ThemeReset        key.Binding
	ThemeSave         key.Binding

	// selectHint is display-only: it folds Up+Down into the single
	// "j/k select" entry the hint row shows (the real bindings stay
	// separate for dispatch). zoomHint similarly folds the three zoom
	// bindings into one "-/+/0 zoom" hint-row entry (tui/SPEC.md §3 Stage
	// D.3); each key still dispatches through its own binding. scrollHint
	// folds the same up/down/k/j keys into "j/k scroll" for the details
	// screen, where they drive the content viewport instead of selection
	// (tui/SPEC.md §2 Stage E). themeRowHint/themeCycleHint are the same
	// idiom for the theme screen's row-move and color-cycle bindings.
	selectHint     key.Binding
	zoomHint       key.Binding
	scrollHint     key.Binding
	themeRowHint   key.Binding
	themeCycleHint key.Binding
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
		ZoomOut: key.NewBinding(
			key.WithKeys("-"),
			key.WithHelp("-", "zoom out"),
		),
		ZoomIn: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+", "zoom in"),
		),
		ZoomReset: key.NewBinding(
			key.WithKeys("0"),
			key.WithHelp("0", "zoom reset"),
		),
		Modal: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "info"),
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
		Theme: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "colors"),
		),
		ThemeCycleLeft: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("h", "color -"),
		),
		ThemeCycleRight: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("l", "color +"),
		),
		ThemeToggleBright: key.NewBinding(
			key.WithKeys("space"),
			key.WithHelp("space", "bright"),
		),
		ThemeReset: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "reset"),
		),
		ThemeSave: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "save"),
		),
		selectHint: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("j/k", "select"),
		),
		zoomHint: key.NewBinding(
			key.WithKeys("-", "+", "=", "0"),
			key.WithHelp("-/+/0", "zoom"),
		),
		scrollHint: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("j/k", "scroll"),
		),
		themeRowHint: key.NewBinding(
			key.WithKeys("up", "down", "k", "j"),
			key.WithHelp("j/k", "row"),
		),
		themeCycleHint: key.NewBinding(
			key.WithKeys("left", "right", "h", "l"),
			key.WithHelp("h/l", "color"),
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
// mode · -/+/0 zoom · i info · p source · ? help · q quit. The protected
// core (select, refresh, help, quit) always renders; the rest drop by
// ascending rank — details first, then source, then mode, then window,
// then zoom, then info last — until the row fits (tui/SPEC.md §2). Zoom
// and info are appended at the end of the existing drop ladder (most-
// protected-among-droppable) so D.1's established order for the earlier
// entries is untouched; both are still-provisional bindings (§11), so
// they're the first to go under width pressure.
func (k KeyMap) MeterHints() []hintEntry {
	return []hintEntry{
		{k.selectHint, hintCore},
		{k.Details, 1},
		{k.Refresh, hintCore},
		{k.Timeframe, 4},
		{k.Mode, 3},
		{k.zoomHint, 5},
		{k.Modal, 6},
		{k.ToggleSource, 2},
		{k.Theme, 7},
		{k.Help, hintCore},
		{k.Quit, hintCore},
	}
}

// DetailsHints is the details-screen equivalent of MeterHints: back and
// quit are always shown. scroll drops first — it only matters on short
// terminals where the content overflows, unlike source/refresh/window
// which matter at every height (tui/SPEC.md §2 Stage E) — then source,
// then refresh, then window last.
func (k KeyMap) DetailsHints() []hintEntry {
	return []hintEntry{
		{k.Back, hintCore},
		{k.scrollHint, 1},
		{k.Timeframe, 4},
		{k.Refresh, 3},
		{k.ToggleSource, 2},
		{k.Quit, hintCore},
	}
}

// ThemeHints is the theme-picker screen's hint-row entries (tui/SPEC.md §5
// Stage E.1): back and save are always shown; row-move drops before
// color-cycle (the picker is useless without cycling, less so without
// visible scroll affordance on a tall list), then toggle-bright, then
// reset.
func (k KeyMap) ThemeHints() []hintEntry {
	return []hintEntry{
		{k.Back, hintCore},
		{k.themeRowHint, 1},
		{k.themeCycleHint, hintCore},
		{k.ThemeToggleBright, 2},
		{k.ThemeReset, 3},
		{k.ThemeSave, hintCore},
		{k.Quit, hintCore},
	}
}

// FullHelp feeds the `?` overlay: every binding, grouped navigate /
// act / app / theme.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.selectHint, k.Details, k.Back},
		{k.Timeframe, k.Mode, k.ZoomOut, k.ZoomIn, k.ZoomReset, k.Modal, k.Refresh, k.ToggleSource},
		{k.Theme, k.themeCycleHint, k.ThemeToggleBright, k.ThemeReset, k.ThemeSave},
		{k.Help, k.Quit},
	}
}
