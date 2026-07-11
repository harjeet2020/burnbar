// The details screen (tui/SPEC.md §2, §10 Stage E) — a full-screen
// drill-down for one model over the active window: a responsive stats
// grid (two columns at the widest breakpoint, collapsing to one column
// and eventually total-only triads as width tightens) plus a
// provider-split table that sheds columns before falling back to a
// per-provider stacked list at the narrowest breakpoint. Content taller
// than the available rows scrolls as one flat unit via detailsViewport
// (layout.go).

package ui

import (
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// statGridRow is one stats-grid line: a label plus a value that's either
// a bare scalar (in/out empty — requests, avg duration, cache hit,
// reasoning) or a total·in·out triad. A zero-value row (empty label)
// renders as a blank line — the grid's one intentional gap.
type statGridRow struct {
	label   string
	total   string
	in, out string
}

// fmtCostPtr renders a nullable cost, "—" for nil — the same nil-check
// the Stage A stub already did inline for InputCost/OutputCost.
func fmtCostPtr(c *float64) string {
	if c == nil {
		return "—"
	}
	return core.FormatCost(*c)
}

// fmtAvgTokens renders a nullable mean-token count at core.FormatTokens
// precision, "—" for nil.
func fmtAvgTokens(v *float64) string {
	if v == nil {
		return "—"
	}
	return core.FormatTokens(int64(math.Round(*v)))
}

// fmtAvgDuration renders a nullable mean duration in milliseconds
// through core.FormatDuration, "—" for nil — mirrors the seconds-based
// idiom modal.go/meter.go already use for LagSeconds.
func fmtAvgDuration(ms *float64) string {
	if ms == nil {
		return "—"
	}
	return core.FormatDuration(time.Duration(*ms * float64(time.Millisecond)))
}

// statGridColumns builds the fixed stats-grid content for one model —
// always the full triad values; renderStatsGrid decides per breakpoint
// whether to show the split or collapse to total. left (4 rows) and right
// (5 rows) intentionally differ in length — right groups every cost/token
// stat that has a real in/out split, left the four that don't — rather
// than padding left with a blank row to match, which used to land a dead
// gap between "requests" and "avg duration" (tui/SPEC.md §2 Stage E
// refinement).
func statGridColumns(st core.ModelStat) (left, right []statGridRow) {
	left = []statGridRow{
		{label: "requests", total: core.FormatTokens(st.Requests)},
		{label: "avg duration", total: fmtAvgDuration(st.AvgDurationMS())},
		{label: "cache hit", total: core.FormatPct(st.CacheHitPct())},
		{label: "reasoning", total: core.FormatPct(st.ReasoningPct())},
	}
	right = []statGridRow{
		{label: "cost", total: core.FormatCost(st.Cost), in: fmtCostPtr(st.InputCost), out: fmtCostPtr(st.OutputCost)},
		{label: "tokens", total: core.FormatTokens(st.TotalTokens()), in: core.FormatTokens(st.InputTokens), out: core.FormatTokens(st.OutputTokens)},
		{label: "avg cost/req", total: fmtCostPtr(st.AvgCost()), in: fmtCostPtr(st.AvgInputCost()), out: fmtCostPtr(st.AvgOutputCost())},
		{label: "avg tokens/req", total: fmtAvgTokens(st.AvgTokens()), in: fmtAvgTokens(st.AvgInputTokens()), out: fmtAvgTokens(st.AvgOutputTokens())},
		{label: "eff. price/1M", total: core.FormatRate(st.EffectiveRate()), in: core.FormatRate(st.EffectiveInputRate()), out: core.FormatRate(st.EffectiveOutputRate())},
	}
	return left, right
}

// padLeft right-aligns s within w display cells by left-padding with
// spaces — how the stats grid's total/in/out mini-table columns line up
// despite differing value lengths ($0.2851 vs 759K): every row in a
// tripleWidths-derived column is guaranteed no wider than w, so no
// truncation branch is needed here (tui/SPEC.md §2 Stage E refinement).
func padLeft(s string, w int) string {
	if pad := w - lipgloss.Width(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// tripleWidths measures the widest rendered total/in/out value across a
// set of triad rows, so every row's mini-table columns share one right
// edge regardless of unit (a dollar amount vs. a bare token count).
func tripleWidths(rows []statGridRow) (totalW, inW, outW int) {
	for _, r := range rows {
		totalW = max(totalW, lipgloss.Width(r.total))
		inW = max(inW, lipgloss.Width(r.in))
		outW = max(outW, lipgloss.Width(r.out))
	}
	return totalW, inW, outW
}

// renderScalarRow formats a plain label+value line — the left column's
// four rows, none of which carry an in/out split.
func renderScalarRow(th Theme, g Glyphs, r statGridRow) string {
	if r.label == "" {
		return ""
	}
	pad := 16 - lipgloss.Width(r.label)
	if pad < 1 {
		pad = 1
	}
	return " " + th.Muted.Render(r.label) + strings.Repeat(" ", pad) + th.Text.Render(r.total)
}

// renderTripleRow formats a label + total·in·out line, right-aligning
// each of the three values into the shared totalW/inW/outW column widths
// (tripleWidths) computed across all triad rows — the mini-table
// structure that replaces the old ad hoc "$0.2851 - in $0.1695 - out
// $0.0406" concatenation. showSplit=false (bpMini) renders total only,
// still aligned to totalW.
func renderTripleRow(th Theme, g Glyphs, r statGridRow, totalW, inW, outW int, showSplit bool) string {
	if r.label == "" {
		return ""
	}
	value := padLeft(r.total, totalW)
	if showSplit {
		value += g.Sep + "in " + padLeft(r.in, inW) + g.Sep + "out " + padLeft(r.out, outW)
	}
	pad := 16 - lipgloss.Width(r.label)
	if pad < 1 {
		pad = 1
	}
	return " " + th.Muted.Render(r.label) + strings.Repeat(" ", pad) + th.Text.Render(value)
}

// gridLeftW is the fixed left-column width in the two-column stats grid
// (bpWide only) — generous enough for its longest label plus a
// comfortably long scalar value, leaving the rest of contentW to the
// right column's wider triads.
const gridLeftW = 28

// padCell pads (or truncates) s to exactly w display cells.
func padCell(s string, w int) string {
	s = truncate(s, w, "")
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// renderTwoColumnGrid lays left/right side by side. left always has fewer
// rows than right (statGridColumns), so it renders top-aligned and simply
// runs out of content early — ordinary unequal-column whitespace, not the
// old dead gap mid-column. The right column's mini-table columns get the
// remaining width; pathological cost precision truncates through the same
// ellipsis every other row in the app already relies on.
func renderTwoColumnGrid(th Theme, g Glyphs, left, right []statGridRow, contentW int) []string {
	rightW := contentW - gridLeftW - 2
	totalW, inW, outW := tripleWidths(right)
	rows := make([]string, len(right))
	for i := range rows {
		var lRow statGridRow
		if i < len(left) {
			lRow = left[i]
		}
		l := padCell(renderScalarRow(th, g, lRow), gridLeftW)
		r := truncate(renderTripleRow(th, g, right[i], totalW, inW, outW, true), rightW, g.Ellipsis)
		rows[i] = truncate(l+"  "+r, contentW, g.Ellipsis)
	}
	return rows
}

// renderOneColumnGrid stacks left then a blank divider then right —
// always len(left)+1+len(right) lines regardless of showSplit, so
// resizing across the one-column breakpoints never jitters the grid's
// height (tui/SPEC.md §4's "whitespace never jitters" rule).
func renderOneColumnGrid(th Theme, g Glyphs, left, right []statGridRow, showSplit bool) []string {
	totalW, inW, outW := tripleWidths(right)
	rows := make([]string, 0, len(left)+1+len(right))
	for _, r := range left {
		rows = append(rows, renderScalarRow(th, g, r))
	}
	rows = append(rows, "")
	for _, r := range right {
		rows = append(rows, renderTripleRow(th, g, r, totalW, inW, outW, showSplit))
	}
	return rows
}

// renderStatsGrid dispatches the stats grid by breakpoint: bpWide gets
// the two-column layout; bpStandard/bpNarrow collapse to one column with
// full triads; bpMini additionally collapses every triad to total-only.
// The narrowest tier prefers one clean number over a partial split,
// matching the app's existing width philosophy (tui/SPEC.md §4:
// bpStandard's bars list already prefers a single aggregate count over a
// misleading in→out shorthand when only one number fits).
func renderStatsGrid(th Theme, g Glyphs, st core.ModelStat, l layout) []string {
	left, right := statGridColumns(st)
	switch l.bp {
	case bpWide:
		return renderTwoColumnGrid(th, g, left, right, l.contentW)
	case bpStandard, bpNarrow:
		return renderOneColumnGrid(th, g, left, right, true)
	default: // bpMini
		return renderOneColumnGrid(th, g, left, right, false)
	}
}

// providerColumn is one provider-table column: header text, the label
// used when the bpMini fallback stacks it as a kv row instead ("" for
// PROVIDER, which becomes that fallback's own block header), alignment,
// and how to derive its cell text from (model, provider). Building the
// active column list as data — not per-breakpoint hand-duplicated
// rendering code — keeps the header/style/cell logic in one place
// regardless of tier.
type providerColumn struct {
	header    string
	listLabel string
	right     bool
	cell      func(core.ModelStat, core.ProviderStat) string
}

// providerLabel renders a provider's routed slug, "—" when unreported
// (matches ProviderStat.Slug's own doc comment).
func providerLabel(p core.ProviderStat) string {
	if p.Slug == nil {
		return "—"
	}
	return *p.Slug
}

var (
	providerColProvider = providerColumn{header: "PROVIDER", cell: func(_ core.ModelStat, p core.ProviderStat) string { return providerLabel(p) }}
	providerColReq      = providerColumn{header: "REQ", listLabel: "req", right: true, cell: func(_ core.ModelStat, p core.ProviderStat) string { return core.FormatTokens(p.Requests) }}
	providerColAvgDur   = providerColumn{header: "AVG DUR", listLabel: "avg dur", right: true, cell: func(_ core.ModelStat, p core.ProviderStat) string { return fmtAvgDuration(p.AvgDurationMS()) }}
	providerColTokens   = providerColumn{header: "TOKENS", listLabel: "tokens", right: true, cell: func(_ core.ModelStat, p core.ProviderStat) string { return core.FormatTokens(p.TotalTokens()) }}
	providerColCost     = providerColumn{header: "COST", listLabel: "cost", right: true, cell: func(_ core.ModelStat, p core.ProviderStat) string { return core.FormatCost(p.Cost) }}
	providerColInRate   = providerColumn{header: "IN $/1M", listLabel: "in $/1m", right: true, cell: func(_ core.ModelStat, p core.ProviderStat) string { return core.FormatRate(p.EffectiveInputRate()) }}
	providerColOutRate  = providerColumn{header: "OUT $/1M", listLabel: "out $/1m", right: true, cell: func(_ core.ModelStat, p core.ProviderStat) string { return core.FormatRate(p.EffectiveOutputRate()) }}
	providerColShare    = providerColumn{header: "SHARE", listLabel: "share", right: true, cell: func(m core.ModelStat, p core.ProviderStat) string { return core.FormatPct(p.SharePct(m.Cost)) }}
)

// providerColsFull is every provider-table column in canonical
// left-to-right display order.
var providerColsFull = []providerColumn{
	providerColProvider, providerColReq, providerColAvgDur, providerColTokens,
	providerColCost, providerColInRate, providerColOutRate, providerColShare,
}

// providerOptionalHeaderOrder lists the columns beyond the required core
// (PROVIDER/REQ/COST/SHARE, which survive to the narrowest tabular tier)
// in the order they're added back as width allows: tokens first — it was
// already the widest breakpoint's 5th column before this tier ladder
// existed — then the per-provider rate/latency diagnostics, which mostly
// duplicate the stats-grid's model-level totals above (tui/SPEC.md §2
// Stage E).
var providerOptionalHeaderOrder = []string{"TOKENS", "AVG DUR", "IN $/1M", "OUT $/1M"}

// providerColumnsFor returns the required core columns plus the first n
// (0..len(providerOptionalHeaderOrder)) optional columns, in canonical
// display order — the building block fitProviderColumns searches over to
// find the widest set that still fits.
func providerColumnsFor(n int) []providerColumn {
	included := map[string]bool{"PROVIDER": true, "REQ": true, "COST": true, "SHARE": true}
	for _, h := range providerOptionalHeaderOrder[:n] {
		included[h] = true
	}
	cols := make([]providerColumn, 0, len(included))
	for _, c := range providerColsFull {
		if included[c.header] {
			cols = append(cols, c)
		}
	}
	return cols
}

// maxLineWidth is the widest rendered line in a block of text — used both
// to measure a candidate provider-table rendering against its width
// budget and to size the details block's centering margin.
func maxLineWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	return w
}

// fitProviderColumns picks the widest provider-table column set whose
// natural rendered width fits capW, trying 8 columns down to the required
// 4 one at a time — so the table sheds a single column as width tightens
// instead of three at once — and falling back to nil (renderProviderList
// takes over) when even the required 4 don't fit (tui/SPEC.md §2 Stage E
// refinement).
func fitProviderColumns(th Theme, g Glyphs, st core.ModelStat, capW int) []providerColumn {
	for n := len(providerOptionalHeaderOrder); n >= 0; n-- {
		cols := providerColumnsFor(n)
		if maxLineWidth(renderProviderTableLines(th, g, st, cols)) <= capW {
			return cols
		}
	}
	return nil
}

// renderProviderTableLines renders the provider-split table for a given
// column set at its natural width — no forced stretch to fill available
// space, which used to balloon column padding the moment a breakpoint
// dropped several columns at once — as a bare header/column rule
// (Theme.TableRule), deliberately without a box border: OverlayBorder is
// reserved for the app's one transient overlay (modal.go/overlay.go), so
// this persistent in-content structure stays visually lighter.
func renderProviderTableLines(th Theme, g Glyphs, st core.ModelStat, cols []providerColumn) []string {
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.header
	}
	rows := make([][]string, len(st.Providers))
	for i, p := range st.Providers {
		row := make([]string, len(cols))
		for j, c := range cols {
			row[j] = c.cell(st, p)
		}
		rows[i] = row
	}

	border := lipgloss.NormalBorder()
	if g.Arrow == ">" { // ASCII mode (styles.go asciiGlyphs)
		border = lipgloss.ASCIIBorder()
	}
	styleFunc := func(row, col int) lipgloss.Style {
		s := th.Text.Padding(0, 1)
		if row == table.HeaderRow {
			s = th.Muted.Padding(0, 1)
		}
		if col >= 0 && col < len(cols) && cols[col].right {
			s = s.Align(lipgloss.Right)
		}
		return s
	}

	tbl := table.New().
		Headers(headers...).
		Rows(rows...).
		Wrap(false).
		Border(border).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderRow(false).
		BorderStyle(th.TableRule).
		StyleFunc(styleFunc)

	return strings.Split(tbl.String(), "\n")
}

// kvLine formats one indented label+value line — the idiom both the
// bpMini provider-list fallback and its per-provider stat blocks use.
func kvLine(th Theme, k, v string) string {
	pad := 8 - lipgloss.Width(k)
	if pad < 1 {
		pad = 1
	}
	return "   " + th.Muted.Render(k) + strings.Repeat(" ", pad) + th.Text.Render(v)
}

// renderProviderList is the table's width-exhausted fallback: each
// provider becomes a mini header (its slug) plus a stat-block covering
// every provider-table column except PROVIDER itself (now the block's own
// header) — all 8 stats as rows, not just the 4 the old bpNarrow table
// tier kept, since scrolling is cheap and losing data isn't (tui/SPEC.md
// §2 Stage E refinement).
func renderProviderList(th Theme, g Glyphs, st core.ModelStat, capW int) []string {
	var lines []string
	for i, p := range st.Providers {
		if i > 0 {
			lines = append(lines, "")
		}
		label := truncate(providerLabel(p), capW-1, g.Ellipsis)
		lines = append(lines, " "+th.AccentPrimary.Bold(true).Render(label))
		for _, c := range providerColsFull {
			if c.listLabel == "" {
				continue
			}
			lines = append(lines, kvLine(th, c.listLabel, c.cell(st, p)))
		}
	}
	return lines
}

// renderProviderSection is region-agnostic: a muted section header, then
// either the natural-width table (fitProviderColumns found a column set
// that fits capW) or the stacked list (nothing fit) — or a friendly empty
// state when the model has no provider breakdown at all.
func renderProviderSection(th Theme, g Glyphs, st core.ModelStat, capW int) []string {
	lines := []string{th.Muted.Render(" provider split"), ""}
	if len(st.Providers) == 0 {
		return append(lines, " "+th.Muted.Render("no provider data"))
	}
	if cols := fitProviderColumns(th, g, st, capW); cols != nil {
		return append(lines, renderProviderTableLines(th, g, st, cols)...)
	}
	return append(lines, renderProviderList(th, g, st, capW)...)
}

// detailsMaxW caps the details block's rendered width so it doesn't
// stretch across an ultra-wide terminal — content centers in whatever
// room is left instead (tui/SPEC.md §2 Stage E refinement). Tuned by
// feel via VHS, not tied to the breakpoint ladder.
const detailsMaxW = 96

// detailsCapW is the width ceiling actually available to the details
// block this render: the narrower of the terminal's content width and
// detailsMaxW.
func detailsCapW(contentW int) int {
	return min(contentW, detailsMaxW)
}

// detailsContent builds the full flat content-line list for one model —
// shared by renderDetails (which windows it to listHeight) and the
// scroll handlers (detailsViewport, which only need its length). Grid and
// table content render against a capped-width copy of l (detailsCapW) so
// nothing stretches past detailsMaxW; the block's own natural width (never
// more than the cap) then decides how much left margin centers it in the
// full terminal width, the same lipgloss.Place idea view.go/overlay.go
// already use for the too-small message and the transient overlays.
func (m Model) detailsContent(st core.ModelStat, l layout) []string {
	th, g := m.theme, m.glyphs
	dl := l
	dl.contentW = detailsCapW(l.contentW)

	lines := []string{
		th.Selected.Render(g.Select+truncateName(st.Name, dl.contentW-1, g.Ellipsis)) +
			"  " + th.Muted.Render("("+m.tf.Label()+")"),
		"",
	}
	lines = append(lines, renderStatsGrid(th, g, st, dl)...)
	lines = append(lines, "")
	lines = append(lines, renderProviderSection(th, g, st, dl.contentW)...)

	margin := max(0, (m.width-maxLineWidth(lines))/2)
	pad := strings.Repeat(" ", margin)
	for i, r := range lines {
		lines[i] = truncate(pad+r, m.width, g.Ellipsis)
	}
	return lines
}

// renderDetails draws region 2 of the details screen as exactly
// layout.listHeight rows.
func (m Model) renderDetails(l layout) []string {
	th := m.theme
	st, ok := m.selectedModel()
	if !ok {
		return padRows([]string{"", " " + th.Muted.Render("nothing selected")}, l.listHeight)
	}
	lines := m.detailsContent(st, l)
	vp := computeDetailsViewport(len(lines), l.listHeight)
	scroll := vp.clampScroll(m.detailsScroll)
	return renderDetailsViewport(lines, vp, scroll, l.listHeight, m.theme, m.glyphs)
}

// renderDetailsViewport windows a flat line list to exactly listHeight
// rows, reusing the bars list's "N more" idiom (Glyphs.MoreUp/MoreDown)
// line-indexed instead of block-indexed.
func renderDetailsViewport(lines []string, vp detailsViewport, scroll, listHeight int, th Theme, g Glyphs) []string {
	if !vp.scrolling {
		return padRows(lines, listHeight)
	}
	above := ""
	if scroll > 0 {
		above = th.Muted.Render(" " + g.MoreUp + " " + strconv.Itoa(scroll) + " more")
	}
	out := []string{above}
	end := scroll + vp.visible
	if end > len(lines) {
		end = len(lines)
	}
	out = append(out, lines[scroll:end]...)
	below := ""
	if remaining := len(lines) - end; remaining > 0 {
		below = th.Muted.Render(" " + g.MoreDown + " " + strconv.Itoa(remaining) + " more")
	}
	out = append(out, below)
	return padRows(out, listHeight)
}

// padRows fits a row slice to exactly h rows, padding with blanks or
// cutting from the end.
func padRows(rows []string, h int) []string {
	for len(rows) < h {
		rows = append(rows, "")
	}
	return rows[:h]
}
