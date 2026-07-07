// Burst-grouped credits debounce (tui/SPEC.md §7) — a pure, clock-injected
// scheduler that decides *when* to poll the credits endpoint after
// broadcast events, without deferring one spend moment's poll behind an
// unrelated later one.
//
// Why the 70s delay: OpenRouter caches credit values for up to ~60s, so
// `lastEventInBurst + 70s` is the earliest poll guaranteed to observe the
// whole burst's spend. Why per-burst: events <10s apart are one working
// burst and share a single (pushed) timer; a ≥10s gap starts an
// independent burst whose poll fires on its own schedule.
//
// The scheduler emits Actions rather than touching timers directly,
// because Bubble Tea's tea.Tick cannot be cancelled: every armed poll
// carries an id, and a fired tick only polls if its id is still the
// current one for that poll (see OnTick). Pushing a timer just re-arms
// with a fresh id, leaving the stale tick to fire and be ignored.

package data

import (
	"sort"
	"time"
)

// Debounce constants (tui/SPEC.md §7).
const (
	// burstGap: events closer than this belong to the same burst.
	burstGap = 10 * time.Second
	// creditDelay: how long after an event to poll, covering OpenRouter's
	// ~60s credit cache.
	creditDelay = 70 * time.Second
	// maxPush: a pending poll's target is never pushed more than this past
	// its first scheduling, so a minutes-long rapid burst still yields
	// interim polls instead of starving.
	maxPush = 120 * time.Second
	// mergeWindow: two pending polls landing this close read the same
	// cached value, so they collapse into the later one.
	mergeWindow = 10 * time.Second
)

// Action is one instruction to the UI: either arm a tick or poll now.
// A zero ArmTickAt with PollNow=false is a no-op (never emitted).
type Action struct {
	// PollNow, when true, means fetch the credits balance immediately.
	PollNow bool
	// ArmTickAt is the wall time at which the UI should schedule a tick
	// that calls OnTick(ID); zero when this Action is a PollNow.
	ArmTickAt time.Time
	// ID identifies the poll a tick belongs to; passed back to OnTick.
	ID int
}

// pendingPoll is a scheduled-but-unfired credits poll.
type pendingPoll struct {
	id          int
	firstTarget time.Time // original target, for the maxPush cap
	target      time.Time // current scheduled fire time
}

// CreditScheduler tracks pending polls. Not safe for concurrent use — it
// lives inside the single-threaded Bubble Tea update loop.
type CreditScheduler struct {
	nextID    int
	lastEvent time.Time
	polls     []*pendingPoll
	currentID int // id of the active burst's poll; 0 when no burst is active
}

// NewCreditScheduler returns an empty scheduler.
func NewCreditScheduler() *CreditScheduler { return &CreditScheduler{} }

// OnEvent records a broadcast event at now and returns any tick to arm.
// Same-burst events push the current poll's target forward (capped by
// maxPush); a new burst schedules an independent poll.
func (s *CreditScheduler) OnEvent(now time.Time) []Action {
	sameBurst := s.currentID != 0 && now.Sub(s.lastEvent) < burstGap
	s.lastEvent = now

	if sameBurst {
		if p := s.find(s.currentID); p != nil {
			target := now.Add(creditDelay)
			if capAt := p.firstTarget.Add(maxPush); target.After(capAt) {
				target = capAt
			}
			if !target.After(p.target) {
				// Already capped — the existing tick still stands.
				return nil
			}
			s.nextID++
			p.id = s.nextID
			p.target = target
			s.currentID = p.id
			return s.armSurviving(p)
		}
		// The active poll already fired; fall through to a new burst.
	}

	s.nextID++
	p := &pendingPoll{id: s.nextID, firstTarget: now.Add(creditDelay), target: now.Add(creditDelay)}
	s.polls = append(s.polls, p)
	s.currentID = p.id
	return s.armSurviving(p)
}

// OnTick handles a fired tick. It polls only if id is still the current
// id for a pending poll — otherwise the tick was superseded by a push or
// a merge and is ignored.
func (s *CreditScheduler) OnTick(id int, now time.Time) []Action {
	p := s.find(id)
	if p == nil {
		return nil
	}
	s.remove(id)
	if s.currentID == id {
		s.currentID = 0
	}
	return []Action{{PollNow: true}}
}

// OnManualRefresh polls immediately (the `r` key / launch). Pending
// burst polls are left in place; they harmlessly fold into the next
// debounce and the heartbeat reset that follows a successful fetch.
func (s *CreditScheduler) OnManualRefresh(now time.Time) []Action {
	return []Action{{PollNow: true}}
}

// armSurviving collapses near-coincident polls, then returns a tick for p
// if it survived the merge (a merged-away poll needs no tick — the poll it
// merged into already has one).
func (s *CreditScheduler) armSurviving(p *pendingPoll) []Action {
	s.mergeClose()
	if s.find(p.id) == nil {
		return nil
	}
	return []Action{{ArmTickAt: p.target, ID: p.id}}
}

// mergeClose collapses any two pending polls whose targets fall within
// mergeWindow into the later one (near-simultaneous polls read the same
// cached value — pure waste, tui/SPEC.md §7).
func (s *CreditScheduler) mergeClose() {
	if len(s.polls) < 2 {
		return
	}
	sort.Slice(s.polls, func(i, j int) bool { return s.polls[i].target.Before(s.polls[j].target) })
	kept := s.polls[:0:0]
	for _, p := range s.polls {
		if n := len(kept); n > 0 && p.target.Sub(kept[n-1].target) < mergeWindow {
			// p is the later of the pair — drop the earlier, keep p.
			if s.currentID == kept[n-1].id {
				s.currentID = p.id
			}
			kept[n-1] = p
			continue
		}
		kept = append(kept, p)
	}
	s.polls = kept
}

// find returns the pending poll with the given id, or nil.
func (s *CreditScheduler) find(id int) *pendingPoll {
	for _, p := range s.polls {
		if p.id == id {
			return p
		}
	}
	return nil
}

// remove drops the pending poll with the given id.
func (s *CreditScheduler) remove(id int) {
	for i, p := range s.polls {
		if p.id == id {
			s.polls = append(s.polls[:i], s.polls[i+1:]...)
			return
		}
	}
}

// pendingCount reports how many polls are scheduled — a test/inspection
// hook; the UI never needs it.
func (s *CreditScheduler) pendingCount() int { return len(s.polls) }
