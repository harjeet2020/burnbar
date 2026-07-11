// Tests for the theme-picker screen's state transitions (tui/SPEC.md §5
// Stage E.1): cursor/scroll clamping, the draft/commit/cancel semantics,
// and the h/l "shadows Back on this screen only" ordering trick in
// update.go's handleKey switch.

package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newThemeTestModel builds a minimal Model sized for the theme screen —
// wide/tall enough that its content doesn't scroll, unless a test
// overrides width/height itself.
func newThemeTestModel() Model {
	colors := defaultThemeColors()
	return Model{
		keys:       newKeyMap(),
		theme:      buildTheme(colors),
		glyphs:     DefaultGlyphs(),
		colors:     colors,
		themeDraft: colors,
		scr:        screenTheme,
		width:      80,
		height:     24,
	}
}

func keyText(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Text: s} }
func keyCode(c rune) tea.KeyPressMsg   { return tea.KeyPressMsg{Code: c} }

func TestMoveThemeCursor_ClampsAtEnds(t *testing.T) {
	m := newThemeTestModel()
	m.themeCursor = 0
	if got := m.moveThemeCursor(-1).themeCursor; got != 0 {
		t.Errorf("moveThemeCursor(-1) at 0 = %d, want 0", got)
	}
	m.themeCursor = len(themeTokens) - 1
	if got := m.moveThemeCursor(1).themeCursor; got != len(themeTokens)-1 {
		t.Errorf("moveThemeCursor(1) at last = %d, want %d", got, len(themeTokens)-1)
	}
}

func TestEnsureThemeCursorVisible_ScrollsOnShortTerminal(t *testing.T) {
	m := newThemeTestModel()
	m.width, m.height = 40, minHeight // the app's floor — content will overflow
	m = m.moveThemeCursor(len(themeTokens) - 1)

	l := m.currentLayout()
	vp := computeViewport(len(m.themeContent(l)), l.listHeight)
	if !vp.scrolling {
		t.Fatalf("expected the 8-token list + preview to overflow a %dx%d terminal", m.width, m.height)
	}
	if m.themeCursor < m.themeScroll || m.themeCursor >= m.themeScroll+vp.visible {
		t.Errorf("cursor %d not within visible window [%d, %d)", m.themeCursor, m.themeScroll, m.themeScroll+vp.visible)
	}
}

func TestHandleKey_ThemeReset_ResetsDraftToBuiltinDefaultsOnly(t *testing.T) {
	m := newThemeTestModel()
	custom := defaultThemeColors().cycled(3, 2) // simulates an already-saved custom theme
	m.colors = custom
	m.theme = buildTheme(custom)
	m.themeDraft = custom.cycled(0, 1) // further edited this session

	next, _ := m.handleKey(keyText("d"))
	got := next.(Model)

	if got.themeDraft != defaultThemeColors() {
		t.Errorf("themeDraft after 'd' = %+v, want built-in defaults", got.themeDraft)
	}
	if got.colors != custom {
		t.Errorf("colors after 'd' = %+v, want untouched %+v (reset only touches the draft)", got.colors, custom)
	}
}

func TestHandleKey_ThemeEsc_DiscardsDraftLeavesColorsUntouched(t *testing.T) {
	m := newThemeTestModel()
	custom := defaultThemeColors().cycled(5, 1)
	m.colors = custom
	m.themeDraft = custom.cycled(2, 3) // edited this session, never saved

	next, _ := m.handleKey(keyCode(tea.KeyEscape))
	got := next.(Model)

	if got.scr != screenMeter {
		t.Errorf("scr after esc = %v, want screenMeter", got.scr)
	}
	if got.colors != custom {
		t.Errorf("colors after esc = %+v, want untouched %+v", got.colors, custom)
	}
}

func TestHandleKey_ThemeSave_CommitsDraftAndFiresCmd(t *testing.T) {
	m := newThemeTestModel()
	edited := defaultThemeColors().cycled(1, 3)
	m.themeDraft = edited

	next, cmd := m.handleKey(keyText("s"))
	got := next.(Model)

	if got.colors != edited {
		t.Errorf("colors after 's' = %+v, want the committed draft %+v", got.colors, edited)
	}
	if got.scr != screenMeter {
		t.Errorf("scr after 's' = %v, want screenMeter", got.scr)
	}
	if cmd == nil {
		t.Errorf("handleKey('s') returned a nil Cmd, want the save command")
	}
}

func TestHandleKey_ThemeReopen_SeedsDraftFromSavedNotDefaults(t *testing.T) {
	m := newThemeTestModel()
	m.scr = screenMeter
	custom := defaultThemeColors().cycled(6, 1)
	m.colors = custom

	next, _ := m.handleKey(keyText("c"))
	got := next.(Model)

	if got.scr != screenTheme {
		t.Fatalf("scr after 'c' = %v, want screenTheme", got.scr)
	}
	if got.themeDraft != custom {
		t.Errorf("themeDraft after reopen = %+v, want the last-saved palette %+v, not built-in defaults", got.themeDraft, custom)
	}
}

// TestHandleKey_HCyclesColorInsteadOfClosingOnThemeScreen is the crux of
// the user-requested override: Back's keyset includes "h", and so does
// ThemeCycleLeft's — on the theme screen the cycle case must win, purely
// through switch-case ordering in handleKey (tui/SPEC.md §5 Stage E.1).
func TestHandleKey_HCyclesColorInsteadOfClosingOnThemeScreen(t *testing.T) {
	m := newThemeTestModel()
	m.themeCursor = 2 // bar.input.bright
	before := themeTokens[2].get(m.themeDraft)

	next, _ := m.handleKey(keyText("h"))
	got := next.(Model)

	if got.scr != screenTheme {
		t.Fatalf("scr after 'h' = %v, want screenTheme (h must not trigger Back here)", got.scr)
	}
	after := themeTokens[2].get(got.themeDraft)
	if after != before.cycled(-1) {
		t.Errorf("token 2 after 'h' = %+v, want %+v (cycled left)", after, before.cycled(-1))
	}
}

// TestHandleKey_EscStillClosesThemeScreen guards against a regression
// where shadowing h/l for cycling accidentally also swallowed esc — esc
// isn't in either cycle binding's keyset, so it must still reach Back.
func TestHandleKey_EscStillClosesThemeScreen(t *testing.T) {
	m := newThemeTestModel()
	next, _ := m.handleKey(keyCode(tea.KeyEscape))
	got := next.(Model)
	if got.scr != screenMeter {
		t.Errorf("scr after esc = %v, want screenMeter", got.scr)
	}
}

func TestHandleKey_ThemeToggleBright(t *testing.T) {
	m := newThemeTestModel()
	m.themeCursor = 4 // bar.output.bright
	before := themeTokens[4].get(m.themeDraft).bright

	next, _ := m.handleKey(keyCode(tea.KeySpace))
	got := next.(Model)

	after := themeTokens[4].get(got.themeDraft).bright
	if after == before {
		t.Errorf("bright bit unchanged after space: before=%v after=%v", before, after)
	}
	// Only the targeted token's bright bit should flip.
	if hue := themeTokens[4].get(got.themeDraft).hue; hue != themeTokens[4].get(m.themeDraft).hue {
		t.Errorf("space toggled hue too: got %d, want unchanged %d", hue, themeTokens[4].get(m.themeDraft).hue)
	}
}
