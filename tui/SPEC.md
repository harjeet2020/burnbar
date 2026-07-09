# Burnbar TUI — Design & Implementation Spec

> **How to use this file (for AI agents):** This is the runbook for the
> Go TUI. Read it together with the root `SPEC.md` (§2 Frontend Data
> Contract is binding here) and the README UI/UX section. Work through
> the stages in §10 in order; check off items and note partial work as
> you go. All design decisions were agreed with the user on 2026-07-05
> over three review passes (§11) — if implementation reveals one to be
> wrong, stop and discuss before deviating.

## 1. Product Shape

A long-running, full-screen terminal app (`burnbar`) on the alternate
screen. One primary screen (the meter) and one drill-down (model
details). It sits in a corner of a terminal all day, so the design
priorities are: calm at rest (no flicker, no pointless motion), alive
on events (a request landing is *seen*), and honest about its own state
(connection, staleness, lag are always visible).

**Stack:** Bubble Tea **v2** (`charm.land/bubbletea/v2` — v2 is stable
and current; `View()` returns `tea.View`, alt-screen is declared on the
view, key handling uses `tea.KeyPressMsg`), Lip Gloss v2, Bubbles
(help/key/viewport), Harmonica for springs. Verify current v2 APIs at
build time rather than trusting training data — the v1→v2 surface
changed materially.

## 2. Screen Anatomy

```
 ▂▄▆ burnbar                            credits  $12.43 · 2m

 [today]  week  month                        spent  $1.2345

 deepseek/deepseek-v4-flash    1.2M in · 340.2K out  $0.4821
 ███▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▊

 anthropic/claude-haiku-4.5    210.4K in · 88.1K out $0.3110
 ██▒▒▒▒▒▒▒▒▒▒▒▒▒▊

 qwen/qwen3-coder               955.0K in · 82.3K out $0.0214
 ▊

 last request 12:04:32 (2m ago) · lag 2.1s · scale $1 · ● live
 j/k select · enter details · t window · m mode · ? help · q quit
```

Four fixed regions, top to bottom — **regions never move or reorder**.
Notes on this sketch:
- It shows the default **cost mode** (§3): the header reads `spent $…`,
  the status scale chip is dollar-denominated (`scale $1`), and bar
  **length is proportional to cost**, so `qwen` — a huge *token*
  workhorse (955K) but nearly free ($0.02) — is a cost stub. Press `m`
  for token mode, where `qwen` would be one of the longest bars. That
  stub is exactly what manual zoom (`-`/`+`, §3) expands to compare.
- Plain monospace can't show color, so the latest-burst highlight isn't
  visible here: on screen, the input segment's trailing cells render in
  bright cyan and the tail's cells in bright blue — the last burst's
  input and output (cost, in this mode), §5. The `▊` at each bar's
  leading edge is the sub-cell fractional animation tip — §6.

1. **Header (3 rows):** wordmark left (`▂▄▆ burnbar` — the glyph is a
   tiny bar-chart motif; drop the glyph in ASCII fallback). Right-aligned
   column: credits balance **with its age** (row 1) and the active
   window's total in the active mode's unit (row 3) — `spent $1.2345`
   in cost mode, `used 1.5M` in token mode (§3), reinforcing which mode
   is live. A blank spacer row separates the two (row 2) so the
   wordmark/credits line reads as its own band above the timeframe line.
   Row 3 left: the timeframe selector — three labels with the active one
   highlighted (reverse video), the others muted; **`t` cycles** today →
   week → month → today.
2. **Bars list (fills remaining height):** one block per model used in
   the window, **sorted by the active mode's metric desc** (window cost
   in cost mode, total tokens in token mode; ties: name asc — stable
   order, no jitter, §3). Each block = label row + bar row (+ one blank
   spacer row when height allows; drop spacers first when tight).
   Scrolls inside a viewport when models exceed available height, with
   `▼ 3 more` / `▲ 2 more` edge indicators. **Fixed vertical rhythm
   (§4):** exactly one blank row separates the header from the first
   list element and the last list element from the status row, in
   *every* density mode — spacer, no-spacer, and scroll. The scroll
   indicators live *inside* that frame (a blank row always sits between
   `▲ N more` and the header, and between `▼ N more` and the status
   row); the list never touches the chrome above or below it, and the
   gap does not change as the window resizes.
3. **Status row:** last-request wall time + relative age, meter lag
   (§7), the current bar scale in the active mode's unit (`scale 2M` in
   token mode, `scale $5` in cost mode, muted — see §3; this doubles as
   the **mode indicator**, and gains a `·manual` marker when the scale is
   a hand-set zoom), connection state: `● live` (realtime healthy),
   `◌ polling` (poll mode, chosen via `p`), `○ reconnecting…` (with retry
   countdown), `✗ offline`. Symbols + words, never color alone.
4. **Hint row:** the core bindings from the central keymap, collapsed by
   **priority** as width shrinks (not ellipsized). `j/k select · ? help ·
   q quit` are the protected core — they always fit, down to the smallest
   screen. As width allows, lower-priority bindings are added back in
   order: `enter details`, `t window`, `m mode`, `± zoom`, `r refresh`,
   `p source`, `i last request`. This replaces the `help` bubble's blunt
   `…` truncation, which could hide *everything* on a narrow terminal.
   `?` toggles the expanded help overlay (every binding, always reachable
   there regardless of width).

**Keymap:** `t` cycle timeframe · `m` toggle mode (cost ↔ tokens, §3) ·
`-`/`+` zoom scale out/in · `0` reset scale to auto (§3) · `j/k` (or
`↑/↓`) select · `enter`/`l` details · `esc`/`h` back · `r` refresh ·
`p` toggle live source (realtime ↔ poll) · `i` recent-request modal (§7)
· `?` help · `q`/`ctrl+c` quit. Both `j/k` and the arrow keys move the
selection, but the hint row and help advertise **`j/k`** — the arrows are
an undocumented bonus, so the primary label is compact and vim-native.
Single-key timeframe cycling (`t`) replaced the earlier `d`/`w`/`m`
bindings; `m` is now the cost/token **mode** toggle. (`i` for the
recent-request modal, and the exact zoom keys, are provisional — free,
hint-row-friendly picks; change on review if better mnemonics surface.
`+` and `=` are the same physical key, so both zoom in.)

**Mouse (augmentation, never required):** wheel scrolls the list; click
selects a bar; click on the already-selected bar (or double-click) opens
details; clicking a timeframe label activates it. Every mouse action has
a keyboard equivalent.

**Details screen** is a full-screen drill-down replacing region 2 (header
/status/hints persist, hints swap to context: `esc back · t window ·
r refresh · q quit`). Chosen over an inline expanding panel because it
degrades gracefully at small sizes and keeps the main screen's spatial
layout sacred. Contents for the selected model over the active window,
all derived client-side per root-spec §2:

- Totals: requests, input/output tokens, total cost with input/output
  cost split.
- Cache: cached tokens, cache hit % (= cached/input).
- Reasoning: reasoning tokens, % of output.
- Effective rates: input & output **$ per 1M tokens** (= cost/tokens ×
  1e6 — reflects discounts actually received, per root spec).
- Avg request duration (= duration_ms_sum / timed_request_count).
- Provider split table (`lipgloss/table`): provider_slug · requests ·
  tokens · cost · share % · effective rates. Sorted by cost desc.
- Unreported values (NULL) render as `—`, never as 0 (root-spec §2
  NULL-vs-0 rule guards every ratio here).

The details screen stays live — realtime events update it in place.
Selection state: selected model name rendered bold + `▸` prefix.
Selection follows the *model*, not the row index, across re-sorts.

**Recent-request modal (`i`)** is a centered overlay (same chrome as the
`?` help overlay — the app's one transient border, §2 region 4) that
answers "what did the last thing that happened actually cost?" — the
question a live meter raises but the bars can't fully answer at a glance.
It shows the **most recent burst** (§7 — same-model live requests
coalesced within `burstGap`, so an agent's rapid tool-call fan-out reads
as one logical action, not a flicker of a dozen tiny ones):

- Model (and routed provider slug when reported).
- Requests coalesced (`3 requests` when >1; the time span they covered).
- Input / output tokens, cached and reasoning tokens, all summed over the
  burst; `—` for unreported (NULL) facts, never 0.
- Total cost with the input/output split.
- Wall time of the burst's latest request + relative age, and meter lag.

It is **live** (a new arrival updates or replaces it in place) and
**display-only** — it reads the same coalesced object that drives the §5
highlight, and neither ever feeds the authoritative window totals. When
no live request has been seen this session, the modal shows a friendly
`no requests seen yet` line. `i` or `esc` closes it.

## 3. The Bars

**Two display modes — cost and tokens — toggled by `m`.** *(Revised
2026-07-08 — supersedes the earlier "two decoupled axes" design.)* The
app never tries to show volume and money in the *same* bar (that made a
cell mean neither — see the revision note below). Instead each of two
modes is **single-denomination end to end** — length, interior split,
sort order, and the §5 highlight all speak one language — so the intent
of every cell is unambiguous, and `m` re-renders between them:

- **Cost mode** (the default — burnbar is a money meter): bar length ∝
  the model's **total window cost**; the interior split is
  **input-cost vs output-cost**; the highlight is the latest burst's
  **input/output cost** (§5). Answers "how fast is my money going, and on
  what".
- **Token mode:** bar length ∝ the model's **total tokens** (input +
  output); the split is **input-token vs output-token volume**; the
  highlight is the burst's **input/output tokens**. Answers "how much am I
  actually using".

**Sort follows the mode:** cost mode sorts by window cost desc, token
mode by total tokens desc (ties: name asc). So in either mode the longest
bar sits on top and vertical position agrees with length — each mode is
internally coherent. Toggling `m` may re-sort; selection follows the
*model* across it (§2). *(Judgment call, overridable: the alternative was
a stable always-by-cost sort that never jumps vertically on toggle; per-
mode coherence won.)*

> **Why two modes, not the old hybrid.** The prior design set length ∝
> tokens but split the interior by *cost* — so an individual cell was
> neither a fixed number of tokens nor a fixed number of dollars, just a
> product of the two, and there was no honest place to draw "this burst
> added Δinput". Splitting into two clean modes keeps *both* truths (spend
> shape **and** usage volume) available on a keypress, with each bar
> coherent. The label row (below) still shows the numbers for the *other*
> denomination, so no information is ever hidden.

**Auto-ranging scale (no bar is ever pinned at 100%).** All bars share
one scale `S`, drawn from a **1–2–5 ladder in the active mode's unit** —
tokens (`10K, 20K, 50K, 100K, …`; floor 10K) or dollars (`$0.001,
$0.002, $0.005, $0.01, …`; a small dollar floor, tuned by feel). `S` is
the smallest ladder value such that `max_i(value_i) ≤ 0.8·S`, a pure
function of the current window + mode — no state, no hysteresis. Bar
width = `W · value_i / S`, **minimum 1 cell** for any nonzero model
(this floor is what keeps a heavily-used **free/$0 model** visible in
cost mode rather than vanishing). The geometry generalizes over `value`
(cost or tokens), so one code path serves both modes.

The UX this produces: early in a day the first requests visibly fill
the top bar; when it crosses 80% the scale steps up and **all bars
shrink together** (a springs-animated "zoom out"), after which each
request advances the bar by proportionally less — sustained usage reads
as repeated zoom-outs, exactly the "thresholds that scale with usage"
feel. Because every bar shares `S`, proportions *between* bars stay
mathematically exact at all times.

**Manual zoom (`-` / `+` / `0`) — the honest fix for the stub problem.**
A long-tailed distribution (one or two dominant models, a handful of
tiny ones) squashes the tail into indistinguishable 1-cell stubs under
the auto scale. Rather than a *nonlinear* scale — which would break
exact proportionality **and** fight the animation (a fixed Δ would move
the bar by a size-dependent amount) — the user steps the **same linear
1–2–5 ladder** by hand:

- `-` zooms **out** (next larger `S`, bars shrink); `+` (and `=`) zooms
  **in** (next smaller `S`, bars grow); `0` **resets to auto**.
- Zooming in lengthens the small bars so their real differences show
  (and makes incoming requests arrive as larger, more visible chunks);
  the trade-off — accepted — is that bars now exceeding `S` **clamp to
  full width** (`BarWidth` already caps at `W`), so several large bars
  pin at ~100% and lose their mutual differences. Zoom out for the
  opposite trade. There is no single scale that distinguishes both ends
  of a long tail; manual zoom lets the user choose which end to inspect.
- A manual `S` is **transient view state**, not config: it **resets to
  auto** on refresh (`r`), window switch (`t`), and mode toggle (`m`) —
  each re-baselines the view, and the auto scale differs per window and
  per denomination anyway. While a manual `S` holds, auto-ranging does
  not re-engage even when new data would overflow it (the bars clamp).
- The zoom retargets every bar at once, reusing the §6 collective
  scale-step animation, so a manual zoom reads as the same satisfying
  "everyone re-scales together" motion.

The current scale is always shown muted in the status row in the active
mode's unit (`scale 2M` tokens, `scale $5` cost) — which is also the
primary **mode indicator** — with a marker when it is manual
(`scale 500K·manual`) so a pinned zoom is never mistaken for auto.

**Inside a bar — the split, in the active mode's unit.** The boundary
between the input segment (full block `█`) and output segment (medium
shade `▒`, same hue) sits at the input share of the bar's own
denomination: `input_cost / (input_cost + output_cost)` in **cost mode**,
`input_tokens / (input_tokens + output_tokens)` in **token mode**. The
cost split shows at a glance what fraction of spend went to input vs
output — truthful in a way token proportions can't be, because output
tokens cost several times more and cache discounts shrink effective input
cost; the summed **actual split costs** already embody those discounts
(the schema stores facts, not `unit_price × tokens`), so cached-token
handling is automatic and exact. Cost-mode fallback: if split costs are
unreported (NULL sums) or both zero (free models), fall back to the
token-volume split for the interior boundary (length stays honest — total
cost is always known). The glyph difference keeps the split readable in
monochrome in either mode.

**Label row** (above each bar): model name left (the alias slug, e.g.
`deepseek/deepseek-v4-flash`); then both denominations, with the
**active mode's metric** bold and right-aligned as the row's anchor and
the other denomination muted beside it — so the bold right column always
mirrors the sort order (§3 "sort follows the mode"), and the *other*
denomination stays visible so no information is hidden. In **cost mode**:
`1.2M in · 340.2K out` (muted, the input/output token breakdown) …
`$0.4821` (bold cost anchor). In **token mode**: `$0.4821` (muted) …
`1.5M` (bold total-tokens anchor). Right-aligned anchors down the screen
read as a column, mirroring the active ordering.

## 4. Responsive Behavior

Layout derives from the cached `WindowSizeMsg` on every resize — no
fixed dimensions anywhere. Resizing **snaps** (no animation): springs
animate data changes, but a resize must feel like the terminal is in
charge. Ladder, by columns:

- **≥110:** full labels, verbose tokens, spacer rows between models.
- **80–109:** spacers dropped when height demands, tokens compact
  (`1.2M→340K`).
- **60–79:** model names middle-truncated keeping the model part
  (`…/deepseek-v4-flash`); the **secondary** denomination dropped from
  the label row, the active mode's bold metric anchor stays (cost in cost
  mode, tokens in token mode — §3; the details screen has the rest).
- **40–59:** name + active-metric anchor + bar only.
- **Below 40×10:** centered `terminal too small (min 40×10)` message —
  never a crash or a mangled layout.

Height pressure drops, in order: spacer rows → status row merges into
hint row → list scrolls. Header (now **3 rows** — wordmark, blank
spacer, timeframe/spend, §2) and hint row never disappear.

**Fixed list vertical rhythm.** The bars list keeps a constant one-row
gap above its first element and below its last, in every density mode:

- The old layout only reserved a top gap in *spacer* mode. In no-spacer
  and scroll modes the first block (or the `▲ N more` indicator) butted
  against the header and the bottom indicator butted against the status
  row — so the whitespace visibly jittered as the window resized. That
  is the bug this rule fixes.
- Invariant: reserve one blank row at the top of the list region and one
  at the bottom **before** placing content, in all of spacer /
  no-spacer / scroll. The `▲`/`▼` scroll indicators render *inside* that
  frame (so a blank always separates them from the chrome), and the
  number of visible blocks is computed against the height that remains
  after the two reserved rows. The result: consistent breathing room
  regardless of size or mode, even when it costs one fewer visible bar.

## 5. Color, Accents & Theming

**ANSI-16 only, semantic tokens, no hardcoded hex.** The app inherits
the user's terminal color scheme, so it looks native in any theme.
(Fonts aren't a decision at all: terminal apps render in whatever font
the emulator uses. We only rely on standard block/box glyphs —
`█ ▒ ▸ ● ◌ ▼` — no Nerd Font; every glyph has an ASCII fallback:
`# = > * . v`.)

| Token | ANSI | Used for |
|---|---|---|
| `accent.primary` | cyan | wordmark, active timeframe, selection, input segment |
| `accent.primary.bright` | bright cyan | latest-burst **input** highlight (trailing input cells) |
| `bar.output` | blue | output segment (plus the `▒` glyph difference) |
| `bar.output.bright` | bright blue | latest-burst **output** highlight (the bar tail) |
| `text.primary` | default fg | names, values |
| `text.muted` | bright black | token counts, timestamps, inactive labels, hints, scale |
| `status.ok` / `status.warn` / `status.error` | green / yellow / red | connection dot, error banner |

The highlight uses the **bright ANSI pair** of the two segment colors,
not two extra hues — this is what let the old `accent.session` (yellow)
and `accent.latest` (magenta) slots be **retired entirely** (freeing two
ANSI-16 slots and simplifying the Stage E.1 theme picker's token list).

Honor `NO_COLOR` and dumb terminals: monochrome must remain fully
readable (segments by glyph, selection by `▸` + bold, status by symbol
+ word).

**The latest-burst highlight — one highlight, brighter-shade
augmentation.** *(Revised 2026-07-08, superseding the earlier
rolling-anchor + accent1/accent2 + focus-events design — see Stage D.2.)*

There is exactly **one** highlight: the **most recent burst** (§7 —
same-model live requests coalesced within `burstGap`; a fan-out of tool
calls is one logical action). It is drawn not as a separate colored
slice appended to the bar, but as a **brighter shade of the segment it
belongs to**, so the bar never carries more than its two semantic hues.
It works identically in both display modes (§3), reading the burst's
value in the **active mode's unit** — cost in cost mode, tokens in token
mode:

- The burst's **input** value brightens the **trailing edge of the input
  segment** (`accent.primary.bright`) — the input side visibly grew.
- The burst's **output** value brightens the **tail** of the bar
  (`bar.output.bright`) — the output side visibly grew.

So a landing burst resizes *both* sides of the bar and paints the added
cells in the bright variant of each side's own color: `[in][Δin·bright]
[out][Δout·bright]` — in cost mode those Δ's are the burst's input/output
**cost**, in token mode its input/output **tokens**. This is strictly
more legible than the old scheme, where an entire request (input **and**
output) was appended as a single foreign-colored slice, giving the bar
3–4 competing hues that were hard to read at a glance.

**Why the delete is a win.** This retires the whole `accent1` /
rolling-`anchor` / focus-and-blur subsystem: no `accentWindow`, no
`accentClearDelay`, no xterm-1004 focus reporting, no tmux
`focus-events` documentation, no "usage since anchor" concept. Keyboard
focus was always a poor proxy for "seen it" on a meter that lives
*visible but unfocused* beside the working terminal; dropping it removes
the app's fiddliest state with no real loss.

**Highlight geometry.** The bright regions are **proportional in the
active mode's unit** (the same shared-scale math as bar length, §3): the
Δinput and Δoutput cell counts come from the burst's summed input/output
cost (cost mode) or tokens (token mode). Each bright region is clamped
inside its own segment. The **minimum-visible floor** is preserved: a
live burst always paints **≥1 bright cell** (borrowed from the base of
the segment to its left) so a burst too small to lengthen the bar by a
whole cell is still *seen* (§1) — the tiny-request guarantee, applied to
the two-region highlight.

**Lifecycle (persist until superseded).** On arrival the highlight gets a
brief ~1 s **bold** emphasis (§6) — the landing pulse, and the primary
signal for a sub-cell tiny burst. It then settles to the plain bright
shade and **persists** until a newer burst supersedes it (a newer burst
for any model moves the highlight there — only one model is ever
highlighted) or a manual refresh clears it. No timed fade, no focus
clearing: at rest the bar simply keeps showing "here is what the last
activity added," which is exactly what a glanceable meter wants. (The
recent-request modal, §2/§7, is the on-demand detail behind that
highlight.)

The highlight lives only on live rows and only when the burst falls
inside the active window; a manual refresh (which drops the live deltas,
§7) therefore also clears it. Accepted: refresh means "re-baseline my
view of the world."

## 6. Motion

Harmonica springs, one per bar, animating **bar width** toward its
target whenever data changes (new event, scale step, window switch,
refresh). Segment and highlight boundaries are computed as fractions of
the animated width each frame — no separate springs. A scale step (§3)
retargets every spring at once — the collective zoom-out is the app's
most satisfying moment; make sure springs are tuned so it reads as one
motion, not a ripple. New model appearing: grows from 0. Model leaving
the window: shrinks to 0, then the row is removed (rows don't animate
vertically; reorders are discrete).

**Sub-cell smoothing via fractional block glyphs.** *(Added 2026-07-08
— Stage D.2.)* The spring animates a *fractional* cell position, but the
naïve render rounds it to whole cells, so the smooth motion is quantized
away — worst at the end of the travel, where a critically-damped spring
decelerates and the last cell or two land as visibly separated steps.
The fix is not more FPS (the choppiness is **spatial**, not temporal —
even at 120 fps a full-block bar still jumps a whole cell at a time) but
**8× finer horizontal resolution**: render the bar's **leading tip** as a
fractional block glyph (`▏▎▍▌▋▊▉█`, eighths) in the tip's color, so the
edge advances in eighth-cell steps. This is the standard smooth-progress
technique (`pv`, `btop`).

Constraints, so this is honest:
- The tip is a **solid** partial block even when the tip sits in the
  shaded output segment — there is no partial-width `▒`, so the moving
  edge reads as a clean solid growing tip (a slight, deliberate glyph
  mix; tune by feel).
- The interior seams (input/output cost split, the highlight boundaries)
  do **not** need sub-cell smoothing — only the overall length change
  does, and that lives at the tip.
- **ASCII / `NO_COLOR` fallback** has no fractional glyphs, so it stays
  cell-quantized — an accepted, documented degradation, not a bug.

**Performance rule — idle means idle:** the app renders at 0 fps until
a message arrives. A tick loop (~30 fps) runs **only while any spring
is unsettled**, then stops. A slow 15 s ticker refreshes relative
timestamps and the credits age — the only steady-state wakeup. No
animation on resize (snap, §4) or on the details screen (values just
update).

The highlight arrival moment gets a brief emphasis: the new bright
region renders **bold** for the first ~1 s (one timed message, not a
fade — ANSI-16 has no alpha). This ~1 s emphasis is also the *primary*
arrival signal for tiny bursts (§5's minimum-visible 1-cell floor): when
a landing burst is too small to lengthen the bar, the single bright cell
pulsing bold for a second is what makes it seen.

## 7. Data & State Logic

### State model

Three stores plus derived view state:

- **`baseline`** — the `usage_daily` rows (all columns, full 30 days)
  from the last PostgREST fetch, tagged with `fetchedAt`. UTC-day
  grain.
- **`rows`** — raw `requests` rows, **deduped by (trace_id, span_id)**
  (a map keyed on the pair), from two sources that share one shape:
  the **today-slice fetch** (PostgREST query for all rows with
  `requested_at ≥ local midnight`, fetched alongside the baseline) and
  **live events** from the LiveSource. Live-sourced rows are flagged
  and carry `receivedAt` — only they participate in the latest-burst
  highlight (§5), the recent-request modal (§2), and meter lag.

(The earlier **`anchor`** store is **removed** — the rolling-anchor /
focus-events accent design it served was retired in the 2026-07-08
revision, §5/Stage D.2.)

**Aggregation is one pure function** — the heart of the app and the
most-tested code in it:

```
aggregate(baseline, rows, window, now) → []ModelStat
```

All ratios (cache %, effective rates, avg duration, provider share,
cost-split boundary) are computed here from sums, never averaged across
rows, and every division is zero-guarded (render `—`). Per-model and
per-(model, provider_slug) grains both come out of this function; raw
rows carry every column the view sums, so live data enriches the
details screen too.

### The latest burst — display-only request coalescing

*(Added 2026-07-08 — Stage D.2/D.3.)* Agents fan a single prompt out into
many rapid OpenRouter calls (tool loops), each landing as its own
`requests` row. A "single most recent request" highlight would then show
only the *last* tiny call of a twelve-call burst — misleading for a live
meter. So a pure helper coalesces them **for display only**:

```
latestBurst(rows) → Burst   // nil when no live row has been seen
```

- A **burst** is a run of **live** rows for the **same model** whose
  arrivals are each within `burstGap` (a named constant, ~3 s, tune by
  feel) of the previous one, walked back from the most recent live
  arrival (`receivedAt` order, the same deterministic tie-break as the
  old accent2 pick). It sums input/output/cached/reasoning tokens and
  cost, and records the request count and the wall-time span.
- It is the single input to **both** the §5 highlight (Δinput/Δoutput
  bright cells) and the §2 recent-request modal (full detail).
- **Backend grouping was researched and rejected:** OpenRouter is
  stateless and its Broadcast/OTLP payload carries no
  prompt/conversation id — `trace_id` is per *generation*, not per
  conversation, and Privacy Mode (required, root §3) strips content.
  There is no shared key to add a column for; a passive meter cannot
  assume clients inject one. Time-coalescing is therefore the only
  viable path.
- **Safe because it is display-only.** `aggregate()` still sums every
  row into the authoritative window totals, untouched. A mis-grouped
  burst (genuinely concurrent distinct requests, unusual gaps) only
  slightly misattributes the *highlight and modal*, never a real number
  — which is what makes the necessarily-imperfect ~3 s heuristic
  acceptable to ship.

### Windows — local today, UTC week/month

- **`today` = the user's local calendar day**, computed **exclusively
  from `rows`** (today-slice ∪ live, deduped): sum rows with
  `requested_at ≥ local midnight`. View rows are UTC-day grain and
  can't be re-cut to local midnights — the raw slice can, exactly, for
  any UTC offset (including :30/:45 zones), and it makes the most-
  stared-at window **exactly correct** (the dedupe map even eliminates
  the subscribe/fetch overlap double-count here). Rationale: a UTC
  "today" would visibly reset mid-session for most timezones —
  confusing where it matters most.
- **`week` / `month` = last 7 / 30 UTC days inclusive of the current
  UTC day**, computed as before: baseline view rows + live rows layered
  on top (the brief overlap double-count self-heals on refresh, per
  root-spec §2). These windows stay UTC-pure — mixing a local today
  into UTC day stacks would double-count the overlap hours.
- **Rollover timers:** at local midnight, the today window empties
  (clear the today-slice, refetch it, re-aggregate); at UTC midnight,
  week/month slide (re-aggregate). Local-midnight math goes through the
  system timezone at each scheduling, so DST shifts are handled for
  free.
- **Timeframe switching (`t`) is pure client-side re-aggregation — no
  refetch.** Baseline and slice are both already in memory; switching
  is instant.

### Startup sequence

1. Load config; on any problem print a plain, actionable error **before
   entering the alt screen** and exit non-zero.
2. Start the program; render immediately with loading placeholders
   (spinner in the bars region, `…` for credits).
3. Connect realtime and **subscribe first**; when the subscription is
   confirmed, fetch the baseline + today-slice (root-spec §2 ordering —
   brief double-count self-heals, silent undercount doesn't).
4. Fetch credits in parallel.

### Realtime (via phx), and the manual poll backup

Primary: a Phoenix-channel WebSocket to
`wss://<ref>.supabase.co/realtime/v1/websocket` carrying
`postgres_changes` INSERT on `public.requests`. **On every successful
(re)join: refetch baseline + today-slice and clear live rows** — events
missed while disconnected are healed by re-baselining, so gap tracking is
never needed.

The transport is **`github.com/nshafer/phx`**, a maintained general Go
Phoenix Channels client. The original `supabase-community/realtime-go`
proved a no-go: its reconnect self-deadlocks and it logs to stderr
(corrupting the alt-screen) — see root §6. phx gives us heartbeats,
reconnect-with-backoff, channel rejoin, and a silent default logger as
first-class features, so we own only a thin Supabase framing layer on top
(the `phx_join` payload carrying `config.postgres_changes` +
`access_token`, and decoding INSERT payloads through the existing
`requestDTO`). Making that swap is the next task (Stage C.1).

**Poll is a manual backup, not a silent fallback.** Both sources sit
behind the same **`LiveSource` interface** (emits request-row events +
connection-state changes), but rather than auto-switching on a
hard-to-observe heuristic, the user drives it: `live_source` sets the
startup source, and the in-app **`p`** key toggles realtime ↔ poll at
runtime. If realtime ever misbehaves, you *see* it (the status row
degrades) and flip to poll yourself — no hidden state machine deciding
for you. The poll source queries PostgREST
(`requests?inserted_at=gt.<last>`) every **20 s** — deliberately slower
than realtime, sized as a low-cost backup that stays gentle on the free
tier (a 2 s poll running all day is ~1.3M requests/month of mostly-empty
responses; 20 s is ~10× lighter). The `◌ polling` status state (§2) keeps
the active source honest on screen.

### Credits

`GET https://openrouter.ai/api/v1/credits` with the user's key
(`Authorization: Bearer`); balance = `total_credits − total_usage`.
Displayed as-is, no local decrement (root-spec §2), **always with its
age** (`$12.43 · 2m`; `now` under 30 s). Poll cadence — event-driven
with a slow heartbeat, per root-spec §2:

- On launch and on every manual refresh (`r`).
- **Burst-grouped debounce after broadcast events.** Events less than
  10 s apart belong to the same burst: the burst's first event
  schedules a poll at `event + 70 s`, and each subsequent burst member
  *pushes that same timer* to its own `+70 s`. An event arriving ≥10 s
  after the previous one starts a **new burst with its own
  independently scheduled poll** — earlier bursts' polls fire as
  scheduled, so the balance updates promptly per spend moment instead
  of being deferred by unrelated later requests. (Worked example:
  requests at t=0, t=5, t=20 → the t=5 event pushes the first timer to
  75; the t=20 event, 15 s after its predecessor, schedules a separate
  poll at 90 — two polls total.) The 70 s delay exists because
  OpenRouter caches credit values up to ~60 s: `lastEvent + 70 s` is
  the earliest poll guaranteed to see the entire burst. Two guards: a
  pending poll is never pushed more than ~120 s past its first
  scheduling (a minutes-long rapid burst — sub-10 s gaps throughout —
  still gets interim polls instead of starving), and two pending polls
  whose targets land within 10 s of each other merge into the later
  one (near-simultaneous polls read the same cached value — pure
  waste).
- Every 5 minutes otherwise (timer resets on any successful poll) —
  the idle heartbeat for spend from other machines.

On fetch failure: keep showing the last value with its (growing) age —
a stale number labeled stale beats a blank; a 401 additionally hints
"check openrouter_api_key".

### Config

Real config: `~/.config/burnbar/config.toml` (via `os.UserConfigDir()`
on other platforms) — **outside the repo, user-managed, never
committed**. The repo commits `tui/config.example.toml`, a fully
commented template with placeholder values; `.gitignore` additionally
excludes `config.toml` anywhere in the repo as a belt-and-braces guard
against someone copying their real config into the working tree. Env
vars override file values:

| TOML key | Env override | Required |
|---|---|---|
| `supabase_url` | `BURNBAR_SUPABASE_URL` | yes |
| `supabase_anon_key` | `BURNBAR_SUPABASE_ANON_KEY` | yes |
| `openrouter_api_key` | `BURNBAR_OPENROUTER_API_KEY` | no — credits header shows `—` with a hint when absent |
| `live_source` | `BURNBAR_LIVE_SOURCE` | no — sets the *startup* source (`poll` \| `realtime`); toggle at runtime with `p`. Default `poll` today, `realtime` once Stage C.1 lands |

Missing-config error message includes a ready-to-paste TOML template.
The repo `.env` maps onto the env overrides for dev convenience.

An optional `[colors]` section (token → ANSI color name) is managed by
the Stage E.1 theme picker rather than hand-edited in the normal case;
absent or partial, the §5 default slot applies per-token.

## 8. Errors & Resilience

**Expected (designed-for) failures** — each has a visible, non-fatal
state:

| Failure | Behavior |
|---|---|
| Invalid/missing config | Plain-text actionable error before alt screen; exit 1 |
| No network at launch | App starts; bars region shows `✗ offline — retrying in Ns`; recovers automatically |
| Baseline/slice fetch fails | Retry with backoff; keep stale data rendered with a `data from HH:MM` badge; error line in status row |
| Realtime drops | `○ reconnecting…` state; backoff rejoin; re-baseline on rejoin |
| Credits fetch fails | Stale value + growing age (§7); a 401 specifically hints "check openrouter_api_key" |
| Empty window | Friendly empty state (`no usage in this window yet`) — not an error |
| Terminal too small | Min-size message (§4) |

**Guarded-against (unexpected) failures** — never crash, log + degrade:

- Malformed/incomplete realtime payload → log, skip the event (mirror
  of the ingest parser's tolerance).
- Duplicate events → the (trace_id, span_id) dedupe map.
- Clock skew making meter lag negative (lag = `receivedAt −
  (requested_at + duration_ms)`, the "request concluded → seen by the
  meter" figure in the status row) → clamp to `—`; missing
  `duration_ms` → `—`.
- Division by zero anywhere ratios are derived → `—`.
- Absurd inputs (hundreds of models, very long slugs) → viewport
  scrolling + truncation already handle it.
- Panic → terminal state restored before the trace prints (Bubble Tea
  recovers; verify raw mode + alt screen release on a deliberate test
  panic).
- `Ctrl+C` quits cleanly always; `Ctrl+Z`/resume and resize are handled
  by the framework — verify, don't assume.

**Logging:** never stdout (it's the UI). `BURNBAR_DEBUG=1` enables
`tea.LogToFile` to `~/.local/state/burnbar/debug.log` (`tail -f` in a
second terminal is the debug workflow). Silent otherwise.

## 9. Formatting Rules

Deterministic and unit-tested:

- **Tokens (and the scale chip):** `842` · `12.5K` · `1.2M` · `3.4B` —
  ≤3 significant digits, one decimal for K/M/B; ladder values render
  bare (`500K`, `2M`).
- **Costs:** adaptive precision — `$123` (≥100) · `$12.34` (≥1) ·
  `$0.4821` (≥0.001) · `$0.000073` (below; enough digits for 2
  significant figures). Credits balance: always 2 decimals.
- **Durations:** `840ms` under 1 s, else `2.1s`, `1m 12s` over a
  minute.
- **Timestamps:** local wall clock `HH:MM:SS` + relative `(2m ago)`;
  relative text refreshed by the 15 s ticker. Ages: `now` under 30 s,
  then `2m`, `1h`.
- All width math via `lipgloss.Width()`, never `len()` (model slugs are
  ASCII today, but the rule is free to follow).

## 10. Implementation Stages

Suggested package shape (final judgment at build time): `tui/` is the
Go module; `internal/core` (pure: types, windows, aggregate, format,
bar geometry), `internal/data` (config, PostgREST, credits, LiveSource
+ both impls), `internal/ui` (model/update/view split, styles/tokens,
keymap, screens). Everything in `core` is testable with `go test` alone.

Each stage below opens with a **Goal** (what we're actually trying to
achieve, and why this stage exists as its own unit of work) and a
**Done when** (the observable, measurable bar that says the stage is
finished — the thing you check, not just a feeling). The checklist under
each is the *how*; the Goal and Done-when are the *what* and *why*, and
they ladder up to the Phase-2 definition of done at the very bottom.

### Stage A — Shell ✅ (2026-07-05)

**Goal:** Stand up a real, running terminal app you can look at and
click around — the whole four-region layout, at every screen size,
driven entirely by fake data — so the *shape* of the product is settled
and agreed before a single real number flows through it. Cheapest place
to get the visual and interaction design right is against fixtures.

**Done when:** `burnbar` launches on the alt screen, renders the full
layout at every breakpoint (and the too-small state), quits / suspends /
resizes cleanly, and both mouse and keyboard navigation reach the
details stub — all from fixture data.

- [x] Go module scaffold; Bubble Tea v2 program on the alt screen;
  clean quit (`q`/`ctrl+c`), suspend/resume, resize handling
  — suspend needs an explicit `ctrl+z → tea.Suspend` binding in v2
  (raw mode swallows the key); panic-restore verified empirically
- [x] Config loading + validation with the friendly pre-TUI error path;
  committed `config.example.toml`; `config.toml` gitignore guard
  — note: config path is `~/.config/burnbar/` on macOS too (deliberate;
  `os.UserConfigDir()` would give `~/Library/Application Support`)
- [x] Central keymap (`bubbles/key`) + help bubble hint row; `?` overlay
- [x] Full layout rendered from fake fixture data: header, bars list
  (label row + bar row), status row, hints — at every ladder breakpoint
  (§4), including the too-small state — verified via VHS screenshots at
  120×35 / 90×28 / 70×20 / 50×15 / 38×8 + scroll mode at 90×12
- [x] Semantic style tokens (§5) incl. `NO_COLOR`/ASCII degradation
- [x] Mouse augmentation: wheel scroll, click select, click-through to
  details, timeframe click — wired and hit-tested through the same
  layout math the view renders from; **needs a manual interactive pass**
  (VHS cannot send mouse events)

Stage A notes: format/bar-geometry helpers in `internal/core` are
provisional (written to §3/§9 spec, no tests yet) — Stage B formalizes
them with its test suite. A details-screen *stub* exists so the
enter/click-through navigation is real; Stage E replaces its body.

### Stage B — Pure core ✅ (2026-07-05)

**Goal:** Get the arithmetic provably right in complete isolation. Every
calculation the app will ever trust — dedupe, window cuts, aggregation,
ratios, bar geometry, formatting — nailed down and covered by tests
before any network or terminal I/O can muddy it. This is the brain of
the app; if it's wrong, nothing downstream can be right, and bugs here
are far cheaper to catch with `go test` than through the UI.

**Done when:** `go test ./internal/core` is green across the whole suite
(~160 cases), `go vet` / `go build` are clean, and every spec rule that
math depends on (§3 bars, §7 windows/aggregation, §9 formatting) has a
test that would fail if the rule broke.

- [x] Domain types; window math — local today, UTC week/month, both
  rollovers, DST edge cases — `rows.go` (RequestRow/DailyRow mirroring
  the SQL surfaces + RowStore dedupe map with live-wins merge: a live
  event upgrades a fetched row, a fetched row never overwrites) and
  `windows.go` (all pure over `(now, loc)`; DST verified incl. the
  Chile midnight-doesn't-exist case)
- [x] `aggregate()` over baseline + deduped rows with zero-guards,
  NULL-vs-0 handling, today-from-rows / week-month-from-baseline+live
  source selection — `Aggregate(AggregateInput)` (§7's four args plus
  the accent anchor and local zone the math needs); nullable sums
  mirror SQL SUM (nil only when every input was nil); ModelStat grew
  the details-screen sums + `Providers []ProviderStat` grain; ratio
  methods all return `*float64` (nil → `—`)
- [x] Bar geometry: 1–2–5 auto-range ladder (0.8 headroom, 10K floor),
  min-width clamp, cost-share segment split with token fallback,
  token-proportional accent slices — accent-slice math extracted from
  `ui/meter.go` into `core.Geometry() → BarGeometry`; render parity
  verified via VHS at 120×35; `ScaleFor` gained an int64-overflow cap
  (top ladder value 5e18 — the old walk hung on absurd inputs)
- [x] Formatting suite (§9) — tests caught a real FormatTokens bug:
  999,949 rendered "1000K" (4 digits); next-unit threshold corrected
  999.95 → 999.5 (999,500+ → "1M"). FormatAge 30–59 s now rounds up to
  "1m" (spec was silent; "0m" read as a bug)
- [x] `go test` for all of the above — 5 test files, ~160 cases, all
  green (`go vet` + `go build` clean; UI stays on Fixture() until
  Stage C swaps in Aggregate())

Stage B notes: accent2 = most recent live row by **ReceivedAt**
(arrival order — §5's long-running-request rationale), accent1 = live
rows received strictly after the anchor excluding the accent2 row;
accents exist only inside the active window (an out-of-window newest
row accents nothing — no fallback). `windows_test.go` imports
`time/tzdata` so zone tests are hermetic.

### Stage C — Data

**Goal:** Make the numbers real. Swap fixtures for your actual
OpenRouter usage: fetch the real baseline + today-slice, wire the tested
`Aggregate()` in place of `Fixture()`, prove the realtime pipeline
delivers a live request end-to-end, and decide *with evidence* whether
the realtime SDK can be the default `LiveSource`. This is the moment the
app stops being a mockup and starts metering the real world — and the
highest-risk unknown (does the community realtime SDK survive a
sleep/wake?) gets resolved up front. **Outcome:** `realtime-go` was a
no-go; poll ships as the interim default and the realtime path moves to
`phx` in Stage C.1 (see §7, root §6).

**Done when:** launching against your own Supabase project shows your
real per-model usage, and firing an OpenRouter request makes a new row
arrive in the app within a few seconds — surviving at least one
disconnect/reconnect and a laptop sleep/wake via re-baselining. The
realtime-go go/no-go outcome is recorded in root-spec §6, and the
credits burst-debounce timer is unit-tested against the §7 worked
example.

- [x] PostgREST baseline fetch (all `usage_daily` columns, 30 days) +
  today-slice fetch (raw `requests` since local midnight) —
  `internal/data/rest.go` + tagged DTOs (`dto.go`) mapping into core with
  NULL-vs-0 pointer discipline; `Fixture()` swapped for `Aggregate()` via
  `Model.rebuilt()`
- [x] Credits client + full cadence (§7: launch / refresh /
  burst-grouped post-event debounce / 5-min heartbeat) + always-on age
  display; unit-test the burst-grouping timer logic (the A/B/C example
  in §7 is the test case) — `credits.go` + pure `credits_sched.go`
  (id-tagged ticks tolerate un-cancelable `tea.Tick`); heartbeat is
  gen-guarded in the UI
- [x] **realtime-go spike against the live project — NO-GO** (2026-07-06).
  Live INSERT arrived, but the socket then closed with
  `StatusNormalClosure` and realtime-go v0.1.1 could not reconnect:
  `reconnect()` sets `isReconnecting=true` and calls `Connect()`, which
  rejects with `"client is already reconnecting"` when that flag is set —
  a self-deadlock that fails all 5 retries every time. It also logged to
  stderr and corrupted the alt-screen. **Default flipped to `poll`**;
  standard logger discarded in `main()`. Full verdict in root-spec §6.
- [x] `LiveSource` interface + realtime impl + 2 s polling impl +
  `live_source` config override — `live.go`/`poll.go`/`realtime.go`;
  factory falls back to poll for non-`*.supabase.co` hosts
- [x] Startup sequence wired (subscribe → baseline+slice ∥ credits),
  loading states, reconnect with re-baseline — `Init()` starts the feed;
  the first/every `LiveJoined` triggers baseline+slice (subscribe before
  fetch); `r` re-baselines and resets the accent anchor

Stage C notes: `internal/core` stayed network-free — I/O and JSON tags
live entirely in `internal/data`, mapping into the existing core types.
New core helper `SnapshotMeta`/`LatestLive` (`meta.go`) derives the
status-row last-request + meter-lag purely. Money `numeric` decodes to
float64 (root §6). Accent anchor is a simple app-start/refresh timestamp
for now — the rolling 5-min floor + focus-gain reset was Stage D **but is
now removed entirely** (2026-07-08 revision, §5/Stage D.2 — the whole
anchor concept is deleted in favor of the latest-burst highlight); the
springs, the 15s relative-time ticker, and the local-/UTC-midnight
rollover timers remain Stage D (window *math* already exists in core;
only the timers are deferred). Verified offline: `go build`/`vet`/`test` green (new tests
for the DTO NULL-vs-0 decode, the §7 credits-debounce example incl. both
guards, and `SnapshotMeta`), plus a VHS smoke run of the real binary
against a dead-endpoint poll config confirming the loading→reconnecting
render path and the credits/no-key hint. realtime-go v0.1.1 limitations
(broken channel rejoin, capped retries, optimistic subscribe) documented
in `realtime.go` and root §6 — the spike is the go/no-go.

### Stage C.1 — Realtime via phx

**Goal:** replace the broken `realtime-go` with a WebSocket path that
survives socket drops and laptop sleep/wake, so realtime can be the
default again — the low-latency, near-zero-egress source a leave-open
meter wants (root §6 no-go; §7). Polling stays only as a *manual* backup,
never a silent auto-fallback.

**Done when:** a live INSERT lights a bar within ~1–2 s; the socket
recovers on its own across a network blip and a sleep/wake (the exact
thing realtime-go failed); nothing the library logs ever touches the
alt-screen; and `p` toggles realtime ↔ poll live.

- [x] Swap `supabase-community/realtime-go` → `github.com/nshafer/phx`;
  rewrite `realtime.go`'s internals against it — the `LiveSource`
  interface, `poll.go`, and all UI wiring stay unchanged
- [x] Send the Supabase `phx_join` payload (`config.postgres_changes`
  INSERT on `public.requests` + `access_token`); decode INSERTs through
  the existing `requestDTO`; point phx's logger at the debug file, never
  stderr
- [x] Poll backup: flat **20 s** interval (down from 2 s) — it is the
  manual backup, not an auto-fallback
- [x] Add the `p` key to toggle the live source at runtime; reflect the
  active source in the status row + hint bar
- [x] Flip the default back to `realtime`; keep `poll` as the override
- [x] Verify live: a real INSERT, a network blip, and a sleep/wake;
  record the go/no-go in root §6

### Stage D — Live UX ✅ (2026-07-07) — accent/motion design revised 2026-07-08

> **Revision banner (2026-07-08):** Stage D shipped (springs, the accent
> system, the status row) and a review pass against the running app then
> reworked two of its design pillars. The **focus-anchor + accent1/accent2**
> highlight design below is **superseded by Stage D.2** (single
> latest-burst highlight, brighter-shade augmentation — §5), and the
> motion work gains **fractional-tip smoothing** (§6). The checklist items
> here are kept for history; D.1–D.3 are the live schedule. The status-row
> / ticker / rollover work stands unchanged.

**Goal:** Make it feel *alive on events and calm at rest* — the two
things a glanceable, always-open meter lives or dies by. A request
landing should be unmistakably *seen* (motion + accent highlight); an
idle app should burn zero frames, never flicker, and never accumulate
false "new" highlights while it sits unfocused beside your working
terminal. This is where the raw data from Stage C becomes a pleasant
companion you can leave open all day.

**Done when:** a landing request animates its bar toward the new target
and highlights the new slice (accent2, with its ~1 s emphasis); a scale
step reads as one collective zoom-out rather than a ripple; the status
row honestly reflects connection state, meter lag, and staleness; and at
rest the app provably renders at 0 fps (no tick loop, no CPU) between the
slow 15 s timestamp refreshes.

- [ ] Focus anchor: rolling 5-min `accentWindow` + focus-gain reset **on
  a ~1 s `accentClearDelay` debounce** (hold-then-clear, not instant —
  §5), anchor-independent accent2 (§5); verify v2 focus-report API; tune
  the window + delay constants by feel; tmux note in docs
- [ ] Harmonica springs on bar widths; scale-step collective retarget;
  conditional tick loop (idle = 0 fps); accent2 arrival emphasis +
  **minimum-visible 1-cell accent2 floor** for tiny requests (§5)
- [ ] Status row: last request, meter lag, scale chip, connection
  states; 15 s relative-time ticker; local-midnight + UTC-midnight
  rollover timers

---

**Stages D.1–D.4 (2026-07-08 review):** a post-Stage-D UI/UX pass, agreed
with the user over two rounds after living with the running app. Run
**in order** (D.1 → D.2 → D.3 → D.4): D.1 is safe, self-contained chrome
polish with no dependencies; D.2 is the visual-core bar rework (dual
cost/token modes + the latest-burst highlight + sub-cell smoothing) that
establishes the burst concept and the mode/scale generalization; D.3 adds
manual scale zoom on top of that scale generalization; D.4 is the modal
that reads the burst concept. They slot *before* Stage E (details
screen), which is unchanged.

### Stage D.1 — Chrome & layout polish

**Goal:** Fix the layout papercuts a review of the running app surfaced —
inconsistent whitespace and an unhelpful `…` on narrow terminals — so the
frame around the bars feels as deliberate as the bars themselves. Pure
layout/keymap work, no `core` math, so it is the safe warm-up.

**Done when:** the header has a spacer row between its two lines; the list
keeps a fixed one-row gap top and bottom in every density mode (spacer /
no-spacer / scroll) and as the window resizes; the hint row collapses by
priority (never to a bare `…`), always keeping `j/k select · ? help ·
q quit`; and the select hint reads `j/k` (arrows still work, unadvertised).

- [ ] Header: blank spacer row between wordmark/credits (row 1) and
  timeframe/spend (row 3) — header becomes 3 rows; `computeLayout`
  `listTop`/`listHeight` adjusted (§2/§4)
- [ ] Fixed list vertical rhythm: reserve one blank row top and bottom of
  the list region in all of spacer / no-spacer / scroll; scroll
  indicators render inside that frame; `visible` computed against the
  remaining height (§4) — the "first block touches the header" /
  "indicator touches the chrome" bug
- [ ] Priority hint-row collapse replacing `help.ShortHelpView`'s `…`
  truncation: protected core `j/k select · ? help · q quit`, then add
  `enter`/`t`/`r`/`p`/`i` by priority while they fit (§2); same for the
  details-screen hint variant
- [ ] Select hint label `j/k` (drop the `↑/↓` glyph dependency); arrows
  stay bound and undocumented (§2)

### Stage D.2 — Bar rework: dual modes, single highlight, sub-cell smoothing

**Goal:** Rebuild the bar into two coherent, single-denomination display
modes (cost / tokens) toggled by `m`; replace the confusing multi-hue
accent scheme with one legible latest-burst highlight drawn in brighter
shades of the bar's own two colors (in the active mode's unit); delete the
whole focus-anchor subsystem it replaces; and beat the choppy animation
with fractional-block sub-cell resolution. This is the visual heart of the
review and the highest-risk change (it touches the most-tested `core`
math), so it stands alone. It also **generalizes the scale/geometry over a
`value` (cost or tokens)**, which Stage D.3's manual zoom then builds on.

**Done when:** `m` toggles cost↔token mode, re-rendering length, split,
sort, header unit, and scale-chip unit coherently (§3); a landing burst
resizes both the input and output sides of the bar and paints the added
cells in `accent.primary.bright` / `bar.output.bright` in the active unit
(with the ≥1-cell floor + ~1 s bold pulse for tiny bursts); the highlight
persists until a newer burst supersedes it or a refresh clears it;
`accent1` / `anchor` / focus + tmux `focus-events` are gone from the code;
the bar's leading tip animates in eighth-cell steps via fractional glyphs
(ASCII/`NO_COLOR` cell-quantized); and `go test ./internal/core` is green
with the accent tests rewritten to the burst model and new per-mode
geometry tests.

- [ ] `core`: generalize `ScaleFor`/`BarWidth`/`Geometry`/`SplitFraction`
  over a `value` (float64) + a per-mode 1–2–5 ladder (token floor 10K;
  dollar floor tuned by feel); a `Mode` selector picks cost vs token
  values, the split source, and the sort key (§3)
- [ ] `core`: `latestBurst(rows)` coalescing (§7, `burstGap`), returning
  summed input/output **tokens and cost** + count + span; delete
  `Accent1Tokens`/`Anchor` and the accent1 math; retarget the accent unit
  tests onto the burst
- [ ] Bar geometry: two brighter-shade highlight regions (Δinput at the
  input-segment trailing edge, Δoutput at the tail), proportional in the
  active mode's unit, clamped within each segment, ≥1-cell minimum-visible
  floor borrowed from the segment to the left (§5)
- [ ] Delete focus/blur handling, `accentWindow`, `accentClearDelay`, the
  v2 focus-report wiring, and the tmux `focus-events` doc note (§5)
- [ ] UI: `m` toggle (cost default); mode-aware sort, header unit
  (`spent $` / `used <tokens>`), and scale-chip unit (`scale $5` /
  `scale 2M`) — §2/§3
- [ ] `meter.go` `renderBarRow`: draw base + bright regions in the bright
  ANSI pair; ~1 s bold arrival pulse on the bright region (§6)
- [ ] Fractional-block leading tip (`▏▎▍▌▋▊▉█`) in the tip color for
  sub-cell smoothness; ASCII/`NO_COLOR` fallback stays cell-quantized (§6)
- [ ] Styles: add `accent.primary.bright` / `bar.output.bright`; remove
  `accent.session` / `accent.latest`; `NO_COLOR`/monochrome parity intact

### Stage D.3 — Manual scale zoom

**Goal:** Solve the long-tail "stub" problem *honestly* — without a
nonlinear scale (which would break exact proportionality and fight the
animation, §3) — by letting the user step the same linear 1–2–5 ladder by
hand to zoom in on the small bars or out on the big ones. Builds directly
on D.2's generalized, mode-aware scale.

**Done when:** `-`/`+` step the active mode's scale ladder out/in and `0`
resets to auto; zooming retargets every bar through the §6 collective
animation; bars past the manual `S` clamp to full width; the scale chip
shows a `·manual` marker while pinned; and the manual scale resets to auto
on refresh (`r`), window switch (`t`), and mode toggle (`m`).

- [ ] Manual `S` as transient view state overriding the auto scale until
  reset; auto does not re-engage while it holds (§3)
- [ ] `-`/`+`(`=`)/`0` bindings + hint-row/`?`-help entries (§2, keys
  provisional); `+`/`=` share the zoom-in action
- [ ] Reset-to-auto on `r`/`t`/`m`; `·manual` marker on the status scale
  chip (§2/§3)

### Stage D.4 — Recent-request modal

**Goal:** Give the live meter the one thing a bar can't show — the actual
detail of the thing that just happened — via an on-demand overlay over the
most recent burst, so a curious glance ("what did that cost?") is one
keypress away without cluttering the calm main screen.

**Done when:** `i` toggles a centered overlay (help-overlay chrome) showing
the latest burst's model/provider, coalesced request count + span,
input/output/cached/reasoning tokens, cost with split, wall time + age,
and meter lag — `—` for NULL facts, a friendly empty line before any live
request; it updates live and closes on `i`/`esc`.

- [ ] `core`: expose the `latestBurst` detail object (reuses D.2's
  coalescing) with the modal's fields
- [ ] Modal overlay reusing the `renderHelpOverlay` pattern; `i` binding +
  hint-row/`?`-help entries (key provisional — §2)
- [ ] Live update in place; empty state before the first live request

### Stage E — Details screen

**Goal:** Answer the follow-up question the main screen deliberately
raises — "where *exactly* is this model's money going?" — with the full
per-model breakdown the raw rows already carry: cache hit rate, reasoning
share, effective $/1M rates, average duration, and the provider split.
The main screen stays a calm summary; this is where the curious user
drills in without cluttering it.

**Done when:** pressing enter (or click-through) on any model opens its
details screen, every §2 stat renders correctly with `—` for
unreported/NULL-derived values (never 0), the provider split table is
right and cost-sorted, and live events update an open details screen in
place.

- [ ] Drill-down navigation (selection ↔ details, live updates in place)
- [ ] Stats grid + provider split table per §2, `—` for NULL-derived
  values

### Stage E.1 — Live theme picker (ANSI-16 remap)

**Goal:** Fix the one place the ANSI-16-only choice (§5) bites. The app
maps each semantic token to a *fixed* ANSI slot (`accent.primary→cyan`,
`bar.output→blue`, plus their bright-variant highlight pair
`accent.primary.bright→bright cyan`, `bar.output.bright→bright blue`),
and in some terminal themes two of those slots are near-identical — the
input/output segments, or a base color and its bright variant, collapse
into each other and the app's core distinctions stop reading. Let the
user re-slot which of *their theme's* 16 colors each token grabs, seeing
the change live, and persist it. This buys back the lost distinctions
**without spending the theme-native inheritance** that makes ANSI-16 the
right default in the first place. Sequenced after the details screen
(Stage E) — it depends on the finished highlight system (Stage D.2) to
preview the bar and its bright regions meaningfully.

**Note (post-2026-07-08 revision):** the token list this picker edits is
now **smaller** — the retired `accent.session` (yellow) and
`accent.latest` (magenta) slots are gone, replaced by the two
bright-variant highlight tokens above (§5). So the picker covers the
input/output base pair and their bright pair, not four independent hues.

**Scope guard — remap within ANSI-16 only, never to hex.** A token may
be pointed at a different one of the 16 ANSI colors; it may **not** be
set to a 256/truecolor hex value. Hex would let the app clash with the
terminal theme instead of inheriting it — the exact pitfall §5 exists to
avoid. Truecolor theming is explicitly out of scope (a possible
far-later, opt-in escalation with ANSI-16 staying the default — not now).

**Done when:** a `theme`/`c` binding opens a picker screen listing the
semantic tokens (§5) with each one's current ANSI color; changing a
token's color updates a **live sample of the real meter** (a rendered
bar with real input/output segments + both bright highlight regions, not
abstract swatches) in the
same frame; `s` saves to config and `esc` cancels (reverting the
in-memory palette); and relaunching picks up the saved palette. Removing
color entirely (`NO_COLOR`) still yields a fully readable app — the
picker is enhancement, never load-bearing (§5).

- [ ] Theme-editor screen (token list + live meter sample); edit-in-place
  → live in-memory apply → `s` persist / `esc` revert
- [ ] `[colors]` table in `config.toml` (token → ANSI color name) with the
  existing env-override precedence (§7); missing/partial table falls back
  to the §5 default slot per-token; unknown color names rejected with a
  hint
- [ ] Preview renders through the same style tokens the real view uses
  (one palette source of truth — no divergent editor styling)
- [ ] `NO_COLOR`/monochrome parity preserved; ASCII glyph fallbacks
  untouched

### Stage F — Hardening & polish

**Goal:** Make it robust and documented enough to hand to a stranger who
will self-host it unaided. Every designed-for failure degrades *visibly*
instead of crashing, the main screens are locked down against regression
by golden tests, and the docs carry someone from `git clone` to live
bars. This is the difference between "works on my machine" and
"shippable open-source tool".

**Done when:** the full §8 failure table has been walked against the live
backend (killed network, paused project, bad keys) with every row
behaving exactly as specified, golden-render tests pass for the
main-screen states (color profile pinned), the VHS smoke shots render,
and the config / debug-logging docs are written. (The tmux
`focus-events` note is dropped — the focus system it documented was
removed in the 2026-07-08 revision, §5/Stage D.2.)

- [ ] Walk the full §8 failure table against the live backend (kill
  network, pause project, bad keys)
- [ ] Golden-render tests via teatest/v2 (`WithInitialTermSize(80,24)`,
  ASCII color profile pinned — unpinned profiles are the #1 golden
  flake) for the main screen states
- [ ] VHS screenshot smoke (the `vhs-cli-demos` skill) — doubles as
  README material later
- [ ] README/docs: config reference, debug logging (no tmux
  focus-events note — focus reporting removed, §5/Stage D.2)

**Definition of done (= root-spec Phase 2):** run `burnbar`, fire an
OpenRouter request, watch the bar animate within seconds — through at
least one disconnect/reconnect without restarting the app.

## 11. Sign-off

All design decisions confirmed with the user on 2026-07-05 (third
pass): unconditional rolling anchor + independent accent2 (§5),
burst-grouped credits debounce (§7), local today with UTC-pure
week/month (§7). No open questions — the spec is build-ready.
Anything discovered during implementation that contradicts it goes back
to the user before code deviates.

**2026-07-07 additions (pre-Stage-D):** two Stage-D accent refinements —
the minimum-visible 1-cell accent2 floor + ~1 s bold emphasis for tiny
requests (§3/§5/§6), and focus-gain accent clearing on a ~1 s
`accentClearDelay` debounce rather than instantly (§5) — plus a new
**Stage E.1 live theme picker** (ANSI-16 re-slotting only, live preview,
`[colors]` config; §5/§7/§10), sequenced after the details screen. All
agreed with the user; both accent refinements are explicitly "tune by
feel later" once the app has seen real use.

**2026-07-08 review (post-Stage-D, against the running app):** a UI/UX
pass agreed with the user over two rounds, scheduled as **Stages
D.1–D.4** (§10). Round-one decisions:

1. **Latest-burst highlight replaces the accent1/accent2 + focus system**
   (§5). One highlight, drawn as brighter shades of the bar's own two
   colors (Δinput at the input-segment edge, Δoutput at the tail); the
   whole rolling-anchor + focus/blur subsystem is deleted. The 2026-07-05
   "rolling anchor + independent accent2" decision and the 2026-07-07
   `accentClearDelay` refinement above are **superseded** by this.
2. **Requests coalesce into a display-only burst** (~`burstGap` ≈ 3 s,
   same model) feeding both the highlight and the recent-request modal
   (§7). Backend prompt-id grouping was researched and rejected (no
   conversation id in OpenRouter's stateless Broadcast payload). Never
   touches authoritative totals.
3. **Fractional-block sub-cell smoothing** for the bar's leading tip (§6)
   — the choppiness is spatial quantization, not FPS; eighth-cell glyphs
   give 8× resolution, with ASCII/`NO_COLOR` staying cell-quantized.
4. **Chrome polish** (§2/§4): header spacer row, fixed list vertical
   rhythm across all density modes, priority-based hint collapse (protect
   `j/k select · ? help · q quit`), `j/k` as the advertised select label.

Plus a **recent-request modal** (`i`, provisional key) over the latest
burst (§2). `burstGap` and the fractional-tip feel are "tune by feel."

Round-two decisions (which reshaped the bar denomination question rather
than answering it):

5. **Dual display modes instead of one denomination** (§3, `m` toggle).
   The earlier "length=tokens, split=cost" hybrid was incoherent (a cell
   meant neither); rather than picking cost *or* tokens, the app ships
   **two single-denomination modes** — cost (default) and tokens — each
   coherent end to end (length, split, sort, highlight, header/scale
   units), with the label row always showing the other denomination's
   numbers. This supersedes §3's original "two decoupled axes" framing.
6. **Manual scale zoom** (§3, `-`/`+`/`0`) as the honest fix for the
   long-tail stub problem — stepping the same linear 1–2–5 ladder by hand
   beats a nonlinear scale, which was rejected for breaking exact
   proportionality *and* fighting the animation. Zoom is transient view
   state (resets to auto on `r`/`t`/`m`); over-scale bars clamp to full
   width.

Judgment calls made under the user's "go ahead if reasonable" (all
overridable on review): **sort follows the active mode** (vs a stable
always-by-cost sort); keybinds `m` (mode), `-`/`+`/`0` (zoom), `i`
(modal); **mode signaled by units** (header `spent $`/`used <tokens>`,
scale chip `$`/tokens) rather than extra chrome; **free/$0 models get a
1-cell floor** in cost mode. The provisional keys are the open items
flagged for this spec's review.
