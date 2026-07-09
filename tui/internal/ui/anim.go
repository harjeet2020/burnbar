// Bar-width animation (tui/SPEC.md §6): one Harmonica spring per model,
// animating a bar's *total* width toward its target in cell space. Segment
// and accent boundaries are recomputed as fractions of the animated width
// each frame (see meter.go / core.SplitBar) — no separate springs. A data
// change retargets the springs and, if any is unsettled, kicks the ~30 fps
// tick loop; the loop stops the instant every spring settles, so the app
// renders at 0 fps at rest (§6). Resize snaps (§4).

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

// barAnim is one bar's animation state: its current width and velocity in
// (fractional) cells. Held by pointer in Model.anim so a spring step
// mutates in place across Bubble Tea's value-copied Model (like rows/sched).
type barAnim struct {
	pos float64
	vel float64
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
		if a == nil || math.Abs(a.pos-target) > animEpsilon || math.Abs(a.vel) > animEpsilon {
			return true
		}
	}
	return false
}

// withAnim starts the animation tick loop if a data change left any bar
// unsettled and no loop is already running, batching the frame command in
// alongside cmd. Handlers that change bar data wrap their result in this;
// resize does not (it snaps, §4).
func (m Model) withAnim(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if !m.animating && m.unsettled() {
		m.animating = true
		log.Printf("anim: loop start (%d bars)", len(m.anim))
		return m, tea.Batch(cmd, animTickCmd())
	}
	return m, cmd
}

// stepAnim advances every spring one frame toward its target and reports
// whether all bars have settled. Settled springs are snapped exactly onto
// target so the render stops twitching and equality checks are clean.
func (m Model) stepAnim() (Model, bool) {
	targets := m.barTargets()
	allSettled := true
	for name, a := range m.anim {
		target, ok := targets[name]
		if !ok {
			continue // model left mid-flight; reconcile will drop it
		}
		a.pos, a.vel = m.spring.Update(a.pos, a.vel, target)
		if math.Abs(a.pos-target) <= animEpsilon && math.Abs(a.vel) <= animEpsilon {
			a.pos, a.vel = target, 0
		} else {
			allSettled = false
		}
	}
	return m, allSettled
}

// snapBars parks every bar on its target instantly — resize must feel like
// the terminal is in charge, never animated (tui/SPEC.md §4).
func (m Model) snapBars() Model {
	for name, target := range m.barTargets() {
		if a := m.anim[name]; a != nil {
			a.pos, a.vel = target, 0
		}
	}
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
