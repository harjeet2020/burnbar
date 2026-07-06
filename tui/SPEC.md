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
 ████████████▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▓▓▓██

 anthropic/claude-haiku-4.5    210.4K in · 88.1K out $0.3110
 ████▒▒▒▒▒▒

 qwen/qwen3-coder               955.0K in · 82.3K out $0.0214
 █████████████████▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒▒

 last request 12:04:32 (2m ago) · lag 2.1s · scale 2M · ● live
 ↑/↓ select · enter details · t window · r refresh · ? help · q quit
```

Four fixed regions, top to bottom — **regions never move or reorder**:

1. **Header (2 rows):** wordmark left (`▂▄▆ burnbar` — the glyph is a
   tiny bar-chart motif; drop the glyph in ASCII fallback). Right-aligned
   column: credits balance **with its age** (row 1) and total spend for
   the active window (row 2). Row 2 left: the timeframe selector — three
   labels with the active one highlighted (reverse video), the others
   muted; **`t` cycles** today → week → month → today.
2. **Bars list (fills remaining height):** one block per model used in
   the window, **sorted by window cost desc** (ties: name asc — stable
   order, no jitter). Each block = label row + bar row (+ one blank
   spacer row when height allows; drop spacers first when tight).
   Scrolls inside a viewport when models exceed available height, with
   `▼ 3 more` / `▲ 2 more` edge indicators.
3. **Status row:** last-request wall time + relative age, meter lag
   (§7), the current bar scale (`scale 2M`, muted — see §3), connection
   state: `● live` (realtime healthy), `◌ polling` (fallback active),
   `○ reconnecting…` (with retry countdown), `✗ offline`. Symbols +
   words, never color alone.
4. **Hint row:** the core bindings rendered by the `help` bubble from
   the central keymap. `?` toggles the expanded help overlay.

**Keymap:** `t` cycle timeframe · `↑/↓` or `j/k` select · `enter`/`l`
details · `esc`/`h` back · `r` refresh · `?` help · `q`/`ctrl+c` quit.
Single-key timeframe cycling replaces the earlier `d`/`w`/`m` bindings —
one key to remember, three states, and the header always shows where
you are.

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

## 3. The Bars

Two axes carry two different truths, deliberately decoupled:

- **Vertical position = money.** Bars are sorted by window **cost**
  desc — the list order answers "where is my money going".
- **Bar length = volume.** Length is proportional to the model's
  **total tokens** (input + output) — the fill answers "what am I
  actually using". An expensive model used sporadically sits high in
  the list with a short bar; a budget workhorse sits lower with a long
  bar. That divergence is signal, not noise.

**Auto-ranging scale (no bar is ever pinned at 100%).** All bars share
one scale `S` = the full-content-width token value, drawn from the
1–2–5 ladder (`10K, 20K, 50K, 100K, 200K, 500K, 1M, 2M, 5M, …`; floor
10K). `S` is the smallest ladder value such that `tokens_max ≤ 0.8·S`,
recomputed as a pure function of the current window's data — no state,
no hysteresis. Bar width = `W · tokens_i / S`, minimum 1 cell for any
nonzero model.

The UX this produces: early in a day the first requests visibly fill
the top bar; when it crosses 80% the scale steps up and **all bars
shrink together** (a springs-animated "zoom out"), after which each
request advances the bar by proportionally less — sustained usage reads
as repeated zoom-outs, exactly the "thresholds that scale with usage"
feel. Because every bar shares `S`, proportions *between* bars stay
mathematically exact at all times. The current scale is always shown
muted in the status row (`scale 2M`) so absolute lengths stay
interpretable; window switches recompute `S` instantly.

**Inside a bar — split by cost shares, not token counts.** The boundary
between the input segment (full block `█`) and output segment (medium
shade `▒`, same hue) sits at `input_cost / (input_cost + output_cost)`
using the window's summed **actual split costs**. This shows at a
glance what fraction of the model's spend went to input vs output —
far more truthful than token-volume proportions, because output tokens
cost several times more and cache discounts shrink effective input
cost. The reported split costs already embody cache discounts (that's
why the schema stores facts, not `unit_price × tokens` recomputation),
so cached-token handling is automatic and exact. Fallbacks, in order:
if split costs are unreported (NULL sums) or both zero (free models),
fall back to the token-volume split; glyph difference keeps the split
readable in monochrome either way.

**Label row** (above each bar): model name left (the alias slug, e.g.
`deepseek/deepseek-v4-flash`); tokens center-right, muted:
`1.2M in · 340.2K out`; cost right-aligned, bold. The cost column is the
row's anchor — right-aligned costs down the screen read as a column,
mirroring the cost-ranked ordering.

## 4. Responsive Behavior

Layout derives from the cached `WindowSizeMsg` on every resize — no
fixed dimensions anywhere. Resizing **snaps** (no animation): springs
animate data changes, but a resize must feel like the terminal is in
charge. Ladder, by columns:

- **≥110:** full labels, verbose tokens, spacer rows between models.
- **80–109:** spacers dropped when height demands, tokens compact
  (`1.2M→340K`).
- **60–79:** model names middle-truncated keeping the model part
  (`…/deepseek-v4-flash`), tokens dropped from the label row (cost
  stays — the details screen has the tokens).
- **40–59:** name + cost + bar only.
- **Below 40×10:** centered `terminal too small (min 40×10)` message —
  never a crash or a mangled layout.

Height pressure drops, in order: spacer rows → status row merges into
hint row → list scrolls. Header and hint row never disappear.

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
| `bar.output` | blue | output segment (plus the `▒` glyph difference) |
| `accent.session` | yellow | accent1 slices — usage since anchor |
| `accent.latest` | magenta | accent2 slice — the most recent request |
| `text.primary` | default fg | names, values |
| `text.muted` | bright black | token counts, timestamps, inactive labels, hints, scale |
| `status.ok` / `status.warn` / `status.error` | green / yellow / red | connection dot, error banner |

Honor `NO_COLOR` and dumb terminals: monochrome must remain fully
readable (segments by glyph, selection by `▸` + bold, status by symbol
+ word).

**Accent semantics — a rolling anchor, and an independent accent2.**

- **accent2 is anchor-independent:** the single most recent live row
  (§7) always renders as the accent2 slice, until superseded by a newer
  one or cleared by refresh. Rationale: after any absence, the one
  thing worth seeing is the impact of the last request (e.g. a
  long-running request that resolved while you were away) — and it
  should be visible regardless of any staleness window.
- **accent1 = live rows newer than `anchor`**, where
  `anchor = max(lastFocusGainOrAppStart, now − 5 min)` —
  **unconditionally**. The 5-minute floor makes accent1 a rolling
  "recent activity" window. No blur exception: keyboard focus is the
  only thing terminals can report — there is no "visible in the
  viewport" signal — and the meter's primary usage pattern is sitting
  *visible but never focused* beside the working terminal, where a
  blur-frozen anchor would accumulate highlights forever.
- **Focus-gain still resets the anchor to now** when the event is
  available — clicking into the meter means "seen it", clearing
  accent1 instantly.

Slice widths follow the same token-proportional math as bar lengths.
The 5-minute window is a named constant (`accentWindow`), tuned by feel
in Stage D — 2–3 min is twitchy across agent think-pauses, 10 min keeps
stale work looking new. Focus reporting is xterm mode 1004, surfaced as
focus/blur messages in Bubble Tea (v1: `tea.WithReportFocus()`; confirm
the v2 equivalent — likely declared on the view); tmux needs
`set -g focus-events on` (document it). With no focus events at all,
the rolling window alone governs — same behavior, no configuration.

Accents live only on live rows, so a manual refresh (which clears them,
§7) also clears accents. Accepted: refresh means "re-baseline my view
of the world".

## 6. Motion

Harmonica springs, one per bar, animating **bar width** toward its
target whenever data changes (new event, scale step, window switch,
refresh). Segment and accent boundaries are computed as fractions of
the animated width each frame — no separate springs. A scale step (§3)
retargets every spring at once — the collective zoom-out is the app's
most satisfying moment; make sure springs are tuned so it reads as one
motion, not a ripple. New model appearing: grows from 0. Model leaving
the window: shrinks to 0, then the row is removed (rows don't animate
vertically; reorders are discrete).

**Performance rule — idle means idle:** the app renders at 0 fps until
a message arrives. A tick loop (~30 fps) runs **only while any spring
is unsettled**, then stops. A slow 15 s ticker refreshes relative
timestamps and the credits age — the only steady-state wakeup. No
animation on resize (snap, §4) or on the details screen (values just
update).

The accent2 arrival moment gets a brief emphasis: the new slice renders
bold for the first ~1 s (one timed message, not a fade — ANSI-16 has no
alpha).

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
  and carry `receivedAt` — only they participate in accents (§5) and
  meter lag.
- **`anchor`** — the accent timestamp (§5).

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

### Realtime, and the pre-approved fallback

Primary: `supabase-community/realtime-go` — Phoenix-channel WebSocket to
`wss://<ref>.supabase.co/realtime/v1/websocket`, `postgres_changes`
INSERT on `public.requests`, heartbeats, and reconnect with exponential
backoff + jitter (1 s → 30 s cap). **On every successful (re)join:
refetch baseline + today-slice and clear live rows** — events missed
while disconnected are healed by re-baselining, so gap tracking is
never needed.

The **fallback is insurance against realtime-go itself** — it is a
community-maintained SDK, not an official Supabase client, so the root
spec pre-approved a plan B: poll PostgREST
(`requests?inserted_at=gt.<last>`) every 2 s — same rows, same latency
budget, just chattier. Both live behind a small **`LiveSource`
interface** (emits request-row events + connection-state changes), so
swapping is a one-line default change plus a config override
(`live_source = "realtime" | "poll"`, default realtime) as an escape
hatch for users whose networks fight WebSockets. Stage C starts with a
spike: subscribe with realtime-go against the live project, receive a
real INSERT, survive a laptop sleep/wake. Go/no-go decides the default;
**record the outcome in root-spec §6 either way.** The `◌ polling`
status state (§2) keeps the active source honest on screen.

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
| `live_source` | `BURNBAR_LIVE_SOURCE` | no — `realtime` (default) or `poll` |

Missing-config error message includes a ready-to-paste TOML template.
The repo `.env` maps onto the env overrides for dev convenience.

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

### Stage A — Shell ✅ (2026-07-05)
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
- [ ] PostgREST baseline fetch (all `usage_daily` columns, 30 days) +
  today-slice fetch (raw `requests` since local midnight)
- [ ] Credits client + full cadence (§7: launch / refresh /
  burst-grouped post-event debounce / 5-min heartbeat) + always-on age
  display; unit-test the burst-grouping timer logic (the A/B/C example
  in §7 is the test case)
- [ ] **realtime-go spike against the live project** — subscribe,
  receive a real INSERT, survive sleep/wake; go/no-go decides the
  default `LiveSource` (record in root-spec §6)
- [ ] `LiveSource` interface + realtime impl + 2 s polling impl +
  `live_source` config override
- [ ] Startup sequence wired (subscribe → baseline+slice ∥ credits),
  loading states, reconnect with re-baseline

### Stage D — Live UX
- [ ] Focus anchor: rolling 5-min `accentWindow` + focus-gain reset,
  anchor-independent accent2 (§5); verify v2 focus-report API; tune the
  window constant by feel; tmux note in docs
- [ ] Harmonica springs on bar widths; scale-step collective retarget;
  conditional tick loop (idle = 0 fps); accent2 arrival emphasis
- [ ] Status row: last request, meter lag, scale chip, connection
  states; 15 s relative-time ticker; local-midnight + UTC-midnight
  rollover timers

### Stage E — Details screen
- [ ] Drill-down navigation (selection ↔ details, live updates in place)
- [ ] Stats grid + provider split table per §2, `—` for NULL-derived
  values

### Stage F — Hardening & polish
- [ ] Walk the full §8 failure table against the live backend (kill
  network, pause project, bad keys)
- [ ] Golden-render tests via teatest/v2 (`WithInitialTermSize(80,24)`,
  ASCII color profile pinned — unpinned profiles are the #1 golden
  flake) for the main screen states
- [ ] VHS screenshot smoke (the `vhs-cli-demos` skill) — doubles as
  README material later
- [ ] README/docs: config reference, tmux focus-events note, debug
  logging

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
