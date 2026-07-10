// Bar-width animation (tui/SPEC.md §6): one Harmonica spring per model,
// animating a bar's *total* width toward its target in cell space — no
// separate springs. While a bar is in flight it brackets its motion with a
// uniform-color fade (fadePhase, stepFade) instead of animating its
// interior segment/highlight boundaries: those are only ever computed (via
// meter.go / core.SplitBar) once the bar is fully at rest, which is what
// keeps them from jumping or flickering mid-motion. A data change retargets
// the springs and, if any bar is unsettled, kicks the ~30 fps tick loop; the
// loop stops once every spring has settled *and* every fade has drained, so
// the app renders at 0 fps at rest (§6). Resize snaps (§4).

package ui

import (
	"log"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// Spring feel (tui/SPEC.md §6, "tune by feel"): critically damped so the
// collective scale-step zoom-out reads as one settling motion, never a
// ripple or a bounce.
const (
	springFreq    = 7.0
	springDamping = 1.0
	// animEpsilon is the cell distance (and speed) under which a spring is
	// treated as arrived: snap to target and stop ticking. Sub-half-cell,
	// so the rounded render has already stopped changing.
	animEpsilon = 0.15
)

// fadeRampFrames is how many animFPS frames the entering/exiting density
// ramp takes: 6 frames ≈ 200ms, two frames per glyph (░→▒→▓) since there
// are only three density glyphs — see renderFadingBar's step→glyph scaling
// in meter.go. Tune by feel, like springFreq/springDamping (tui/SPEC.md §6
// fade).
const fadeRampFrames = 6

// fadePhase is a bar's position in the uniform-color fade that brackets its
// length animation (tui/SPEC.md §6 fade): real segment/highlight colors are
// only ever rendered at fadeNone. Entering/exiting are the brief
// density-glyph ramps; held spans the whole remainder of the bar's motion.
type fadePhase int

const (
	fadeNone    fadePhase = iota // at rest: render real SplitBar geometry
	fadeEntering                 // ramping ░→▒→▓ in
	fadeHeld                     // solid fade color, for the rest of the motion
	fadeExiting                  // ramping ▓→▒→░ out, mirroring entry
)

// barAnim is one bar's animation state: its current width and velocity in
// (fractional) cells, plus its fade phase. Held by pointer in Model.anim so
// a spring step mutates in place across Bubble Tea's value-copied Model
// (like rows/sched).
type barAnim struct {
	pos float64
	vel float64

	// fade/fadeStep track the uniform-color fade (tui/SPEC.md §6 fade),
	// stepped alongside the spring in stepFade. fadeStep's meaning is
	// phase-local: the density-ramp frame index while entering/exiting,
	// unused while held/none.
	fade     fadePhase
	fadeStep int
}

// settledAt reports whether a is at rest on target within animEpsilon — the
// single definition of "arrived" every settle/fade decision uses.
func (a *barAnim) settledAt(target float64) bool {
	return math.Abs(a.pos-target) <= animEpsilon && math.Abs(a.vel) <= animEpsilon
}

// stepFade advances one bar's spring and its fade state machine exactly one
// frame (tui/SPEC.md §6 fade). The two are sequenced, not overlapped: a bar
// entering fade holds its position frozen through the whole ramp before the
// spring is allowed to move at all, fadeHeld is the only phase that steps
// the spring, and fadeExiting only starts once the spring has actually
// arrived (settledAt, not a fixed timer) — so every bar reads as fade in,
// then move, then fade out, never overlapping. autoExit controls whether a
// bar leaves fadeHeld for itself the instant it personally settles (the
// live/individual path) or waits for stepAnim to release the whole synced
// batch together (Model.animSync, set by withSyncedAnim for mode/timeframe/
// scale/refresh/rollover changes — tui/SPEC.md §6 fade). Retargeting
// mid-exit (a new data change arrives just as a bar was settling) pops
// straight back to held rather than re-running either ramp — no re-flash.
func (a *barAnim) stepFade(spring harmonica.Spring, target float64, autoExit bool) {
	switch a.fade {
	case fadeNone:
		if !a.settledAt(target) {
			a.fade, a.fadeStep = fadeEntering, 0
		}
	case fadeEntering:
		if a.fadeStep >= fadeRampFrames-1 {
			a.fade, a.fadeStep = fadeHeld, 0
		} else {
			a.fadeStep++
		}
	case fadeHeld:
		a.pos, a.vel = spring.Update(a.pos, a.vel, target)
		if a.settledAt(target) {
			a.pos, a.vel = target, 0
			if autoExit {
				a.fade, a.fadeStep = fadeExiting, 0
			}
		}
	case fadeExiting:
		switch {
		case !a.settledAt(target):
			a.fade, a.fadeStep = fadeHeld, 0
		case a.fadeStep >= fadeRampFrames-1:
			a.fade, a.fadeStep = fadeNone, 0
		default:
			a.fadeStep++
		}
	}
}

// newSpring builds the shared spring config (one config drives every bar).
func newSpring() harmonica.Spring {
	return harmonica.NewSpring(harmonica.FPS(animFPS), springFreq, springDamping)
}

// animTickCmd schedules the next animation frame.
func animTickCmd() tea.Cmd {
	return tea.Tick(time.Second/animFPS, func(time.Time) tea.Msg { return animTickMsg{} })
}

// barTargets returns each visible model's target bar width in cells, keyed
// by model name — the equilibrium the springs pull toward. Pure over the
// current snapshot, layout width, active mode, and shared scale.
func (m Model) barTargets() map[string]float64 {
	l := m.currentLayout()
	scale := m.scale()
	targets := make(map[string]float64, len(m.snap.Models))
	for _, st := range m.snap.Models {
		targets[st.Name] = float64(core.BarWidth(l.contentW, st.Value(m.mode), scale))
	}
	return targets
}

// reconcileAnim keeps the anim map in step with the current model set: new
// models get a spring starting at width 0 (they grow in, §6); models that
// left the window are dropped. Ghost shrink-to-0 rows are intentionally not
// modelled — see the Stage D note in the plan (discrete removal on leave).
func (m Model) reconcileAnim() Model {
	if m.anim == nil {
		m.anim = make(map[string]*barAnim)
	}
	present := make(map[string]bool, len(m.snap.Models))
	for _, st := range m.snap.Models {
		present[st.Name] = true
		if _, ok := m.anim[st.Name]; !ok {
			m.anim[st.Name] = &barAnim{} // grows from 0
		}
	}
	for name := range m.anim {
		if !present[name] {
			delete(m.anim, name)
		}
	}
	return m
}

// unsettled reports whether any bar is away from its target — the signal to
// (re)start the tick loop after a data change.
func (m Model) unsettled() bool {
	for name, target := range m.barTargets() {
		a := m.anim[name]
		if a == nil || !a.settledAt(target) {
			return true
		}
	}
	return false
}

// withAnim starts the animation tick loop if a data change left any bar
// unsettled and no loop is already running, batching the frame command in
// alongside cmd. This is the individual/live path (tui/SPEC.md §6): each
// bar enters, moves, and exits its fade on its own schedule, which is the
// point for real-time arrivals — two models rarely update in the same
// instant, and when they do, one bar animating quicker than another
// carries information about which request landed first. Handlers that
// change bar data from a single live upsert use this; resize does not (it
// snaps, §4). Batch data changes (mode/timeframe/scale/refresh/rollover/
// full re-fetch) use withSyncedAnim instead.
func (m Model) withAnim(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if !m.animating && m.unsettled() {
		m.animating = true
		log.Printf("anim: loop start (%d bars)", len(m.anim))
		return m, tea.Batch(cmd, animTickCmd())
	}
	return m, cmd
}

// withSyncedAnim is withAnim's counterpart for a batch data change
// (tui/SPEC.md §6 fade): mode, timeframe, scale, a manual refresh, a
// midnight rollover, and any full baseline/today-slice re-fetch all re-cut
// every visible bar's target in one Update() call, and should read as one
// synchronized motion, not N bars independently popping in and out of fade
// whenever each happens to finish. It force-starts the entering ramp on
// every currently-tracked bar — including ones whose target isn't moving
// at all, so a stationary bar still gains the fade color, for a uniform
// look across the whole row of bars — and sets Model.animSync so stepAnim
// holds every bar at fadeHeld until the slowest one settles, then exits
// them all together. handleLive's LiveRow case is the one bar-data path
// that deliberately keeps using withAnim instead.
func (m Model) withSyncedAnim(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.animSync = true
	for _, a := range m.anim {
		switch a.fade {
		case fadeNone:
			a.fade, a.fadeStep = fadeEntering, 0
		case fadeExiting:
			// A new batch landed mid-exit from the last one: pop back to
			// held rather than re-running the entering ramp — the same
			// rule stepFade already applies to a single bar's own
			// retarget, extended to the whole group.
			a.fade, a.fadeStep = fadeHeld, 0
		}
	}
	if !m.animating {
		m.animating = true
		log.Printf("anim: loop start (%d bars, synced)", len(m.anim))
		return m, tea.Batch(cmd, animTickCmd())
	}
	return m, cmd
}

// stepAnim advances every spring and fade one frame toward its target and
// reports whether all bars have fully settled — both the spring at rest
// and the fade drained back to fadeNone, so the tick loop never stops mid
// dematerialize (tui/SPEC.md §6 fade). When Model.animSync is set (a batch
// change from withSyncedAnim), a bar that personally settles doesn't leave
// fadeHeld on its own (stepFade's autoExit is false for the whole batch) —
// it waits here until every bar has finished entering and every held bar
// is settled, then all of them flip to fadeExiting on the same frame, so
// the group fades out as one motion instead of trickling out one bar at a
// time.
func (m Model) stepAnim() (Model, bool) {
	targets := m.barTargets()
	allSettled := true
	anyEntering, anyHeld, allHeldSettled := false, false, true
	for name, a := range m.anim {
		target, ok := targets[name]
		if !ok {
			continue // model left mid-flight; reconcile will drop it
		}
		a.stepFade(m.spring, target, !m.animSync)
		if a.fade != fadeNone || !a.settledAt(target) {
			allSettled = false
		}
		switch a.fade {
		case fadeEntering:
			anyEntering = true
		case fadeHeld:
			anyHeld = true
			if !a.settledAt(target) {
				allHeldSettled = false
			}
		}
	}

	if m.animSync {
		switch {
		case anyHeld && !anyEntering && allHeldSettled:
			for _, a := range m.anim {
				if a.fade == fadeHeld {
					a.fade, a.fadeStep = fadeExiting, 0
				}
			}
		case !anyEntering && !anyHeld:
			m.animSync = false // batch fully drained; the next trigger starts fresh
		}
	}

	return m, allSettled
}

// snapBars parks every bar on its target instantly and clears any in-flight
// fade — resize must feel like the terminal is in charge, never animated,
// never faded (tui/SPEC.md §4). It also drops any in-flight synced batch,
// same reasoning: a resize pre-empts everything.
func (m Model) snapBars() Model {
	for name, target := range m.barTargets() {
		if a := m.anim[name]; a != nil {
			a.pos, a.vel = target, 0
			a.fade, a.fadeStep = fadeNone, 0
		}
	}
	m.animSync = false
	return m
}

// barDisplayEighths is the width to draw for a model this frame, split into
// the whole-cell count to render solid and an eighths remainder [0,7] for
// the fractional leading-tip glyph (tui/SPEC.md §6): the spring animates a
// fractional cell position, but naive rounding quantizes the motion away —
// 8x finer horizontal resolution keeps it visible. Uses the animated
// position (clamped to the content width) when a spring exists, else the
// steady-state target — in which case pos is always a whole number, so
// eighths is always 0 (the tip only appears mid-animation). contentW is the
// hard ceiling — a shrinking bar sits transiently above its target, so
// target must NOT be the clamp bound.
func (m Model) barDisplayEighths(name string, target, contentW int) (whole, eighths int) {
	a := m.anim[name]
	pos := float64(target)
	if a != nil {
		pos = a.pos
	}
	if pos < 0 {
		pos = 0
	}
	// The critically-damped spring never overshoots, but guard so a retuned,
	// under-damped spring can't paint past the bar's cell budget.
	if contentW > 0 && pos > float64(contentW) {
		pos = float64(contentW)
	}
	whole = int(math.Floor(pos))
	eighths = int(math.Round((pos - float64(whole)) * 8))
	if eighths >= 8 { // rounding carried into the next whole cell
		whole++
		eighths = 0
	}
	return whole, eighths
}
