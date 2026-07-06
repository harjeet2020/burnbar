package core

import (
	"testing"
	"time"

	// Hermetic zone lookups: embed the tz database so these tests pass
	// on machines without system zoneinfo.
	_ "time/tzdata"
)

// zone loads a tz database location or fails the test.
func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func TestLocalMidnight(t *testing.T) {
	tests := []struct {
		name string
		zone string
		now  time.Time // built in the zone below via time.Date fields
		want time.Time
	}{
		{
			name: "UTC afternoon",
			zone: "UTC",
			now:  time.Date(2026, 7, 5, 15, 30, 0, 0, time.UTC),
			want: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "Kolkata +05:30 half-hour offset",
			zone: "Asia/Kolkata",
			// 20:00 UTC on the 5th is already 01:30 on the 6th in
			// Kolkata — the local cut must pick the 6th.
			now:  time.Date(2026, 7, 5, 20, 0, 0, 0, time.UTC),
			want: time.Date(2026, 7, 6, 0, 0, 0, 0, zoneMust("Asia/Kolkata")),
		},
		{
			name: "Chatham +12:45 quarter-hour offset",
			zone: "Pacific/Chatham",
			now:  time.Date(2026, 7, 5, 14, 0, 0, 0, zoneMust("Pacific/Chatham")),
			want: time.Date(2026, 7, 5, 0, 0, 0, 0, zoneMust("Pacific/Chatham")),
		},
		{
			name: "New York negative offset",
			zone: "America/New_York",
			// 02:00 UTC on the 6th is still 22:00 on the 5th in NY.
			now:  time.Date(2026, 7, 6, 2, 0, 0, 0, time.UTC),
			want: time.Date(2026, 7, 5, 0, 0, 0, 0, zoneMust("America/New_York")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := zone(t, tt.zone)
			got := LocalMidnight(tt.now, loc)
			if !got.Equal(tt.want) {
				t.Errorf("LocalMidnight(%v, %s) = %v, want %v", tt.now, tt.zone, got, tt.want)
			}
		})
	}
}

// zoneMust is the table-literal companion of zone(); panicking in a
// table literal is acceptable because LoadLocation only fails when the
// embedded tzdata is broken.
func zoneMust(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestLocalMidnightBoundary(t *testing.T) {
	loc := zone(t, "Asia/Kolkata")
	midnight := time.Date(2026, 7, 6, 0, 0, 0, 0, loc)

	if got := LocalMidnight(midnight.Add(-time.Nanosecond), loc); !got.Equal(midnight.AddDate(0, 0, -1)) {
		t.Errorf("1ns before midnight: got %v, want previous day's midnight", got)
	}
	if got := LocalMidnight(midnight, loc); !got.Equal(midnight) {
		t.Errorf("exactly midnight: got %v, want %v", got, midnight)
	}
	if got := LocalMidnight(midnight.Add(time.Nanosecond), loc); !got.Equal(midnight) {
		t.Errorf("1ns after midnight: got %v, want %v", got, midnight)
	}
}

func TestNextLocalMidnightDST(t *testing.T) {
	ny := zone(t, "America/New_York")

	t.Run("spring forward makes a 23h day", func(t *testing.T) {
		// US DST starts 2026-03-08 (02:00 → 03:00).
		now := time.Date(2026, 3, 8, 12, 0, 0, 0, ny)
		day := NextLocalMidnight(now, ny).Sub(LocalMidnight(now, ny))
		if day != 23*time.Hour {
			t.Errorf("spring-forward day length = %v, want 23h", day)
		}
	})

	t.Run("fall back makes a 25h day", func(t *testing.T) {
		// US DST ends 2026-11-01 (02:00 → 01:00).
		now := time.Date(2026, 11, 1, 12, 0, 0, 0, ny)
		day := NextLocalMidnight(now, ny).Sub(LocalMidnight(now, ny))
		if day != 25*time.Hour {
			t.Errorf("fall-back day length = %v, want 25h", day)
		}
	})

	t.Run("strictly after now at exactly midnight", func(t *testing.T) {
		midnight := time.Date(2026, 7, 6, 0, 0, 0, 0, ny)
		next := NextLocalMidnight(midnight, ny)
		if !next.After(midnight) {
			t.Errorf("NextLocalMidnight(midnight) = %v, must be strictly after %v", next, midnight)
		}
	})
}

func TestLocalMidnightDSTAtMidnight(t *testing.T) {
	// Chile springs forward at midnight: on the changeover day 00:00
	// does not exist. The Stage D rollover timer must still get a
	// valid instant and never spin (tui/SPEC.md §7 "DST handled for
	// free" only holds if these invariants do).
	scl := zone(t, "America/Santiago")
	// 2026-09-06 is the expected changeover day; even if tzdata shifts
	// the exact date, the invariants below must hold for any day.
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, scl)

	m := LocalMidnight(now, scl)
	if m.After(now) {
		t.Errorf("LocalMidnight = %v is after now %v", m, now)
	}
	if now.Sub(m) > 25*time.Hour {
		t.Errorf("LocalMidnight = %v is more than 25h before now %v", m, now)
	}
	next := NextLocalMidnight(now, scl)
	if !next.After(now) {
		t.Errorf("NextLocalMidnight = %v, must be strictly after now %v", next, now)
	}
	if next.Sub(now) > 25*time.Hour {
		t.Errorf("NextLocalMidnight = %v is more than 25h after now %v", next, now)
	}
}

func TestUTCDayHelpers(t *testing.T) {
	dayStart := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)

	t.Run("UTCDayStart just before next midnight", func(t *testing.T) {
		now := time.Date(2026, 7, 5, 23, 59, 59, 999_999_999, time.UTC)
		if got := UTCDayStart(now); !got.Equal(dayStart) {
			t.Errorf("UTCDayStart = %v, want %v", got, dayStart)
		}
	})

	t.Run("UTCDayStart at exactly midnight", func(t *testing.T) {
		if got := UTCDayStart(dayStart); !got.Equal(dayStart) {
			t.Errorf("UTCDayStart = %v, want %v", got, dayStart)
		}
	})

	t.Run("UTCDayStart normalizes zoned input", func(t *testing.T) {
		// 22:00 NY on the 5th is 02:00 UTC on the 6th.
		ny := zone(t, "America/New_York")
		now := time.Date(2026, 7, 5, 22, 0, 0, 0, ny)
		want := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
		if got := UTCDayStart(now); !got.Equal(want) {
			t.Errorf("UTCDayStart = %v, want %v", got, want)
		}
	})

	t.Run("NextUTCMidnight strictly after at exactly midnight", func(t *testing.T) {
		next := NextUTCMidnight(dayStart)
		want := dayStart.AddDate(0, 0, 1)
		if !next.Equal(want) {
			t.Errorf("NextUTCMidnight(midnight) = %v, want %v", next, want)
		}
	})
}

func TestUTCWindowStart(t *testing.T) {
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)

	t.Run("week is D-6 .. D inclusive", func(t *testing.T) {
		start := UTCWindowStart(now, 7)
		want := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
		if !start.Equal(want) {
			t.Fatalf("UTCWindowStart(7) = %v, want %v", start, want)
		}
		// Membership through the same predicate Aggregate uses.
		dayIn := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)  // D-6
		dayOut := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC) // D-7
		if dayIn.Before(start) {
			t.Error("D-6 must be inside the week window")
		}
		if !dayOut.Before(start) {
			t.Error("D-7 must be outside the week window")
		}
	})

	t.Run("month is D-29 .. D inclusive", func(t *testing.T) {
		start := UTCWindowStart(now, 30)
		want := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
		if !start.Equal(want) {
			t.Fatalf("UTCWindowStart(30) = %v, want %v", start, want)
		}
		dayIn := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)  // D-29
		dayOut := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC) // D-30
		if dayIn.Before(start) {
			t.Error("D-29 must be inside the month window")
		}
		if !dayOut.Before(start) {
			t.Error("D-30 must be outside the month window")
		}
	})
}

func TestWindowStartDispatch(t *testing.T) {
	loc := zone(t, "Asia/Kolkata")
	now := time.Date(2026, 7, 5, 20, 0, 0, 0, time.UTC) // 01:30 on the 6th in Kolkata

	if got, want := WindowStart(TimeframeToday, now, loc), LocalMidnight(now, loc); !got.Equal(want) {
		t.Errorf("today: %v, want %v", got, want)
	}
	if got, want := WindowStart(TimeframeWeek, now, loc), UTCWindowStart(now, 7); !got.Equal(want) {
		t.Errorf("week: %v, want %v", got, want)
	}
	if got, want := WindowStart(TimeframeMonth, now, loc), UTCWindowStart(now, 30); !got.Equal(want) {
		t.Errorf("month: %v, want %v", got, want)
	}
}
