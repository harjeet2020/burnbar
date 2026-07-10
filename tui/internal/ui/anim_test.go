// Pure-function tests for the fade state machine (tui/SPEC.md §6 fade):
// stepFade's phase transitions are deterministic given fixed pos/vel/target
// inputs and the shared spring config, so no clock mocking is needed —
// same convention as bars_test.go/layout_test.go.

package ui

import (
	"testing"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

func TestStepFadeEntersOnFirstUnsettledFrame(t *testing.T) {
	spring := newSpring()
	a := &barAnim{} // fresh bar: pos=0, fade=fadeNone
	a.stepFade(spring, 10, true)
	if a.fade != fadeEntering || a.fadeStep != 0 {
		t.Fatalf("fade = %v, fadeStep = %d after first unsettled tick, want fadeEntering/0", a.fade, a.fadeStep)
	}
}

func TestStepFadeEnteringFreezesPosition(t *testing.T) {
	spring := newSpring()
	a := &barAnim{pos: 0, vel: 0, fade: fadeEntering, fadeStep: 0}
	for i := 0; i < fadeRampFrames-1; i++ {
		a.stepFade(spring, 10, true)
		if a.fade != fadeEntering {
			t.Fatalf("tick %d: fade = %v, want fadeEntering for the whole ramp", i, a.fade)
		}
		if a.pos != 0 || a.vel != 0 {
			t.Fatalf("tick %d: pos/vel = %v/%v, want 0/0 — the spring must not move until the entering ramp finishes", i, a.pos, a.vel)
		}
	}
}

func TestStepFadeRampCounts(t *testing.T) {
	spring := newSpring()
	a := &barAnim{fade: fadeEntering, fadeStep: 0}
	for want := 1; want < fadeRampFrames; want++ {
		a.stepFade(spring, 10, true)
		if a.fade != fadeEntering || a.fadeStep != want {
			t.Fatalf("fade = %v, fadeStep = %d, want fadeEntering/%d", a.fade, a.fadeStep, want)
		}
	}
	a.stepFade(spring, 10, true)
	if a.fade != fadeHeld || a.fadeStep != 0 {
		t.Fatalf("fade = %v, fadeStep = %d, want fadeHeld/0 after the entering ramp completes", a.fade, a.fadeStep)
	}
}

func TestStepFadeHeldStartsMovingTowardTarget(t *testing.T) {
	spring := newSpring()
	a := &barAnim{pos: 0, vel: 0, fade: fadeHeld, fadeStep: 0}
	a.stepFade(spring, 10, true)
	if a.pos == 0 {
		t.Fatalf("pos = %v after a held tick, want the spring to have started moving toward the target", a.pos)
	}
}

func TestStepFadeExitsOnlyOnceSettled(t *testing.T) {
	spring := newSpring()
	a := &barAnim{fade: fadeHeld}
	target := 10.0
	for i := 0; i < 1000; i++ {
		a.stepFade(spring, target, true)
		if a.fade == fadeExiting {
			if !a.settledAt(target) {
				t.Fatalf("tick %d: entered fadeExiting while not settledAt(target)", i)
			}
			if i == 0 {
				t.Fatalf("settled on the very first tick from 10 cells away — spring/epsilon assumption invalid for this test")
			}
			return
		}
		if a.fade != fadeHeld {
			t.Fatalf("tick %d: fade = %v, want fadeHeld until the bar actually settles", i, a.fade)
		}
	}
	t.Fatalf("spring never reached fadeExiting within 1000 ticks")
}

// TestStepFadeHeldSettledWaitsWhenNotAutoExit is the individual-vs-synced
// fork: with autoExit=false (a bar inside a withSyncedAnim batch), a bar
// that reaches its target on its own must stay in fadeHeld — only
// stepAnim's group check is allowed to move it to fadeExiting.
func TestStepFadeHeldSettledWaitsWhenNotAutoExit(t *testing.T) {
	spring := newSpring()
	target := 10.0
	a := &barAnim{pos: target, vel: 0, fade: fadeHeld}
	a.stepFade(spring, target, false)
	if a.fade != fadeHeld {
		t.Fatalf("fade = %v, want fadeHeld to persist past settling when autoExit is false", a.fade)
	}
}

func TestStepFadeExitRampToNone(t *testing.T) {
	spring := newSpring()
	target := 10.0
	a := &barAnim{pos: target, vel: 0, fade: fadeExiting, fadeStep: 0}
	for want := 1; want < fadeRampFrames; want++ {
		a.stepFade(spring, target, true)
		if a.fade != fadeExiting || a.fadeStep != want {
			t.Fatalf("fade = %v, fadeStep = %d, want fadeExiting/%d", a.fade, a.fadeStep, want)
		}
	}
	a.stepFade(spring, target, true)
	if a.fade != fadeNone || a.fadeStep != 0 {
		t.Fatalf("fade = %v, fadeStep = %d, want fadeNone/0 after the exiting ramp completes", a.fade, a.fadeStep)
	}
}

func TestStepFadeRetargetMidExit(t *testing.T) {
	spring := newSpring()
	a := &barAnim{pos: 10, vel: 0, fade: fadeExiting, fadeStep: 1}
	a.stepFade(spring, 40, true) // a new, far target lands mid-exit
	if a.fade != fadeHeld || a.fadeStep != 0 {
		t.Fatalf("fade = %v, fadeStep = %d, want fadeHeld/0 (a retarget mid-exit must not resume fadeEntering or continue the exit countdown)", a.fade, a.fadeStep)
	}
}

// TestStepAnimAllSettledWaitsForFade is the regression test for the tick
// loop: it must not stop while a bar's spring has arrived but its fade is
// still draining, or the bar would freeze mid-dematerialize.
func TestStepAnimAllSettledWaitsForFade(t *testing.T) {
	m := Model{
		width:  100,
		height: 30,
		mode:   core.ModeTokens,
		spring: newSpring(),
		snap: core.Snapshot{
			Models: []core.ModelStat{{Name: "m1", InputTokens: 500, OutputTokens: 500}},
		},
	}
	target := m.barTargets()["m1"]
	m.anim = map[string]*barAnim{
		"m1": {pos: target, vel: 0, fade: fadeExiting, fadeStep: 0},
	}

	for i := 0; i < fadeRampFrames-1; i++ {
		var settled bool
		m, settled = m.stepAnim()
		if settled {
			t.Fatalf("tick %d: stepAnim reported allSettled while fade still draining (fade=%v)", i, m.anim["m1"].fade)
		}
	}
	_, settled := m.stepAnim()
	if !settled {
		t.Fatalf("stepAnim never reported allSettled once the fade fully drained (fade=%v)", m.anim["m1"].fade)
	}
	if m.anim["m1"].fade != fadeNone {
		t.Fatalf("fade = %v after settling, want fadeNone", m.anim["m1"].fade)
	}
}

func TestSnapBarsClearsFade(t *testing.T) {
	m := Model{
		width:  100,
		height: 30,
		mode:   core.ModeTokens,
		snap: core.Snapshot{
			Models: []core.ModelStat{{Name: "m1", InputTokens: 500, OutputTokens: 500}},
		},
		anim: map[string]*barAnim{
			"m1": {pos: 3, vel: 2, fade: fadeEntering, fadeStep: 1},
		},
		animSync: true,
	}
	target := m.barTargets()["m1"]
	m = m.snapBars()
	a := m.anim["m1"]
	if a.pos != target || a.vel != 0 {
		t.Fatalf("pos/vel = %v/%v after snapBars, want %v/0", a.pos, a.vel, target)
	}
	if a.fade != fadeNone || a.fadeStep != 0 {
		t.Fatalf("fade = %v, fadeStep = %d after snapBars, want fadeNone/0", a.fade, a.fadeStep)
	}
	if m.animSync {
		t.Fatalf("animSync = true after snapBars, want false — a resize pre-empts any in-flight synced batch")
	}
}

// --- withSyncedAnim / group-fade tests (tui/SPEC.md §6 fade) ---------------

// syncTestModel builds a Model with two tracked bars: "mover" is off-target
// (will spring toward it) and "still" is already on-target (won't move at
// all), mirroring a mode/timeframe/scale change where only some bars
// actually resize.
func syncTestModel() Model {
	return Model{
		width:  100,
		height: 30,
		mode:   core.ModeTokens,
		spring: newSpring(),
		snap: core.Snapshot{
			Models: []core.ModelStat{
				{Name: "mover", InputTokens: 5000, OutputTokens: 5000},
				{Name: "still", InputTokens: 10, OutputTokens: 10},
			},
		},
		anim: map[string]*barAnim{
			"mover": {pos: 0, vel: 0, fade: fadeNone},
			"still": {},
		},
	}
}

// TestWithSyncedAnimFadesStationaryBar covers the "uniform UX" ask: a bar
// whose target isn't moving at all must still gain the entering fade when a
// synced batch starts, not just the bars that are actually resizing.
func TestWithSyncedAnimFadesStationaryBar(t *testing.T) {
	m := syncTestModel()
	target := m.barTargets()["still"]
	m.anim["still"].pos = target // "still" starts already on-target

	tm, _ := m.withSyncedAnim(nil)
	m = tm.(Model)
	if m.anim["still"].fade != fadeEntering {
		t.Fatalf("still bar fade = %v after withSyncedAnim, want fadeEntering even though its target didn't move", m.anim["still"].fade)
	}
	if !m.animSync {
		t.Fatalf("animSync = false after withSyncedAnim, want true")
	}
}

// TestSyncGroupWaitsForSlowestThenExitsTogether is the core regression test
// for the chaotic-fade fix: in a synced batch, a bar that personally
// settles early must hold at fadeHeld until every bar has settled, then all
// of them must flip to fadeExiting on the very same tick.
func TestSyncGroupWaitsForSlowestThenExitsTogether(t *testing.T) {
	m := syncTestModel()
	m.anim["still"].pos = m.barTargets()["still"] // already on-target
	tm, _ := m.withSyncedAnim(nil)
	m = tm.(Model)

	// Drain the entering ramp for both bars (forced together, so they
	// finish it on the same tick).
	for i := 0; i < fadeRampFrames; i++ {
		m, _ = m.stepAnim()
	}
	if m.anim["mover"].fade != fadeHeld || m.anim["still"].fade != fadeHeld {
		t.Fatalf("fade = mover:%v still:%v after the entering ramp, want both fadeHeld", m.anim["mover"].fade, m.anim["still"].fade)
	}

	// Step until the stationary bar's own state settles; the mover is still
	// far from its target, so the group must not exit yet.
	for i := 0; i < 5; i++ {
		m, _ = m.stepAnim()
		if m.anim["still"].fade != fadeHeld {
			t.Fatalf("tick %d: still bar fade = %v, want it held while mover is still in flight", i, m.anim["still"].fade)
		}
	}

	// Run the mover to completion; the instant it settles, both bars must
	// exit together on the same tick.
	target := m.barTargets()["mover"]
	for i := 0; i < 1000; i++ {
		m, _ = m.stepAnim()
		if m.anim["mover"].fade == fadeExiting {
			if m.anim["still"].fade != fadeExiting {
				t.Fatalf("mover entered fadeExiting but still bar fade = %v, want fadeExiting on the same tick", m.anim["still"].fade)
			}
			return
		}
		if !m.anim["mover"].settledAt(target) && m.anim["mover"].fade != fadeHeld {
			t.Fatalf("tick %d: mover fade = %v while still unsettled, want fadeHeld", i, m.anim["mover"].fade)
		}
	}
	t.Fatalf("mover never reached fadeExiting within 1000 ticks")
}

// TestLiveRowStaysIndividualDuringSyncBatch documents the accepted overlap
// case: withAnim (the live path) never forces entering on stationary bars
// and never sets animSync, so a plain live retarget outside a synced batch
// still exits on its own the moment it settles.
func TestLiveRowStaysIndividualDuringSyncBatch(t *testing.T) {
	m := syncTestModel()
	tm, _ := m.withAnim(nil)
	m = tm.(Model)
	if m.animSync {
		t.Fatalf("animSync = true after withAnim, want false — withAnim must never engage the synced batch")
	}
	if m.anim["still"].fade != fadeNone {
		t.Fatalf("still bar fade = %v after withAnim, want fadeNone — withAnim must not force-start bars whose target didn't move", m.anim["still"].fade)
	}
}
