package core

import (
	"testing"
	"time"
)

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{-5, "0"}, // defensive: negatives render as nothing burned
		{0, "0"},
		{842, "842"},
		{999, "999"},
		{1_000, "1K"},
		{10_000, "10K"}, // ladder values render bare (scale chip)
		{12_500, "12.5K"},
		{99_949, "99.9K"},
		{99_950, "100K"}, // 3 integer digits leave no room for a decimal
		{500_000, "500K"},
		{999_449, "999K"}, // %.0f rounds down: stays in K
		{999_500, "1M"},   // %.0f would print "1000K" — belongs to the next unit
		{999_949, "1M"},
		{1_200_000, "1.2M"},
		{2_000_000, "2M"},
		{3_400_000_000, "3.4B"},
		{1_000_000_000_000, "1000B"}, // terminal unit: no T, digits just grow
	}
	for _, tt := range tests {
		if got := FormatTokens(tt.n); got != tt.want {
			t.Errorf("FormatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		c    float64
		want string
	}{
		{-0.5, "$0.00"}, // defensive: negatives collapse to zero
		{0, "$0.00"},
		{123.4, "$123"},
		{100, "$100"},
		{99.99, "$99.99"},
		{12.34, "$12.34"},
		{1, "$1.00"},
		{0.4821, "$0.4821"},
		{0.001, "$0.0010"},
		{0.00099, "$0.00099"}, // two significant figures below the 0.001 tier
		{0.000073, "$0.000073"},
		{0.0009999, "$0.00100"}, // rounding within the two-sig-fig precision
	}
	for _, tt := range tests {
		if got := FormatCost(tt.c); got != tt.want {
			t.Errorf("FormatCost(%v) = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestFormatCredits(t *testing.T) {
	tests := []struct {
		c    float64
		want string
	}{
		{12.43, "$12.43"},
		{0, "$0.00"},
		{1234.5, "$1234.50"}, // always two decimals, no rounding surprises
	}
	for _, tt := range tests {
		if got := FormatCredits(tt.c); got != tt.want {
			t.Errorf("FormatCredits(%v) = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Nanosecond, "—"}, // clock-skew clamp (tui/SPEC.md §8)
		{840 * time.Millisecond, "840ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1.0s"},
		{2100 * time.Millisecond, "2.1s"},
		{59_900 * time.Millisecond, "59.9s"},
		{time.Minute, "1m 0s"},
		{72 * time.Second, "1m 12s"},
		{3930 * time.Second, "65m 30s"}, // no hour unit — §9 stops at minutes
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.d); got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "now"},
		{29 * time.Second, "now"},
		{30 * time.Second, "1m"}, // rounds up — "0m" would read as a bug
		{45 * time.Second, "1m"},
		{90 * time.Second, "1m"},
		{2 * time.Minute, "2m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{25 * time.Hour, "1d"},
	}
	for _, tt := range tests {
		if got := FormatAge(tt.d); got != tt.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatRelative(t *testing.T) {
	if got := FormatRelative(10 * time.Second); got != "(now)" {
		t.Errorf("FormatRelative(10s) = %q, want (now)", got)
	}
	if got := FormatRelative(2 * time.Minute); got != "(2m ago)" {
		t.Errorf("FormatRelative(2m) = %q, want (2m ago)", got)
	}
}

func TestFormatClock(t *testing.T) {
	// FormatClock renders in time.Local — the one deliberate impurity
	// in core. Pin the zone by overriding the package variable for the
	// duration of the test (must not run in parallel).
	old := time.Local
	time.Local = time.FixedZone("TST", 5*3600+30*60) // +05:30
	defer func() { time.Local = old }()

	at := time.Date(2026, 7, 5, 12, 34, 56, 0, time.UTC)
	if got := FormatClock(at); got != "18:04:56" {
		t.Errorf("FormatClock = %q, want 18:04:56 (+05:30 of 12:34:56Z)", got)
	}
}
