// Formatting rules (tui/SPEC.md §9) — deterministic, locale-independent.
// Provisional in Stage A; Stage B owns the unit-test suite.

package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// FormatTokens renders token quantities at ≤3 significant digits with one
// decimal for K/M/B units: 842 · 12.5K · 1.2M · 3.4B. Round values render
// bare (500K, 2M) — which is also why ladder scale values look clean in
// the status-row chip (tui/SPEC.md §9).
func FormatTokens(n int64) string {
	if n < 0 {
		return "0"
	}
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	units := []struct {
		div    float64
		suffix string
	}{
		{1e3, "K"},
		{1e6, "M"},
		{1e9, "B"},
	}
	for i, u := range units {
		v := float64(n) / u.div
		// Values that would round to ≥1000 belong to the next unit up
		// (999,950 → "1M", not "1000.0K").
		if v >= 999.95 && i < len(units)-1 {
			continue
		}
		if v >= 99.95 {
			// 3 integer digits — no room for a decimal.
			return fmt.Sprintf("%.0f%s", v, u.suffix)
		}
		s := fmt.Sprintf("%.1f%s", v, u.suffix)
		return strings.Replace(s, ".0"+u.suffix, u.suffix, 1)
	}
	return "" // unreachable: the B branch always returns
}

// FormatCost renders USD amounts with adaptive precision (tui/SPEC.md §9):
// $123 (≥100) · $12.34 (≥1) · $0.4821 (≥0.001) · $0.000073 (below: enough
// digits for two significant figures).
func FormatCost(c float64) string {
	switch {
	case c >= 100:
		return fmt.Sprintf("$%.0f", c)
	case c >= 1:
		return fmt.Sprintf("$%.2f", c)
	case c >= 0.001:
		return fmt.Sprintf("$%.4f", c)
	case c > 0:
		// Two significant figures: precision = digits to the first
		// significant decimal + 1 (0.000073 → 6).
		precision := -int(math.Floor(math.Log10(c))) + 1
		return fmt.Sprintf("$%.*f", precision, c)
	default:
		return "$0.00"
	}
}

// FormatCredits renders the credit balance — always two decimals
// (tui/SPEC.md §9).
func FormatCredits(c float64) string { return fmt.Sprintf("$%.2f", c) }

// FormatDuration renders elapsed spans: 840ms under a second, 2.1s under
// a minute, 1m 12s beyond (tui/SPEC.md §9).
func FormatDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "—"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm %ds", m, s)
	}
}

// FormatAge renders value ages for the header and status row: "now" under
// 30s, then whole minutes ("2m"), hours ("1h"), and days (tui/SPEC.md §9).
func FormatAge(d time.Duration) string {
	switch {
	case d < 30*time.Second:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// FormatRelative renders the parenthesized relative form used next to
// wall-clock timestamps: "(2m ago)", or "(now)" under 30 seconds.
func FormatRelative(d time.Duration) string {
	if d < 30*time.Second {
		return "(now)"
	}
	return "(" + FormatAge(d) + " ago)"
}

// FormatClock renders a local wall-clock timestamp, HH:MM:SS
// (tui/SPEC.md §9).
func FormatClock(t time.Time) string { return t.Local().Format("15:04:05") }
