// Window math (tui/SPEC.md §7): the today window is the user's LOCAL
// calendar day; week and month are the last 7/30 UTC days inclusive of
// the current UTC day. Everything here is a pure function of (now, loc)
// — core never reads time.Local or the wall clock, so tests can pin any
// zone, including :30/:45 offsets and DST transitions.

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

// UTCWindowStart returns the start of the last-N-UTC-days window
// inclusive of the current UTC day: days D−(N−1) through D. Window
// membership is `!t.Before(start)` for both baseline Day values and raw
// RequestedAt instants.
func UTCWindowStart(now time.Time, days int) time.Time {
	return UTCDayStart(now).AddDate(0, 0, -(days - 1))
}

// WindowStart returns the inclusive lower bound of the active timeframe:
// local midnight for today, the 7/30-UTC-day starts for week/month.
func WindowStart(tf Timeframe, now time.Time, loc *time.Location) time.Time {
	switch tf {
	case TimeframeWeek:
		return UTCWindowStart(now, 7)
	case TimeframeMonth:
		return UTCWindowStart(now, 30)
	default:
		return LocalMidnight(now, loc)
	}
}
