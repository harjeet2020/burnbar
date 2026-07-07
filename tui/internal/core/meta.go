// Status-row derivations (tui/SPEC.md §2): the "last request" wall time
// and the meter lag. Both are pure functions of the window's rows, kept
// here beside the aggregation they accompany so the UI assembles a
// Snapshot without doing any time math of its own.

package core

import "time"

// SnapshotMeta derives the two status-row facts for the active window:
//
//   - lastRequestAt — the wall time (RequestedAt) of the newest request
//     in the window, across fetched and live rows alike; the zero time
//     when the window holds no rows (the empty-window state).
//   - lag — the meter lag of the most recent live arrival: the seconds
//     between a request concluding and the meter seeing it. nil when
//     there is no live row in the window, its duration was unreported,
//     or the figure comes out negative from clock skew (tui/SPEC.md §8).
//
// Window membership uses the same WindowStart bound as Aggregate, so the
// status row and the bars always agree on what "this window" contains.
// Lag is intentionally independent of aggregation source selection —
// week/month aggregate from the baseline, but their lag still reflects
// the newest live event, which is what "how fresh is the meter" means.
func SnapshotMeta(rows []RequestRow, window Timeframe, now time.Time, loc *time.Location) (lastRequestAt time.Time, lag *float64) {
	start := WindowStart(window, now, loc)

	var newestLive *RequestRow
	for i := range rows {
		r := rows[i]
		if r.RequestedAt.Before(start) {
			continue
		}
		if r.RequestedAt.After(lastRequestAt) {
			lastRequestAt = r.RequestedAt
		}
		if r.Live && (newestLive == nil || arrivedAfter(r, *newestLive)) {
			newestLive = &rows[i]
		}
	}

	if newestLive != nil {
		lag = meterLag(*newestLive)
	}
	return lastRequestAt, lag
}

// LatestLive returns the most recently arrived live row (by ReceivedAt,
// with the same deterministic tie-break as accent2) and whether one
// exists — the single row Stage D's accent2-arrival emphasis keys off.
func LatestLive(rows []RequestRow) (RequestRow, bool) {
	var newest *RequestRow
	for i := range rows {
		if !rows[i].Live {
			continue
		}
		if newest == nil || arrivedAfter(rows[i], *newest) {
			newest = &rows[i]
		}
	}
	if newest == nil {
		return RequestRow{}, false
	}
	return *newest, true
}

// meterLag is the "request concluded → seen by the meter" figure for one
// live row: ReceivedAt − (RequestedAt + DurationMS). nil when the
// duration was unreported or the result is negative (clock skew clamp,
// tui/SPEC.md §8) — a wrong lag is worse than none.
func meterLag(r RequestRow) *float64 {
	if r.DurationMS == nil {
		return nil
	}
	concluded := r.RequestedAt.Add(time.Duration(*r.DurationMS) * time.Millisecond)
	secs := r.ReceivedAt.Sub(concluded).Seconds()
	if secs < 0 {
		return nil
	}
	return &secs
}
