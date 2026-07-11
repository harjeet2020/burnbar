// Package core is the pure heart of the TUI: domain types, window math,
// aggregation, bar geometry, and formatting. Nothing in this package
// touches the network, the terminal, or the clock — everything is a pure
// function of its inputs, testable with go test alone (tui/SPEC.md §10).
//
// Stage A ships the domain types, provisional formatting and bar-geometry
// helpers, and fixture data; Stage B adds window math, aggregate(), and
// the exhaustive test suite that formalizes the provisional helpers.
package core

import "time"

// Timeframe is the active aggregation window. "today" is the user's local
// calendar day; week/month are the current UTC calendar week (Mon-Sun)
// and UTC calendar month (root SPEC §2).
type Timeframe int

// The three selectable windows, in `t`-cycle order.
const (
	TimeframeToday Timeframe = iota
	TimeframeWeek
	TimeframeMonth
)

// Label returns the lowercase UI label used in the header selector.
func (t Timeframe) Label() string {
	switch t {
	case TimeframeWeek:
		return "week"
	case TimeframeMonth:
		return "month"
	default:
		return "today"
	}
}

// Next cycles today → week → month → today (the `t` key, tui/SPEC.md §2).
func (t Timeframe) Next() Timeframe { return (t + 1) % 3 }

// Timeframes lists all windows in display order for the header selector.
var Timeframes = []Timeframe{TimeframeToday, TimeframeWeek, TimeframeMonth}

// Mode is the active bar display mode (tui/SPEC.md §3): each mode is
// single-denomination end to end — length, interior split, sort order, and
// the latest-burst highlight all speak one language. ModeCost is the zero
// value (burnbar is a money meter first).
type Mode int

const (
	ModeCost Mode = iota
	ModeTokens
)

// Toggle flips between the two modes (the `m` key, tui/SPEC.md §2/§3).
func (md Mode) Toggle() Mode {
	if md == ModeCost {
		return ModeTokens
	}
	return ModeCost
}

// ModelStat is one model's aggregate over the active window — the shape
// the bars list and details screen render from. In Stage B this becomes
// the output of aggregate(); Stage A fills it from fixtures.
//
// NULL vs 0 (root SPEC §2): pointer fields distinguish "the payload did
// not report this" (nil → render "—") from "reported as zero" (0). Ratios
// derived from nil sums must never be computed.
type ModelStat struct {
	// Name is the model alias slug, e.g. "deepseek/deepseek-v4-flash".
	Name string
	// Requests is the number of requests in the window.
	Requests int64
	// InputTokens and OutputTokens are summed over the window.
	InputTokens  int64
	OutputTokens int64
	// Cost is the total window cost in USD.
	Cost float64
	// InputCost / OutputCost are the summed actual split costs (cache
	// discounts already embodied). nil when the payload never reported
	// them — the bar segment split then falls back to token volumes
	// (tui/SPEC.md §3).
	InputCost  *float64
	OutputCost *float64
	// CachedTokens, ReasoningTokens, and DurationMSSum are nullable
	// window sums for the details screen; nil when never reported in
	// the window (SQL SUM semantics — see DailyRow).
	CachedTokens    *int64
	ReasoningTokens *int64
	DurationMSSum   *int64
	// TimedRequests counts requests that reported a duration — the
	// avg-duration denominator (root SPEC §2).
	TimedRequests int64
	// Providers is the per-(model, provider_slug) grain for the details
	// screen's split table, sorted by cost descending (ties slug
	// ascending, unreported slug last).
	Providers []ProviderStat
}

// ProviderStat is one (model, provider_slug) grain row — the details
// screen's provider split table (tui/SPEC.md §2). Same sums and pointer
// discipline as ModelStat; Slug is nil when the payload never reported
// the routed provider (renders "—").
type ProviderStat struct {
	// Slug is the routed provider including deployment variant, e.g.
	// "novita/fp8"; nil when unreported.
	Slug *string
	// Requests and the token/cost sums, over the window.
	Requests     int64
	InputTokens  int64
	OutputTokens int64
	Cost         float64
	// Nullable sums, as on ModelStat.
	CachedTokens    *int64
	ReasoningTokens *int64
	InputCost       *float64
	OutputCost      *float64
	DurationMSSum   *int64
	// TimedRequests counts requests that reported a duration.
	TimedRequests int64
}

// TotalTokens is the bar-length driver in token mode: input + output
// (tui/SPEC.md §3).
func (m ModelStat) TotalTokens() int64 { return m.InputTokens + m.OutputTokens }

// TotalTokens — see ModelStat.TotalTokens. The details screen's provider
// table shows this aggregated figure rather than the in/out split
// (tui/SPEC.md §2 Stage E).
func (p ProviderStat) TotalTokens() int64 { return p.InputTokens + p.OutputTokens }

// Value returns the active mode's driving number — cost or total tokens —
// the single place that decides what drives bar length, sort order, and
// scale (tui/SPEC.md §3).
func (m ModelStat) Value(mode Mode) float64 {
	if mode == ModeTokens {
		return float64(m.TotalTokens())
	}
	return m.Cost
}

// ConnState is the live-source connection state shown in the status row —
// always rendered as symbol + word, never color alone (tui/SPEC.md §2).
type ConnState int

// Connection states, healthiest first.
const (
	ConnLive ConnState = iota
	ConnPolling
	ConnReconnecting
	ConnOffline
)

// Snapshot is everything the meter screen renders for one timeframe. In
// Stage A it is produced by Fixture(); later stages assemble it from the
// real stores (baseline, rows, credits, connection).
type Snapshot struct {
	// Timeframe this snapshot was aggregated for.
	Timeframe Timeframe
	// Models, sorted by window cost descending, ties by name ascending —
	// the stable order that prevents list jitter (tui/SPEC.md §2).
	Models []ModelStat
	// Spend is the total cost of the window (header row 2, cost mode).
	Spend float64
	// TotalTokens is the total token volume of the window (header row 2,
	// token mode, tui/SPEC.md §3).
	TotalTokens int64
	// Credits is the remaining OpenRouter balance; nil when unknown (no
	// key configured, or not yet fetched).
	Credits *float64
	// CreditsAt is when Credits was last fetched; its age is always shown.
	CreditsAt time.Time
	// LastRequestAt is the wall time of the newest request in the window;
	// zero when the window is empty.
	LastRequestAt time.Time
	// LagSeconds is the meter lag (request concluded → seen); nil renders
	// as "—" (clock skew clamp, tui/SPEC.md §8).
	LagSeconds *float64
	// Conn is the live-source connection state.
	Conn ConnState
}
