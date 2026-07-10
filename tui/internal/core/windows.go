// Window math (tui/SPEC.md §7): the today window is the user's LOCAL
// calendar day; week is the current UTC calendar week (Monday-Sunday)
// and month is the current UTC calendar month — both anchored to UTC
// so they line up with the usage_daily baseline's UTC-day grain, at the
// accepted cost of the boundary being UTC rather than the user's local
// calendar. Everything here is a pure function of (now, loc) — core
// never reads time.Local or the wall clock, so tests can pin any zone,
// including :30/:45 offsets and DST transitions.

package core

import "time"

// LocalMidnight returns the start of the local calendar day containing
// now — the today-window cut. time.Date normalizes zones where midnight
// does not exist (DST-at-midnight zones like America/Santiago), so the
// result is always a valid instant within the day.
func LocalMidnight(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// NextLocalMidnight returns the first instant of the next local day —
// the today-rollover timer target (Stage D). Always strictly after now,
// including when now is exactly midnight.
func NextLocalMidnight(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, loc)
}

// UTCDayStart returns midnight UTC of now's UTC day — the grain the
// usage_daily baseline is bucketed on (root SPEC §2).
func UTCDayStart(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// NextUTCMidnight returns the start of the next UTC day — the
// week/month slide timer target (Stage D). Always strictly after now.
func NextUTCMidnight(now time.Time) time.Time {
	return UTCDayStart(now).AddDate(0, 0, 1)
}

// currentUTCWeekStart returns 00:00 UTC on the Monday of the UTC
// calendar week containing now. time.Weekday is Sunday=0..Saturday=6,
// so (weekday+6)%7 turns that into days-since-Monday for every day of
// the week, including Sunday itself (0 -> 6 days back).
func currentUTCWeekStart(now time.Time) time.Time {
	day := UTCDayStart(now)
	daysSinceMonday := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -daysSinceMonday)
}

// currentUTCMonthStart returns 00:00 UTC on the 1st of now's UTC
// calendar month.
func currentUTCMonthStart(now time.Time) time.Time {
	y, m, _ := now.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// WindowStart returns the inclusive lower bound of the active timeframe:
// local midnight for today, the current UTC calendar week/month start
// for week/month. Window membership is `!t.Before(start)` for both
// baseline Day values and raw RequestedAt instants.
func WindowStart(tf Timeframe, now time.Time, loc *time.Location) time.Time {
	switch tf {
	case TimeframeWeek:
		return currentUTCWeekStart(now)
	case TimeframeMonth:
		return currentUTCMonthStart(now)
	default:
		return LocalMidnight(now, loc)
	}
}
