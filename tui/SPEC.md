# Burnbar TUI — Design & Implementation Spec

> **How to use this file (for AI agents):** This is the runbook for the
> Go TUI. Read it together with the root `SPEC.md` (§2 Frontend Data
> Contract is binding here) and the README UI/UX section. Work through
> the stages in §10 in order; check off items as you go. All design
> decisions were agreed with the user (§11 tracks the still-open ones)
> — if implementation reveals a decision to be wrong, stop and discuss
> before deviating.
>
> **Maintenance rule — keep this file shrinking, not growing.** When a
> stage or feature ships: collapse its spec section down to a short
> prose summary plus a pointer to the file(s) that implement it, and
> shrink its §10 entry to Goal / Done-when / key files (drop the
> checklist and any worked examples — code, tests, and git history are
> the record of *how*, from here on). Sections describing work that
> hasn't shipped yet should stay verbose: goal, rationale, technical
> approach — an implementer should be able to work from them without
> re-deriving decisions already made. The file should read
> mostly-brief for what's done and mostly-detailed for what's pending,
> at all times — never let a shipped section re-accumulate detail.

## 1. Product Shape

A long-running, full-screen terminal app (`burnbar`) on the alternate
screen. One primary screen (the meter) and one drill-down (model
details). It sits in a corner of a terminal all day, so the design
priorities are: calm at rest (no flicker, no pointless motion), alive
on events (a request landing is *seen*), and honest about its own state
(connection, staleness, lag are always visible).

**Stack:** Bubble Tea **v2** (`charm.land/bubbletea/v2` — `View()`
returns `tea.View`, alt-screen declared on the view, key handling via
`tea.KeyPressMsg`), Lip Gloss v2, Bubbles (help/key/viewport), Harmonica
for springs. Verify current v2 APIs at build time rather than trusting
training data — the v1→v2 surface changed materially.

## 2. Screen Anatomy

**Implemented** (Stage A + original Stage D — `internal/ui/layout.go`,
`meter.go`, `view.go`, `details.go`): four fixed regions, top to
bottom, that never move or reorder.

1. **Header (currently 2 rows):** wordmark left; right side shows the
   credits balance with age (`$12.43 · 2m`) and, on the second row, the
   `t`-cycled timeframe selector (today/week/month, active one reverse
   video) plus the active window's total.
2. **Bars list:** one block per model used in the window (label row +
   bar row), sorted by cost desc, scrolling inside a viewport with
   `▼ N more` / `▲ N more` when models overflow available height.
3. **Status row:** last-request time + relative age, meter lag, the
   current bar scale (muted), connection state — `● live` / `◌
   polling` / `○ reconnecting…` (with retry countdown) / `✗ offline`,
   symbols + words, never color alone.
4. **Hint row:** the core keymap, currently truncated by the help
   bubble's default `…` when width is tight (see the D.1 fix below).

Current keymap: `t` cycle timeframe · `j/k`/arrows select · `enter`/`l`
details · `esc`/`h` back · `r` refresh · `p` toggle live source · `?`
help · `q`/`ctrl+c` quit. Mouse is a pure augmentation (wheel scroll,
click-select, click-through to details, timeframe click) — every
action has a keyboard equivalent, never required.

**Details screen** (`internal/ui/details.go`) is a full-screen
drill-down replacing the bars region for the selected model over the
active window (header/status/hints persist, hints swap to `esc back ·
t window · r refresh · q quit`): totals (requests, tokens, cost
w/split), cache hit %, reasoning share, effective $/1M rates, avg
duration, and a `lipgloss/table` provider-split table sorted by cost
desc. `—` for NULL-derived values, never 0. Stays live; selection
follows the *model* across re-sorts.

---

**Implemented (Stage D.1, revised — the original pass left a bug: the
list's top gap was reserved in the row budget but never actually
rendered, so it silently became bottom padding and the whitespace
jittered as the window resized).** The screen is seven fixed rows, top
to bottom, that never move, reorder, or disappear: 3 header rows
(wordmark/credits, a blank spacer, timeframe/spend), a spacer, the bars
list, a spacer, the status row, the hint row. Only the bars list's
*content* — how many model blocks it shows — responds to window size.
A block of k visible models always occupies `topArrowRow + k×(name, bar)
+ (k−1) inter-block spacers + bottomArrowRow` = `3k+1` rows; the arrow
rows render blank when there's nothing to scroll in that direction, "N
more" text when there is. The status row is never merged into the hint
row, at any height. The hint row collapses by **priority** in a fixed
display order rather than the help bubble's blunt `…` truncation, which
could hide *everything* on a narrow terminal: `j/k select · enter
details · r refresh · t window · m mode · p source · ? help · q quit` is
the full display order; `j/k select`, `r refresh`, `? help`, `q quit` are
the protected core that always renders, and `enter details`, `p source`,
`m mode`, `t window` drop in that order (details first, window last) as
width tightens — each entry's removability is independent of its
position in the display order. `?` always opens the full,
un-collapsed overlay regardless of width. Select hint label reads `j/k`
(arrows stay bound, undocumented).

**Implemented and verified (Stage D.2 mode indicator, Stage D.3 manual
zoom).** Now that
dual cost/token modes have shipped (§3), the header's active-window total and
the status-row scale chip both need to read the *active mode's* unit —
`spent $1.2345` / `used 1.5M` in the header, `scale $5` / `scale 2M` in
the status row (the scale chip doubles as the mode indicator, no extra
chrome needed). The scale chip gains a `·manual` suffix while a manual
zoom is pinned.

**Implemented, chrome/empty-state verified — recent-request modal (Stage
D.4).** A centered overlay
(same chrome as the `?` help overlay — the app's one transient border)
answering "what did the last thing that just happened actually cost?"
— the question a live meter raises but the bars can't answer at a
glance. Bound to `i` (provisional). Shows the **most recent burst**
(§7 — same-model live requests coalesced within `burstGap`, so an
agent's rapid tool-call fan-out reads as one logical action): model (+
routed provider slug if reported), request count if >1 + the time span
covered, input/output tokens (cached/reasoning too, `—` for NULL),
total cost with input/output split, wall time + age of the burst's
latest request, and meter lag. **Live** (a new arrival updates it in
place) and **display-only** — it reads the same coalesced object that
drives the §5 highlight and never feeds the authoritative window
totals. Friendly `no requests seen yet` line before any live request
this session. `i` or `esc` closes it. The overlay chrome, open/close
bindings, and empty state are verified against the running app; the
populated-burst content and live-update-in-place have not yet been
exercised against a real request (no live traffic landed during
verification) — the render code is untested past what unit tests on its
inputs (`core.Burst`, `core.Format*`) and the identical `?`-overlay
pattern already cover.

## 3. The Bars — Stage D.2 and D.3 implemented and verified

The old build (`internal/ui/meter.go`, `core.SplitBar`) rendered a
single hybrid bar: length ∝ tokens, interior split by cost, one
rolling accent1/accent2 highlight — a cell in it meant neither a fixed
token count nor a fixed dollar amount, just a product of the two, with
no honest place to draw "this burst added Δinput." Everything below is
now implemented and verified against the running app with real
backend data.

**Two display modes — cost and tokens — toggled by `m`.** The app
never shows volume and money in the *same* bar. Instead each mode is
**single-denomination end to end** — length, interior split, sort
order, and the §5 highlight all speak one language — so `m` re-renders
between two internally-coherent views rather than blending them:

- **Cost mode** (default — burnbar is a money meter): length ∝ window
  cost; split = input-cost vs output-cost; highlight = latest burst's
  input/output cost. Answers "how fast is my money going, and on
  what."
- **Token mode:** length ∝ total tokens; split = input vs output token
  volume; highlight = burst's input/output tokens. Answers "how much
  am I actually using."

**Sort follows the mode** (cost desc / total-tokens desc, ties: name
asc) so the longest bar always sits on top in either mode. Toggling `m`
may re-sort; selection follows the *model*, not the row index. The
label row (below) always shows the *other* denomination's numbers too,
so no information is hidden by the mode choice.

**Auto-ranging scale (no bar ever pinned at 100%).** All bars share one
scale `S`, drawn from a **1–2–5 ladder in the active mode's unit** —
tokens (`10K, 20K, 50K, 100K…`, floor 10K) or dollars (`$0.001,
$0.002, $0.005…`, small floor tuned by feel). `S` is the smallest
ladder value such that `max_i(value_i) ≤ 0.8·S` — a pure function of
window + mode, no state, no hysteresis. `BarWidth = W · value_i / S`,
**minimum 1 cell** for any nonzero model (this is what keeps a
heavily-used **free/$0 model** visible in cost mode instead of
vanishing). The geometry is generic over `value` (cost or tokens), one
code path serves both modes. Effect: early usage fills the top bar;
crossing 80% steps the scale up and **all bars shrink together**
(springs-animated), so sustained usage reads as repeated "zoom outs"
— proportions between bars stay exact at all times because every bar
shares `S`.

**Implemented and verified — manual zoom (`-`/`+`/`0`),
the honest fix for the long-tail stub
problem.** A dominant model plus several tiny ones squashes the tail
into indistinguishable 1-cell stubs under the auto scale. Rather than
a *nonlinear* scale (rejected — breaks exact proportionality and fights
the animation, since a fixed Δ would move the bar by a size-dependent
amount), the user steps the same linear 1–2–5 ladder by hand:

- `-` zooms out (next larger `S`), `+`/`=` zooms in (next smaller `S`,
  small bars grow, real differences show, but bars exceeding `S` clamp
  to full width — several large bars pin near 100% and lose their
  mutual differences), `0` resets to auto. There is no single scale
  that distinguishes both ends of a long tail; manual zoom lets the
  user pick which end to inspect.
- Manual `S` is **transient view state**, not config: resets to auto on
  refresh (`r`), window switch (`t`), and mode toggle (`m`) — each
  re-baselines the view, and the auto scale differs per window/mode
  anyway. Auto-ranging does not re-engage while a manual `S` holds,
  even if new data overflows it (bars clamp instead).
- Zoom retargets every bar at once, reusing the §6 collective
  scale-step animation, so it reads as the same "everyone re-scales
  together" motion as an auto step.

The current scale is always shown muted in the status row in the
active mode's unit — doubling as the mode indicator — with a `·manual`
marker when pinned.

**Inside a bar — the split.** The boundary between the input segment
(`█`) and output segment (`▒`, same hue) sits at the input share of the
bar's own denomination: `input_cost / (input_cost + output_cost)` in
cost mode, the token equivalent in token mode. The cost split is
truthful in a way raw token proportions can't be, because output
tokens cost several times more and cache discounts shrink effective
input cost — the summed **actual split costs** (facts the schema
stores, not `unit_price × tokens`) already embody those discounts, so
cached-token handling is automatic. Cost-mode fallback: if split costs
are unreported or both zero (free models), fall back to the
token-volume split for the boundary (length stays honest regardless —
total cost is always known).

**Label row** (above each bar): model name left; then both
denominations, with the **active mode's metric** bold and
right-aligned as the row's anchor (mirroring the sort order) and the
other denomination muted beside it. Cost mode: `1.2M in · 340.2K out`
(muted) … `$0.4821` (bold anchor). Token mode: `$0.4821` (muted) …
`1.5M` (bold anchor).

## 4. Responsive Behavior

**Implemented** (`internal/ui/layout.go`): layout derives from the
cached `WindowSizeMsg` on every resize, no fixed dimensions. Resizing
snaps (no animation — springs are for data changes only, §6).
Breakpoint ladder by columns: **≥110** full labels + verbose in/out
token split; **80–109** model names full, but the label row's secondary
token value is a single aggregate count in cost mode (never the
misleading `840K→20K` in→out shorthand — a compromise view either shows
both numbers properly or neither); **60–79** model names middle-
truncated, the secondary denomination dropped from the label row;
**40–59** name + metric anchor + bar only; **below 40×14** a centered
`terminal too small (min 40×14)` message, never a crash. The seven
screen rows (§2) are fixed at every breakpoint and every height above
the floor — height pressure only changes how many model blocks the bars
list shows, never which rows exist.

## 5. Color, Accents & Theming

**Implemented — ANSI-16 tokens** (`internal/ui/styles.go`): the app
inherits the user's terminal color scheme, semantic tokens only, no
hardcoded hex. Standard block/box glyphs only (`█ ▒ ▸ ● ◌ ▼`, no Nerd
Font), every glyph has an ASCII fallback (`# = > * . v`). `NO_COLOR`
and dumb terminals stay fully readable (segments by glyph, selection by
`▸`+bold, status by symbol+word).

| Token | ANSI | Used for |
|---|---|---|
| `accent.primary` | cyan | wordmark, active timeframe, selection, input segment |
| `accent.primary.bright` | bright cyan | latest-burst **input** highlight |
| `bar.output` | blue | output segment |
| `bar.output.bright` | bright blue | latest-burst **output** highlight |
| `text.primary` | default fg | names, values |
| `text.muted` | bright black | token counts, timestamps, inactive labels, hints, scale |
| `status.ok`/`warn`/`error` | green/yellow/red | connection dot, error banner |

The table already reflects the *target* highlight design (below) —
the currently-live `AccentSession`/`AccentLatest` tokens in
`styles.go` are the old rolling-anchor scheme and go away with them.

**Implemented, pending manual verification — the latest-burst highlight
(Stage D.2).** Replaces the old rolling-anchor + accent1/accent2 +
keyboard-focus system entirely: no `accentWindow`, no
`accentClearDelay`, no xterm-1004 focus reporting, no tmux
`focus-events` handling. Keyboard focus was always a poor proxy for
"seen it" on a meter that lives visible-but-unfocused beside the
working terminal, and the two-accent system gave a bar 3–4 competing
hues for one request. The replacement: exactly
**one** highlight, the most recent burst (§7), drawn as a **brighter
shade of the segment it belongs to** rather than a separate colored
slice — the burst's input value brightens the trailing edge of the
input segment (`accent.primary.bright`), its output value brightens
the tail (`bar.output.bright`), reading in the active mode's unit
(cost or tokens, §3). Geometry: the bright region cell counts are
proportional in the active mode's unit (same shared-scale math as bar
length), clamped inside their own segment, with a **≥1-cell minimum
floor** (borrowed from the segment's base) so even a burst too small
to lengthen the bar by a whole cell is still visibly painted. On
arrival it gets a brief ~1 s **bold** pulse (§6) — the primary arrival
signal for sub-cell tiny bursts — then settles to the plain bright
shade and **persists** (no timed fade, no focus clearing) until a
newer burst supersedes it or a manual refresh clears it (refresh drops
live deltas, §7, so this is "re-baseline my view of the world").

**Pending — live theme picker (Stage E.1).** The fixed ANSI-16 mapping
above can collide in some terminal themes (two slots reading as
near-identical). A `theme`/`c` binding will open a picker letting the
user re-slot which of *their theme's* 16 colors each token uses,
previewed live against a real rendered bar, saved to
`config.toml`'s `[colors]` table (token → ANSI color name), with env
override precedence unchanged (§7). **Scope guard: ANSI-16 only, never
hex** — hex would let the app clash with the terminal theme instead of
inheriting it, the exact pitfall the ANSI-16 default exists to avoid;
truecolor theming is out of scope. Sequenced after the details screen
and the D.2 highlight (it previews the highlight system). Missing/
partial `[colors]` falls back to the default slot per-token;
`NO_COLOR`/ASCII parity must stay intact.

## 6. Motion

**Implemented** (`internal/ui/anim.go`): one Harmonica spring per bar,
animating bar width toward its target on any data change (new event,
scale step, window switch, refresh); segment/highlight boundaries are
computed as fractions of the animated width each frame. A scale step
retargets every spring at once (the collective zoom-out is the app's
most satisfying moment). New model grows from 0; a model leaving the
window shrinks to 0 then is removed (no vertical row animation,
reorders are discrete). **Idle means idle:** 0 fps at rest, a ~30 fps
tick loop runs only while any spring is unsettled, plus a slow 15 s
ticker for relative timestamps/credits age. No animation on resize
(snap, §4) or on the details screen (values just update).

**Implemented, pending manual verification — sub-cell smoothing via
fractional block glyphs (Stage D.2).** The spring animates a
fractional cell position but naive
rendering rounds to whole cells, so smooth motion gets quantized away
— worst at the end of travel where a critically-damped spring
decelerates. Fix is not more fps (the choppiness is spatial, not
temporal) but **8× finer horizontal resolution**: render the bar's
leading tip as a fractional block glyph (`▏▎▍▌▋▊▉█`, eighths) in the
tip's color, so the edge advances in eighth-cell steps (the standard
`pv`/`btop` technique). ASCII/`NO_COLOR` fallback has no fractional
glyphs and stays cell-quantized — an accepted degradation, not a bug.
The highlight arrival's ~1 s bold pulse (§5) is implemented here as one
timed message, not a fade (ANSI-16 has no alpha).

**Implemented — Interior fade (Stage D.5, 2026-07-10).** Interior seams
(the input/output split boundary, the two burst-highlight edges) turned
out to need smoothing too: recomputing them every frame against the
live, moving bar width made them jump by whole cells, and made the
highlight boundary specifically flicker (a cell rendered as the plain
growing tip could be retroactively reclassified bright the instant it
became whole). Rather than sub-cell-animating three separate interior
boundaries — which also runs into a wall the leading tip doesn't: the
input zone's glyph (`█`, solid) and the output zone's glyph (`▒`,
dithered) have no shared partial-glyph representation for a fractional
boundary between them — a bar's length animation instead brackets
itself in a uniform-color fade: the whole bar (solid cells + fractional
tip) renders as one flat ANSI-16 color with no split and no highlight
while in flight, and real `SplitBar` geometry is only ever computed once
the bar is at rest. The fade illusion itself is density-based (`░▒▓`,
ramping in/out around a solid hold), not a color gradient — ANSI-16 has
no knowable "halfway between yellow and blue" without assuming RGB
values the terminal might not use, so this works identically on a
16-color terminal and a truecolor one. See Stage D.5 (§10) for the
implementation.

**Implemented — Synced batch fade + fade-then-move ordering (Stage
D.6, 2026-07-10).** Two refinements to D.5's fade, both found by using
it: first, `stepFade` used to move the spring the same frame the
entering ramp began, so a bar's color and its length were changing
simultaneously; it now sequences the two strictly — a bar sits frozen
through the whole entering ramp, only starts moving once solid-held,
and only starts exiting once settled — so every bar always reads as
fade in, then move, then fade out. Second, a *batch* data change
(mode/timeframe/scale, a manual refresh, a midnight rollover, or any
full baseline/today-slice re-fetch — anything that re-cuts every
visible bar's target in one `Update()` call) used to let each bar fade
out independently the instant its own spring settled, which read as
chaotic when several bars resized by different amounts at once. These
triggers now go through `withSyncedAnim` instead of `withAnim`: every
tracked bar — including ones whose target isn't moving at all, so a
stationary bar still gains the fade color — enters together, and
`stepAnim` holds the whole group at `fadeHeld` until every bar has
settled, then exits them all on the same frame. A single live request
landing (`handleLive`'s `LiveRow` case) is the one path that
deliberately keeps using plain `withAnim`: real-time arrivals are rare
enough to overlap that one bar animating quicker than another still
carries information (which model's request landed first / hit
harder), so those stay independent and out of sync with each other, by
design. See Stage D.6 (§10) for the implementation.

## 7. Data & State Logic

**Implemented — state model** (`internal/core/rows.go`,
`aggregate.go`; `internal/data/*`): three stores plus derived view
state — `baseline` (`usage_daily` rows, full 31 days, tagged
`fetchedAt`, UTC-day grain), `rows` (raw `requests` rows, deduped by
`(trace_id, span_id)`, from the today-slice fetch and live events;
live rows carry `receivedAt` and drive the highlight/modal/meter lag).
`aggregate(baseline, rows, window, now) → []ModelStat` is the one pure
function all ratios flow through, zero-guarded everywhere (`—` on
divide-by-zero), computing per-model and per-(model, provider) grains
in one pass.

**Implemented — windows:** `today` = the user's **local** calendar day,
computed exclusively from `rows` (the raw slice can be re-cut to any
local midnight exactly; the UTC-day-grain baseline can't) — this
avoids a UTC "today" visibly resetting mid-session for most timezones.
`week`/`month` = the current **UTC** calendar week (Monday-Sunday) and
UTC calendar month, from baseline + live layered on top (brief overlap
double-count self-heals on refresh). UTC-anchored rather than the
user's local calendar so the boundary lines up with the baseline's
UTC-day grain (root SPEC §2/§6). Rollover timers re-aggregate at local
midnight (today) and UTC midnight (week/month) — every UTC midnight is
also a candidate calendar-week/month boundary, so the existing daily
tick needs no separate weekly/monthly schedule; local-midnight math
goes through the system timezone at each scheduling so DST is handled
for free. Switching timeframe (`t`) is pure client-side re-aggregation,
no refetch.

**Implemented — startup, realtime, poll:** load config (plain
actionable error before the alt screen on failure, exit non-zero) →
render immediately with loading placeholders → connect realtime and
**subscribe first**, fetch baseline + today-slice once confirmed →
credits fetched in parallel. Realtime is a Phoenix-channel WebSocket
via `github.com/nshafer/phx` (`internal/data/realtime.go`) carrying
`postgres_changes` INSERT on `public.requests`; **every successful
(re)join refetches baseline + today-slice and clears live rows**, so
missed events self-heal via re-baselining rather than gap-tracking.
(`supabase-community/realtime-go` was tried first and rejected — its
reconnect self-deadlocks and it logs to stderr, corrupting the alt
screen; full post-mortem in root-spec §6.) Poll is a **manual backup,
not a silent fallback** — both sources share one `LiveSource`
interface, `live_source` config sets the startup source, `p` toggles
realtime ↔ poll at runtime, no hidden auto-switch heuristic. Poll
queries PostgREST every 20 s (`◌ polling` status keeps the active
source honest on screen).

**Implemented — credits:** `GET /api/v1/credits`, balance =
`total_credits − total_usage`, shown as-is with its age (no local
decrement). Cadence: on launch/manual refresh; a **burst-grouped
debounce** after live events (events <10 s apart share a burst; the
burst's poll is scheduled at `lastEvent + 70 s`, since OpenRouter
caches credit values ~60 s; a pending poll is never pushed more than
~120 s past its first scheduling, and two pending polls within 10 s of
each other merge into the later one); every 5 min otherwise as an idle
heartbeat. Stale value + growing age shown on fetch failure; a 401
hints "check openrouter_api_key" (`internal/data/credits.go`,
`credits_sched.go`).

**Implemented — config:** `~/.config/burnbar/config.toml` (real,
user-managed, gitignored everywhere in the repo); `config.example.toml`
is the committed template. Env vars override file values:

| TOML key | Env override | Required |
|---|---|---|
| `supabase_url` | `BURNBAR_SUPABASE_URL` | yes |
| `supabase_anon_key` | `BURNBAR_SUPABASE_ANON_KEY` | yes |
| `openrouter_api_key` | `BURNBAR_OPENROUTER_API_KEY` | no — credits header shows `—` with a hint when absent |
| `live_source` | `BURNBAR_LIVE_SOURCE` | no — startup source (`poll`\|`realtime`); `p` toggles at runtime; default `realtime` |

An optional `[colors]` section (§5, Stage E.1) will be managed by the
theme picker rather than hand-edited normally.

---

**Implemented, pending manual verification — the latest burst (Stage
D.2; D.3/D.4 still build on top of it).** Agents fan a
single prompt into many rapid OpenRouter calls (tool loops), each
landing as its own `requests` row — a "single most recent request"
highlight would then show only the *last* tiny call of a twelve-call
burst, which is misleading for a live meter. A pure helper will
coalesce them **for display only**:

```
latestBurst(rows) → Burst   // nil when no live row has been seen
```

A **burst** is a run of live rows for the **same model** whose
arrivals are each within `burstGap` (~3 s, tune by feel) of the
previous one, walked back from the most recent live arrival
(`receivedAt` order). It sums input/output/cached/reasoning tokens and
cost, and records request count + wall-time span. It is the single
input to both the §5 highlight (Δinput/Δoutput bright cells) and the
§2 recent-request modal (full detail). **Backend grouping was
researched and rejected:** OpenRouter is stateless, its payload
carries no prompt/conversation id (`trace_id` is per-generation), and
Privacy Mode (required, root §3) strips content — there is no shared
key to group on, so time-coalescing is the only viable path. **Safe
because it's display-only:** `aggregate()` still sums every row into
the authoritative window totals untouched; a mis-grouped burst
(genuinely concurrent distinct requests, unusual gaps) only slightly
misattributes the highlight/modal, never a real number — which is what
makes the necessarily-imperfect ~3 s heuristic acceptable to ship.

## 8. Errors & Resilience

**Expected (designed-for) failures** — each has a visible, non-fatal
state, implemented per row except where noted:

| Failure | Behavior |
|---|---|
| Invalid/missing config | Plain-text actionable error before alt screen; exit 1 |
| No network at launch | App starts; bars region shows `✗ offline — retrying in Ns`; recovers automatically |
| Baseline/slice fetch fails | Retry with backoff; keep stale data with a `data from HH:MM` badge; error line in status row |
| Realtime drops | `○ reconnecting…`; backoff rejoin; re-baseline on rejoin |
| Credits fetch fails | Stale value + growing age; 401 hints "check openrouter_api_key" |
| Empty window | Friendly `no usage in this window yet` — not an error |
| Terminal too small | Min-size message (§4) |

**Guarded-against (unexpected) failures** — never crash, log + degrade:
malformed/incomplete realtime payload (log, skip), duplicate events
(dedupe map), clock skew making meter lag negative (clamp to `—`),
division by zero anywhere (`—`), absurd inputs (viewport
scrolling/truncation already handle it), panic (Bubble Tea recovers;
terminal state restored before trace prints — verify empirically),
`Ctrl+C` always quits cleanly.

**Logging:** never stdout (it's the UI). `BURNBAR_DEBUG=1` enables
`tea.LogToFile` to `~/.local/state/burnbar/debug.log`. Silent
otherwise.

**Pending (Stage F):** walking this whole table against the live
backend (killed network, paused project, bad keys) and golden-render
regression tests are still open — see §10.

## 9. Formatting Rules

**Implemented and unit-tested** (`internal/core/format.go`):
adaptive-precision tokens (`842` / `12.5K` / `1.2M` / `3.4B`) and
costs (`$123` / `$12.34` / `$0.4821` / `$0.000073`), durations
(`840ms` / `2.1s` / `1m 12s`), timestamps (`HH:MM:SS` + relative age,
refreshed by the 15 s ticker). All width math via `lipgloss.Width()`,
never `len()`.

## 10. Implementation Stages

Package shape: `tui/` is the Go module; `internal/core` (pure: types,
windows, aggregate, format, bar geometry — testable with `go test`
alone), `internal/data` (config, PostgREST, credits, LiveSource +
impls), `internal/ui` (model/update/view, styles/tokens, keymap,
screens).

Each stage opens with a **Goal** (why this stage exists as its own
unit) and **Done when** (the observable bar that says it's finished).
Completed stages below are intentionally terse — see git history for
the play-by-play; keep it that way per the maintenance rule at the top
of this file.

### Stage A — Shell ✅ (2026-07-05)

Full four-region layout at every breakpoint (incl. too-small state),
alt-screen program with clean quit/suspend/resize, config loading with
the friendly pre-TUI error path, central keymap + help bubble, mouse
augmentation — all driven by fixture data, verified via VHS
screenshots across five breakpoints. `internal/ui/`, fixtures in
`internal/core/fixture.go`.

### Stage B — Pure core ✅ (2026-07-05)

Every calculation the app trusts — dedupe, window cuts, aggregation,
ratios, bar geometry, formatting — covered by `go test ./internal/core`
(~160 cases, all green) before any I/O touches it. `rows.go`,
`windows.go` (DST-verified), `aggregate.go`, `bars.go`, `format.go`.

### Stage C — Data ✅ (2026-07-06)

Fixtures swapped for real PostgREST baseline + today-slice fetches,
credits client + full cadence, `LiveSource` interface with poll +
realtime impls. **`realtime-go` spiked and rejected as a no-go**
(reconnect self-deadlock, stderr logging corrupting the alt screen;
full verdict in root-spec §6) — poll shipped as the interim default.
`internal/data/`.

### Stage C.1 — Realtime via phx ✅ (2026-07-07)

Replaced `realtime-go` with `github.com/nshafer/phx`
(`internal/data/realtime.go`) — survives socket drops and sleep/wake,
logs routed to the debug file only. `p` toggles realtime ↔ poll at
runtime; default flipped back to `realtime`, poll dropped to 20 s as
the manual backup. Verified live against a real INSERT, a network
blip, and a sleep/wake.

### Stage D — Live UX (original) ✅ (2026-07-07) — superseded 2026-07-08

Shipped springs, a rolling-anchor accent1/accent2 highlight, and the
status row (`internal/ui/anim.go`, `meter.go`). A review pass against
the running app then reworked the highlight/mode design — see the
Stage D.1–D.4 sequence below, which replaces it. Kept here only as the
historical predecessor; §3/§5 describe the current target design, not
this one.

---

**Stages D.1–D.4** are a post-Stage-D UI/UX pass, agreed with the user
over two review rounds after living with the running app (round-one:
single burst highlight replaces accent1/accent2 + focus, request
coalescing, fractional-tip smoothing, chrome polish; round-two: dual
cost/token modes replace the length=tokens/split=cost hybrid, plus
manual zoom). Run **in order**: D.1 is safe, self-contained, no `core`
math; D.2 is the visual-core bar rework that establishes the burst
concept and mode/scale generalization everything else depends on; D.3
builds manual zoom on D.2's generalized scale; D.4 is the modal that
reads the burst concept. They slot before Stage E (details screen is
unchanged by them).

### Stage D.1 — Chrome & layout polish ✅ (2026-07-09, pending manual verification)

**Goal / Done when:** see §2 and §4 — header spacer row, fixed list
vertical rhythm, priority-based hint collapse, `j/k` select label.
Implemented in `internal/ui/layout.go`, `keymap.go`. Not yet manually
verified against the running app.

- [x] Header: blank spacer row between wordmark/credits and
  timeframe/spend (3 rows total); `computeLayout` `listTop`/
  `listHeight` adjusted
- [x] Fixed list vertical rhythm: reserve one blank row top and bottom
  of the list region in all of spacer/no-spacer/scroll; scroll
  indicators render inside that frame
- [x] Priority hint-row collapse replacing `help.ShortHelpView`'s `…`
  truncation: protected core `j/k select · ? help · q quit`, then add
  `enter`/`t`/`r`/`p`/`i` by priority while they fit; same for the
  details-screen hint variant
- [x] Select hint label `j/k` (drop the `↑/↓` glyph dependency); arrows
  stay bound and undocumented
- [x] Post-verification fix: the list's top gap was reserved in the row
  budget but never rendered (`renderBars` only padded at the bottom),
  causing the reported resize jitter; `computeLayout`/`blockAt` rewritten
  around one `3k+1`-row formula, the header/status spacers made
  unconditional, the status row never merges away, and the hint row
  switched to a fixed-display-order + independent-removal-rank scheme
  (`refresh` is core but sits mid-order). `minHeight` raised 10→14 to
  fit the seven fixed rows + the 7-row bars-list floor.

### Stage D.2 — Bar rework: dual modes, single highlight, sub-cell smoothing ✅ (2026-07-09, pending manual verification)

**Goal / Done when:** see §3 (dual modes) and §5/§6 (single
latest-burst highlight, fractional-tip smoothing). This was the
highest-risk stage — it touches the most-tested `core` math — and
generalizes scale/geometry over a `value` (cost or tokens) that D.3
will build on. Implemented in `internal/core/bars.go`, `burst.go`,
`types.go`, `internal/ui/meter.go`, `anim.go`, `styles.go`. Not yet
manually verified against the running app.

- [x] `core`: generalize `ScaleFor`/`BarWidth`/`Geometry`/
  `SplitFraction` over a `value` + per-mode 1–2–5 ladder; a `Mode`
  selector picks cost vs token values, split source, sort key
- [x] `core`: `latestBurst(rows)` coalescing (§7, `burstGap`), summed
  input/output tokens and cost + count + span; delete
  `Accent1Tokens`/`Anchor` and the accent1 math; retarget the accent
  unit tests onto the burst
- [x] Bar geometry: two brighter-shade highlight regions (Δinput at the
  input-segment trailing edge, Δoutput at the tail), proportional in
  the active mode's unit, clamped per segment, ≥1-cell floor
- [x] Delete focus/blur handling, `accentWindow`, `accentClearDelay`,
  the v2 focus-report wiring, and the tmux `focus-events` doc note
- [x] UI: `m` toggle (cost default); mode-aware sort, header unit,
  scale-chip unit
- [x] `meter.go` `renderBarRow`: base + bright regions in the bright
  ANSI pair; ~1 s bold arrival pulse
- [x] Fractional-block leading tip (`▏▎▍▌▋▊▉█`) in the tip color;
  ASCII/`NO_COLOR` fallback stays cell-quantized
- [x] Styles: add `accent.primary.bright`/`bar.output.bright`; remove
  `accent.session`/`accent.latest`; `NO_COLOR`/monochrome parity intact
- [x] `go test ./internal/core` green with accent tests rewritten to
  the burst model and new per-mode geometry tests

### Stage D.3 — Manual scale zoom ✅ (2026-07-10, verified)

**Goal / Done when:** see §3's "Manual zoom" — builds directly on
D.2's generalized, mode-aware scale. Implemented in
`internal/core/bars.go` (`NextScale`, ladder-stepping helpers),
`internal/ui/model.go` (`manualScale`, `scale()`), `update.go` (zoom
keys + reset sites), `meter.go` (`·manual` chip suffix), `keymap.go`.
Verified against the running app with real backend data (zoom out/in,
floor clamp, the `·manual` marker, and all three reset triggers), which
also confirmed the intended long-tail-stub fix on a real skewed window.

- [x] Manual `S` as transient view state overriding auto until reset;
  auto does not re-engage while it holds
- [x] `-`/`+`(`=`)/`0` bindings + hint-row/`?`-help entries
- [x] Reset-to-auto on `r`/`t`/`m`; `·manual` marker on the scale chip

### Stage D.4 — Recent-request modal ✅ (2026-07-10, chrome/empty-state verified)

**Goal / Done when:** see §2's "recent-request modal." Implemented in
`internal/core/burst.go` (`Burst.ProviderSlug` + coalescing),
`internal/ui/model.go` (`showModal`), `update.go` (modal capture +
binding), `view.go` (region2 case), `modal.go` (new — the overlay
renderer), `keymap.go`. Overlay chrome, `i`/`esc` open-close, and the
empty state are verified against the running app; no live request
landed during verification, so the populated-burst content and
live-update-in-place are unverified beyond the unit-tested `core.Burst`/
`core.Format*` inputs and the proven `?`-overlay render pattern they
reuse — worth a look next time a real burst lands.

- [x] `core`: expose the `latestBurst` detail object (reuses D.2's
  coalescing) with the modal's fields
- [x] Modal overlay reusing the `renderHelpOverlay` pattern; `i`
  binding + hint-row/`?`-help entries
- [x] Live update in place; empty state before the first live request

### Stage D.5 — Bar interior fade ✅ (2026-07-10, mode/zoom/resize/ASCII verified; live-burst case unverified)

**Goal / Done when:** see §6's "Interior fade." Fixes two problems found
while eyeballing D.2/D.3 against the running app: the input/output split
and burst-highlight boundaries jumped by whole cells every animation
frame (no sub-cell smoothing existed for them, unlike the leading tip),
and the highlight boundary specifically *flickered* — a cell rendered as
the plain growing tip could be retroactively reclassified bright the
instant it became a whole cell, since `core.SplitBar` was recomputed
against the live, moving width every frame. Implemented in
`internal/ui/anim.go` (`fadePhase`, `barAnim.fade`/`fadeStep`,
`settledAt`, `stepFade`, `stepAnim`/`snapBars` updated to drive it),
`internal/ui/meter.go` (`renderBarRow` split into `renderFadingBar`/
`renderResolvedBar`), `internal/ui/styles.go` (`Theme.FadeColor`,
`Glyphs.FadeRamp`). `stepFade`'s phase transitions are unit-tested in
`internal/ui/anim_test.go`. Verified against the running app with real
backend data: a mode toggle shows the entering ramp (`░→▒→▓`, byte-level
confirmed against the actual escape sequences) holding solid yellow,
then correctly revealing the true split color on settle; a terminal
resize shows zero yellow at any point (confirms the snap-clears-fade
path); `TERM=dumb`/`NO_COLOR` runs and exits cleanly with no ramp; the
debug log confirms the tick loop starts once and stops exactly once
per change, never getting stuck mid-fade. Not verified: a live request
landing mid-fade on a growing bar (the specific flicker regression this
change targets) and a brand-new model's first grow-in, since no live
request landed during this session — worth a look next time one does,
same caveat as D.4.

- [x] While a bar's length animation is in flight, render the whole bar
  as one uniform ANSI-16 color (provisional yellow) with no split and no
  highlight; real `SplitBar` geometry is only ever computed once the bar
  is at rest — which also eliminates the highlight flicker as a side
  effect, since `SplitBar` can no longer see a moving width
- [x] Entering/exiting sub-phases ramp through the density glyphs
  (`░▒▓`) for `fadeRampFrames` ticks each, bracketing a solid hold for
  the rest of the motion — a true color gradient isn't possible under
  the ANSI-16-only rule (§5), so the fade illusion is density-based, not
  a color blend
- [x] Exit is gated on the spring's own `settledAt` epsilon, not a fixed
  timer, so the dematerialize always finishes exactly when the length
  animation does; a retarget mid-exit pops back to held rather than
  re-flashing
- [x] Resize (`snapBars`) clears any in-flight fade instantly, same as
  it already snaps position — no fade ever appears on resize (§4)
- [x] ASCII/`NO_COLOR` mode keeps the uniform-fill mechanism (still
  fixes both bugs, since the instability was in `SplitBar`'s inputs, not
  glyph choice) but skips the density ramp — solid fill for the whole
  fade, the same accepted degradation as `FracTips`

### Stage D.6 — Synced batch fade + fade-then-move ordering ✅ (2026-07-10)

**Goal / Done when:** see §6's "Synced batch fade + fade-then-move
ordering." Two refinements requested after using D.5's fade in the
running app. Implemented in `internal/ui/anim.go` (`stepFade` gained an
`autoExit` parameter and now sequences entering/held/exiting instead of
stepping the spring every tick regardless of phase; new
`Model.animSync` field; new `withSyncedAnim`, parallel to `withAnim`;
`stepAnim` gained the group hold-then-flip-together logic), and
`internal/ui/meter.go` (`renderFadingBar`'s ramp-step→glyph index is now
scaled by `len(g.FadeRamp)/fadeRampFrames`, since doubling
`fadeRampFrames` outran the fixed 3-glyph `░▒▓` set). `fadeRampFrames`
doubled from 3 to 6 (~100ms → ~200ms). Every call site that re-cuts the
whole visible bar set in one `Update()` — timeframe switch (key and
click), mode toggle, the three zoom bindings, manual refresh, both
midnight rollovers, `LiveJoined`'s reconnect re-baseline, and the
baseline/today-slice fetch-result handlers — now calls `withSyncedAnim`;
only `handleLive`'s `LiveRow` case (a single live upsert) still calls
`withAnim`. Covered by new cases in `internal/ui/anim_test.go`: the
entering ramp freezes position, a `fadeHeld` bar with `autoExit=false`
doesn't self-exit on settling, a stationary bar still gets forced into
`fadeEntering` by `withSyncedAnim`, and the full group scenario (one
moving bar, one stationary bar) holds until the mover settles and then
exits both on the same tick. `go build`/`go vet`/`go test ./...` all
clean; not yet eyeballed against the running app with real backend data
— worth a look next session, same caveat as D.4/D.5's "verify against a
live request" note.

- [x] `stepFade`: entering freezes the spring for the whole ramp; only
  `fadeHeld` steps `spring.Update`; exiting still gates on `settledAt`
  and still pops retargets-mid-exit back to held without re-flashing
- [x] `autoExit` parameter: individual/live bars still exit `fadeHeld`
  the instant they personally settle; bars in a synced batch wait for
  `stepAnim`'s group check instead
- [x] `withSyncedAnim`: force-starts `fadeEntering` on every tracked bar
  (moving or not) and sets `Model.animSync`; `stepAnim` flips every
  `fadeHeld` bar to `fadeExiting` together only once none are still
  entering and all are settled
- [x] Every batch-triggering call site (timeframe, mode, zoom ×3,
  refresh, both rollovers, `LiveJoined`, baseline/slice fetch results)
  switched from `withAnim` to `withSyncedAnim`; `LiveRow` deliberately
  left on `withAnim` so real-time arrivals stay independent
- [x] `snapBars` (resize) also clears `animSync` — a resize pre-empts
  an in-flight synced batch same as it already pre-empts individual fades
- [x] `fadeRampFrames` 3 → 6; `renderFadingBar`'s glyph index scaled so
  the fixed 3-glyph `░▒▓` ramp still spans the doubled frame count

### Stage E — Details screen

**Goal:** the follow-up question the main screen deliberately raises —
"where exactly is this model's money going?" — answered with the
per-model breakdown the raw rows already carry.

**Done when:** enter/click-through on any model opens its details
screen, every §2 stat renders correctly (`—` for NULL-derived, never
0), the provider split table is cost-sorted, live events update an
open details screen in place.

- [ ] Drill-down navigation (selection ↔ details, live updates in place)
- [ ] Stats grid + provider split table per §2, `—` for NULL-derived
  values

### Stage E.1 — Live theme picker (ANSI-16 remap)

**Goal / Done when:** see §5's "Pending — live theme picker." Sequenced
after Stage E and D.2 (needs the finished highlight system to preview
meaningfully).

- [ ] Theme-editor screen (token list + live meter sample); edit-in-
  place → live in-memory apply → `s` persist / `esc` revert
- [ ] `[colors]` table in `config.toml` with existing env-override
  precedence; missing/partial falls back to the §5 default per-token;
  unknown color names rejected with a hint
- [ ] Preview renders through the same style tokens the real view uses
- [ ] `NO_COLOR`/monochrome parity preserved; ASCII glyph fallbacks
  untouched

### Stage F — Hardening & polish

**Goal:** robust and documented enough to hand to a stranger who will
self-host it unaided — every designed-for failure degrades visibly
instead of crashing, main screens locked down by golden tests, docs
carry someone from `git clone` to live bars.

**Done when:** the full §8 failure table has been walked against the
live backend with every row behaving as specified, golden-render tests
pass for the main-screen states (color profile pinned), VHS smoke
shots render, config/debug-logging docs are written.

- [ ] Walk the full §8 failure table against the live backend (kill
  network, pause project, bad keys)
- [ ] Golden-render tests via teatest/v2 (`WithInitialTermSize(80,24)`,
  ASCII color profile pinned — unpinned profiles are the #1 golden
  flake) for the main screen states
- [ ] VHS screenshot smoke (the `vhs-cli-demos` skill) — doubles as
  README material later
- [ ] README/docs: config reference, debug logging

**Definition of done (= root-spec Phase 2):** run `burnbar`, fire an
OpenRouter request, watch the bar animate within seconds — through at
least one disconnect/reconnect without restarting the app.

## 11. Open Items

All design decisions in this file are confirmed with the user except:

- **Provisional keybinds** — `i` (recent-request modal), the exact
  zoom keys (`-`/`+`/`0`), and `m` (mode toggle) are free,
  hint-row-friendly picks, not load-bearing choices; change on review
  if better mnemonics surface.
- **"Tune by feel" constants** — `burstGap` (~3 s), the spring
  frequency/damping, and the dollar-floor rung of the 1–2–5 ladder are
  all expected to move once the app has seen real daily use.

Anything discovered during implementation that contradicts a decision
in this file goes back to the user before code deviates.
