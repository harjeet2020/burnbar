// Tests for the hint row's fixed-display-order priority collapse
// (tui/SPEC.md §2 Stage D.1, revised): entries render in a fixed order
// regardless of rank; only presence is decided by rank, dropped in
// ascending order (lowest first) as the row runs out of width, with the
// protected core (rank hintCore) never dropped.

package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

func testHintEntries() []hintEntry {
	bind := func(k, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
	}
	return []hintEntry{
		{bind("j", "select"), hintCore},
		{bind("enter", "details"), 1},
		{bind("r", "refresh"), hintCore},
		{bind("t", "window"), 4},
		{bind("m", "mode"), 3},
		{bind("p", "source"), 2},
		{bind("?", "help"), hintCore},
		{bind("q", "quit"), hintCore},
	}
}

func TestRenderPriorityHints_FullWidthShowsAllInDisplayOrder(t *testing.T) {
	got := renderPriorityHints(DefaultTheme(), " · ", 200, testHintEntries())
	wantOrder := []string{"select", "details", "refresh", "window", "mode", "source", "help", "quit"}
	last := -1
	for _, w := range wantOrder {
		idx := strings.Index(got, w)
		if idx == -1 {
			t.Fatalf("output missing %q: %s", w, got)
		}
		if idx < last {
			t.Fatalf("%q appears out of display order in: %s", w, got)
		}
		last = idx
	}
}

func TestRenderPriorityHints_DropsLeastImportantFirst(t *testing.T) {
	entries := testHintEntries()
	full := renderPriorityHints(DefaultTheme(), " · ", 200, entries)
	width := lipgloss.Width(full) - 1 // one cell too narrow for everything
	got := renderPriorityHints(DefaultTheme(), " · ", width, entries)
	if strings.Contains(got, "details") {
		t.Errorf("details (rank 1) should be first to drop, still present: %s", got)
	}
	if !strings.Contains(got, "window") {
		t.Errorf("window (rank 4, most protected extra) should still be present: %s", got)
	}
	if !strings.Contains(got, "refresh") {
		t.Errorf("refresh is core, should never drop: %s", got)
	}
}

func TestRenderPriorityHints_RemovalOrder(t *testing.T) {
	entries := testHintEntries()
	row := renderPriorityHints(DefaultTheme(), " · ", 200, entries)
	// Shrink one cell at a time and record which removable entry
	// disappears at each step; it must match rank order details(1) ->
	// source(2) -> mode(3) -> window(4).
	removedOrder := []string{}
	seen := map[string]bool{"details": true, "source": true, "mode": true, "window": true}
	prev := row
	for w := lipgloss.Width(row) - 1; w > 0; w-- {
		cur := renderPriorityHints(DefaultTheme(), " · ", w, entries)
		for name := range seen {
			if strings.Contains(prev, name) && !strings.Contains(cur, name) {
				removedOrder = append(removedOrder, name)
				delete(seen, name)
			}
		}
		prev = cur
		if len(seen) == 0 {
			break
		}
	}
	want := []string{"details", "source", "mode", "window"}
	if strings.Join(removedOrder, ",") != strings.Join(want, ",") {
		t.Fatalf("removal order = %v, want %v", removedOrder, want)
	}
}

func TestRenderPriorityHints_CoreNeverDrops(t *testing.T) {
	got := renderPriorityHints(DefaultTheme(), " · ", 1, testHintEntries())
	for _, w := range []string{"select", "refresh", "help", "quit"} {
		if !strings.Contains(got, w) {
			t.Errorf("core entry %q dropped at width=1: %s", w, got)
		}
	}
}
