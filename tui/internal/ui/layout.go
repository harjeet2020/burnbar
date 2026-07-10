// Layout math shared by rendering and mouse hit-testing: one pure
// function decides where every region and model block sits, so the view
// and the click handler can never disagree about geometry.

package ui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// Minimum usable terminal size; below it a centered notice is rendered
// instead of a mangled layout (tui/SPEC.md §4). minHeight is the sum of
// the seven fixed rows (3 header + spacer + spacer + status + hints = 7)
// plus the 7-row floor for the bars list itself (two models minimum).
const (
	minWidth  = 40
	minHeight = 14
)

// breakpoint buckets the width ladder of tui/SPEC.md §4.
type breakpoint int

const (
	bpWide     breakpoint = iota // ≥110: full labels, verbose tokens
	bpStandard                   // 80–109: compact tokens
	bpNarrow                     // 60–79: truncated names, no tokens
	bpMini                       // 40–59: name + cost + bar only
)

// breakpointFor maps a terminal width onto the ladder.
func breakpointFor(w int) breakpoint {
	switch {
	case w >= 110:
		return bpWide
	case w >= 80:
		return bpStandard
	case w >= 60:
		return bpNarrow
	default:
		return bpMini
	}
}

// layout is the resolved geometry for one (width, height, model-count)
// triple. Row coordinates are 0-based terminal rows. The seven screen
// rows (tui/SPEC.md §2) never move: 3 header rows, a spacer, the bars
// list, a spacer, the status row, the hint row — only listHeight (and
// how many model blocks fit inside it) responds to window size.
type layout struct {
	tooSmall bool
	bp       breakpoint

	// contentW is the drawable width inside the 1-cell side margins.
	contentW int

	// listTop / listHeight bound region 2 (bars list / details / overlay).
	// listTop is fixed at 4 (3 header rows + the row-4 spacer).
	listTop    int
	listHeight int

	// scrolling: the list can't show every model at once; "N more"
	// indicator rows are reserved at both ends of the list either way
	// (blank when there's nothing to show in that direction).
	scrolling bool
	// visible is how many model blocks render at once.
	visible int
	// maxScroll clamps Model.scroll.
	maxScroll int
}

// computeLayout fixes every row (tui/SPEC.md §2/§4): header (3) + spacer
// + bars list + spacer + status + hints. The bars list always reserves
// one row at each end for "N more" indicators (blank when there's
// nothing to show) and always separates blocks with a blank row — never
// conditional on available height, so whitespace never jitters as the
// window resizes. A block of k visible models needs exactly 3k+1 rows
// (the two indicator rows + k×(name, bar) + (k−1) inter-block spacers);
// the largest k that fits is how many render before the list scrolls.
func computeLayout(w, h, nModels int) layout {
	l := layout{
		bp:       breakpointFor(w),
		contentW: w - 2,
		listTop:  4,
	}
	if w < minWidth || h < minHeight {
		l.tooSmall = true
		return l
	}

	l.listHeight = h - 7
	if nModels == 0 {
		return l
	}

	maxVisible := (l.listHeight - 1) / 3
	if maxVisible < 1 {
		maxVisible = 1 // defensive: minHeight already guarantees >= 2
	}
	if nModels <= maxVisible {
		l.visible = nModels
		return l
	}
	l.scrolling = true
	l.visible = maxVisible
	l.maxScroll = nModels - maxVisible
	return l
}

// clampScroll keeps a scroll offset inside the valid range for this
// layout.
func (l layout) clampScroll(scroll int) int {
	if !l.scrolling || scroll < 0 {
		return 0
	}
	if scroll > l.maxScroll {
		return l.maxScroll
	}
	return scroll
}

// blockAt resolves a terminal row to the index of the model block whose
// label or bar row contains it; -1 for the top/bottom arrow-indicator
// rows, the inter-block spacers, and anything outside the list. scroll
// must already be clamped.
func (l layout) blockAt(y, scroll, nModels int) int {
	if l.tooSmall || y < l.listTop || y >= l.listTop+l.listHeight {
		return -1
	}
	rel := y - l.listTop
	rel-- // top arrow-indicator row, always reserved
	if rel < 0 {
		return -1
	}
	if rel%3 == 2 {
		// Either the spacer between two blocks, or — for the last
		// visible block — the slot the bottom arrow-indicator row
		// occupies; both are non-hits.
		return -1
	}
	idx := scroll + rel/3
	if rel/3 >= l.visible || idx >= nModels {
		return -1
	}
	return idx
}

// tfRange is a clickable timeframe label's cell span on header row 2
// (0-based, [x0, x1) on terminal row y=2 — the header gained a blank
// spacer row in Stage D.1, tui/SPEC.md §2).
type tfRange struct {
	tf     core.Timeframe
	x0, x1 int
}

// truncate cuts s to at most max display cells, appending the ellipsis
// when anything was removed. ANSI-aware (styled strings survive) and
// grapheme-aware; width math never uses len (tui/SPEC.md §9).
func truncate(s string, max int, ellipsis string) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	return ansi.Truncate(s, max, ellipsis)
}

// truncateName applies the model-name rule (tui/SPEC.md §4): middle-
// truncate keeping the model part ("…/deepseek-v4-flash"); if even that
// overflows, tail-truncate the remainder.
func truncateName(name string, max int, ellipsis string) string {
	if lipgloss.Width(name) <= max {
		return name
	}
	if i := lastSlash(name); i >= 0 {
		short := ellipsis + "/" + name[i+1:]
		if lipgloss.Width(short) <= max {
			return short
		}
		return truncate(short, max, ellipsis)
	}
	return truncate(name, max, ellipsis)
}

// lastSlash finds the final "/" in a model slug; -1 when absent.
func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
