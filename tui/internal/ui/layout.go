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
// instead of a mangled layout (tui/SPEC.md §4).
const (
	minWidth  = 40
	minHeight = 10
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
// triple. Row coordinates are 0-based terminal rows.
type layout struct {
	tooSmall bool
	bp       breakpoint

	// contentW is the drawable width inside the 1-cell side margins.
	contentW int

	// listTop / listHeight bound region 2 (bars list / details / overlay).
	listTop    int
	listHeight int

	// spacers: a blank row between model blocks (dropped first under
	// height pressure, tui/SPEC.md §2/§4).
	spacers bool
	// mergedBottom: the status row has been folded into the hint row
	// (second pressure step).
	mergedBottom bool
	// scrolling: the list cannot fit even merged; blocks scroll with
	// edge indicator rows reserved at both ends (third pressure step).
	scrolling bool
	// visible is how many model blocks render at once.
	visible int
	// maxScroll clamps Model.scroll.
	maxScroll int

	statusRow int // -1 when merged
	bottomRow int // hint row (or merged status+hint row)
}

// blockHeight is a model block's rows: label + bar (+1 when spacers on).
func (l layout) blockHeight() int {
	if l.spacers {
		return 3
	}
	return 2
}

// computeLayout applies the §4 height-pressure ladder in order: spacer
// rows → status merges into hints → list scrolls.
func computeLayout(w, h, nModels int) layout {
	l := layout{
		bp:        breakpointFor(w),
		contentW:  w - 2,
		listTop:   2,
		statusRow: h - 2,
		bottomRow: h - 1,
	}
	if w < minWidth || h < minHeight {
		l.tooSmall = true
		return l
	}

	// Full chrome: 2 header rows + status + hint.
	l.listHeight = h - 4

	switch {
	case nModels == 0:
		l.visible = 0
	// Spacer budget: one leading blank under the header + a blank between
	// blocks = 3n rows.
	case 3*nModels <= l.listHeight:
		l.spacers = true
		l.visible = nModels
	case 2*nModels <= l.listHeight:
		l.visible = nModels
	default:
		// Merge status into the hint row and retry without spacers.
		l.mergedBottom = true
		l.statusRow = -1
		l.listHeight = h - 3
		if 2*nModels <= l.listHeight {
			l.visible = nModels
			break
		}
		// Still too tall: scroll. Reserve one indicator row at each end
		// so "▲ N more" / "▼ N more" never displace blocks.
		l.scrolling = true
		l.visible = (l.listHeight - 2) / 2
		if l.visible < 1 {
			l.visible = 1
		}
		if l.visible > nModels {
			l.visible = nModels
		}
		l.maxScroll = nModels - l.visible
	}
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
// label or bar row contains it; -1 for spacers, indicators, and anything
// outside the list. scroll must already be clamped.
func (l layout) blockAt(y, scroll, nModels int) int {
	if l.tooSmall || y < l.listTop || y >= l.listTop+l.listHeight {
		return -1
	}
	rel := y - l.listTop
	if l.scrolling {
		rel-- // top indicator row
		if rel < 0 {
			return -1
		}
		idx := scroll + rel/2
		if rel/2 >= l.visible || idx >= nModels {
			return -1
		}
		return idx
	}
	if l.spacers {
		rel-- // leading blank row under the header
		if rel < 0 || rel%3 == 2 {
			return -1 // header gap or spacer row between blocks
		}
	}
	idx := rel / l.blockHeight()
	if idx >= nModels {
		return -1
	}
	return idx
}

// tfRange is a clickable timeframe label's cell span on header row 2
// (0-based, [x0, x1) on terminal row y=1).
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
