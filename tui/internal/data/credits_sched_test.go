package data

import (
	"sort"
	"testing"
	"time"
)

// t0 is an arbitrary epoch; tests speak in seconds from it.
var t0 = time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

// armed captures a tick the scheduler asked us to schedule.
type armed struct {
	id     int
	target time.Time
}

// collectArm records the ArmTickAt actions from a scheduler call.
func collectArm(dst *[]armed, actions []Action) {
	for _, a := range actions {
		if !a.ArmTickAt.IsZero() {
			*dst = append(*dst, armed{id: a.id(), target: a.ArmTickAt})
		}
	}
}

// id exposes the unexported Action.ID for the test's readability.
func (a Action) id() int { return a.ID }

// firePolls simulates every armed tick firing at its target (in time
// order, as tea.Tick would), returning the seconds-from-t0 of the ticks
// that actually resulted in a poll. Superseded ticks fire and are ignored.
func firePolls(s *CreditScheduler, ticks []armed) []int {
	sort.Slice(ticks, func(i, j int) bool { return ticks[i].target.Before(ticks[j].target) })
	var polls []int
	for _, tk := range ticks {
		for _, a := range s.OnTick(tk.id, tk.target) {
			if a.PollNow {
				polls = append(polls, int(tk.target.Sub(t0).Seconds()))
			}
		}
	}
	return polls
}

// TestScheduler_WorkedExample is the §7 A/B/C case: events at t=0, 5, 20.
// The t=5 event (same burst) pushes the first timer to 75; the t=20 event
// (15s gap → new burst) schedules an independent poll at 90. Two polls.
func TestScheduler_WorkedExample(t *testing.T) {
	s := NewCreditScheduler()
	var ticks []armed
	collectArm(&ticks, s.OnEvent(at(0)))
	collectArm(&ticks, s.OnEvent(at(5)))
	collectArm(&ticks, s.OnEvent(at(20)))

	polls := firePolls(s, ticks)
	want := []int{75, 90}
	if !equalInts(polls, want) {
		t.Errorf("polls at %v, want %v", polls, want)
	}
}

// TestScheduler_SingleEvent: one event polls once, 70s later.
func TestScheduler_SingleEvent(t *testing.T) {
	s := NewCreditScheduler()
	var ticks []armed
	collectArm(&ticks, s.OnEvent(at(0)))
	if polls := firePolls(s, ticks); !equalInts(polls, []int{70}) {
		t.Errorf("polls at %v, want [70]", polls)
	}
}

// TestScheduler_MaxPushGuard: a long rapid burst (5s gaps throughout)
// must still fire an interim poll instead of starving — the pending
// poll's target is capped at firstTarget+120 = 190.
func TestScheduler_MaxPushGuard(t *testing.T) {
	s := NewCreditScheduler()
	var ticks []armed
	for sec := 0; sec <= 130; sec += 5 {
		collectArm(&ticks, s.OnEvent(at(sec)))
	}
	polls := firePolls(s, ticks)
	if !equalInts(polls, []int{190}) {
		t.Errorf("polls at %v, want [190] (capped interim poll)", polls)
	}
}

// TestScheduler_StaleTickIgnored: after a push, the original tick fires
// but must not poll (its id was superseded).
func TestScheduler_StaleTickIgnored(t *testing.T) {
	s := NewCreditScheduler()
	var ticks []armed
	collectArm(&ticks, s.OnEvent(at(0))) // arm id for target 70
	collectArm(&ticks, s.OnEvent(at(5))) // push → new id for target 75

	// The first-armed tick (target 70) is stale.
	if len(ticks) != 2 {
		t.Fatalf("expected 2 armed ticks, got %d", len(ticks))
	}
	stale := ticks[0]
	if got := s.OnTick(stale.id, stale.target); len(got) != 0 {
		t.Errorf("stale tick polled: %+v", got)
	}
}

// TestScheduler_ManualRefresh polls immediately.
func TestScheduler_ManualRefresh(t *testing.T) {
	s := NewCreditScheduler()
	got := s.OnManualRefresh(at(0))
	if len(got) != 1 || !got[0].PollNow {
		t.Errorf("OnManualRefresh = %+v, want a single PollNow", got)
	}
}

// TestScheduler_MergeClose is a white-box check of the near-coincident
// merge guard: two pending polls within mergeWindow collapse into the
// later, and currentID follows the survivor.
func TestScheduler_MergeClose(t *testing.T) {
	s := NewCreditScheduler()
	early := &pendingPoll{id: 1, firstTarget: at(70), target: at(70)}
	late := &pendingPoll{id: 2, firstTarget: at(75), target: at(75)} // 5s apart < 10
	s.polls = []*pendingPoll{early, late}
	s.currentID = 1 // the earlier poll is "current"

	s.mergeClose()

	if s.pendingCount() != 1 {
		t.Fatalf("pendingCount = %d, want 1 after merge", s.pendingCount())
	}
	if s.polls[0].id != 2 {
		t.Errorf("survivor id = %d, want 2 (the later)", s.polls[0].id)
	}
	if s.currentID != 2 {
		t.Errorf("currentID = %d, want 2 (follows survivor)", s.currentID)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
