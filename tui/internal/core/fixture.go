// Fixture data for Stage A: deterministic fake snapshots that exercise
// every layout path — magnitude spread for the shared scale, a long slug
// for truncation, a free model for the token-fallback split, NULL split
// costs for the "—" rule, and accent slices on the top model. Replaced by
// the real data layer in Stage C; kept afterwards for golden-render tests.

package core

import (
	"sort"
	"time"
)

// f64 makes pointer-typed cost literals readable in fixture tables.
func f64(v float64) *float64 { return &v }

// scaleStats multiplies a fixture's volume fields to fake a wider window
// while keeping proportions plausible. Costs scale linearly with tokens.
func scaleStats(models []ModelStat, factor float64) []ModelStat {
	out := make([]ModelStat, len(models))
	for i, m := range models {
		s := m
		s.Requests = int64(float64(m.Requests) * factor)
		s.InputTokens = int64(float64(m.InputTokens) * factor)
		s.OutputTokens = int64(float64(m.OutputTokens) * factor)
		s.Cost = m.Cost * factor
		if m.InputCost != nil {
			s.InputCost = f64(*m.InputCost * factor)
		}
		if m.OutputCost != nil {
			s.OutputCost = f64(*m.OutputCost * factor)
		}
		// Accents are about the last few minutes; they don't grow with
		// the window.
		out[i] = s
	}
	return out
}

// todayModels is the base fixture, pre-sorted by cost desc (ties name
// asc) exactly as aggregate() will emit in Stage B.
var todayModels = []ModelStat{
	{
		Name:         "deepseek/deepseek-v4-flash",
		Requests:     214,
		InputTokens:  1_210_000,
		OutputTokens: 340_200,
		Cost:         0.4821,
		InputCost:    f64(0.1934),
		OutputCost:   f64(0.2887),
		// The top model carries the accent slices: a burst since the
		// anchor plus the single most recent request.
		Accent1Tokens: 180_000,
		Accent2Tokens: 42_000,
	},
	{
		Name:         "anthropic/claude-haiku-4.5",
		Requests:     96,
		InputTokens:  210_400,
		OutputTokens: 88_100,
		Cost:         0.3110,
		InputCost:    f64(0.0842),
		OutputCost:   f64(0.2268),
	},
	{
		Name:         "openai/gpt-5.2-mini",
		Requests:     41,
		InputTokens:  402_300,
		OutputTokens: 51_800,
		Cost:         0.1204,
		InputCost:    f64(0.0721),
		OutputCost:   f64(0.0483),
	},
	{
		Name:         "qwen/qwen3-coder",
		Requests:     120,
		InputTokens:  955_000,
		OutputTokens: 82_300,
		Cost:         0.0214,
		InputCost:    f64(0.0126),
		OutputCost:   f64(0.0088),
	},
	{
		// Long slug: exercises middle truncation at narrow widths.
		Name:         "moonshotai/kimi-k2-thinking-preview-ultra-long-context",
		Requests:     8,
		InputTokens:  120_500,
		OutputTokens: 9_400,
		Cost:         0.0102,
		InputCost:    f64(0.0079),
		OutputCost:   f64(0.0023),
	},
	{
		// Free model with unreported split costs: token-fallback split
		// and the NULL-vs-0 rule in one row.
		Name:         "meta-llama/llama-4-maverick:free",
		Requests:     15,
		InputTokens:  88_000,
		OutputTokens: 12_400,
		Cost:         0,
	},
	{
		// Tiny model: min-width bar clamp.
		Name:         "mistralai/ministral-3b",
		Requests:     3,
		InputTokens:  2_800,
		OutputTokens: 400,
		Cost:         0.0004,
		InputCost:    f64(0.0003),
		OutputCost:   f64(0.0001),
	},
}

// weekExtra appears only in the wider windows, so cycling `t` visibly
// changes the model list, not just the numbers.
var weekExtra = ModelStat{
	Name:         "google/gemini-3-flash",
	Requests:     44,
	InputTokens:  480_000,
	OutputTokens: 61_000,
	Cost:         0.0655,
	InputCost:    f64(0.0402),
	OutputCost:   f64(0.0253),
}

// Fixture returns the fake snapshot for a timeframe. now anchors the
// relative timestamps (last request ≈ 2m ago, credits fetched ≈ 2m ago)
// so ages render believably in a live terminal.
func Fixture(tf Timeframe, now time.Time) Snapshot {
	var models []ModelStat
	switch tf {
	case TimeframeWeek:
		models = append(scaleStats(todayModels, 5.2), weekExtra)
	case TimeframeMonth:
		extra := weekExtra
		extra.Requests *= 4
		extra.InputTokens *= 4
		extra.OutputTokens *= 4
		extra.Cost *= 4
		extra.InputCost = f64(*weekExtra.InputCost * 4)
		extra.OutputCost = f64(*weekExtra.OutputCost * 4)
		models = append(scaleStats(todayModels, 18.4), extra)
	default:
		models = append([]ModelStat(nil), todayModels...)
	}
	sortModels(models)

	var spend float64
	for _, m := range models {
		spend += m.Cost
	}

	lag := 2.1
	return Snapshot{
		Timeframe:     tf,
		Models:        models,
		Spend:         spend,
		Credits:       f64(12.43),
		CreditsAt:     now.Add(-2 * time.Minute),
		LastRequestAt: now.Add(-2*time.Minute - 14*time.Second),
		LagSeconds:    &lag,
		Conn:          ConnLive,
	}
}

// sortModels applies the binding list order: window cost descending, ties
// by name ascending — stable, jitter-free (tui/SPEC.md §2).
func sortModels(models []ModelStat) {
	sort.Slice(models, func(i, j int) bool {
		if models[i].Cost != models[j].Cost {
			return models[i].Cost > models[j].Cost
		}
		return models[i].Name < models[j].Name
	})
}
