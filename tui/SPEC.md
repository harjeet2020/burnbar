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
   state: `● live` (realtime healthy), `◌ polling` (poll mode, chosen
   via `p`), `○ reconnecting…` (with retry countdown), `✗ offline`.
   Symbols + words, never color alone.
4. **Hint row:** the core bindings rendered by the `help` bubble from
   the central keymap. `?` toggles the expanded help overlay.

**Keymap:** `t` cycle timeframe · `↑/↓` or `j/k` select · `enter`/`l`
details · `esc`/`h` back · `r` refresh · `p` toggle live source
(realtime ↔ poll) · `?` help · `q`/`ctrl+c` quit. Single-key timeframe
cycling replaces the earlier `d`/`w`/`m` bindings — one key to remember,
three states, and the header always shows where you are.

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
- **Focus-gain resets the anchor to now** when the event is available —
  clicking into the meter means "seen it" — **but on a ~1 s debounce, not
  instantly**: the accents hold for `accentClearDelay` (~1 s, a named
  constant tuned by feel) after focus-gain so a glance still catches what
  changed, *then* clear. ANSI-16 has no alpha, so this is a timed
  hold-then-clear (like the accent2 emphasis in §6), not a true fade.

Slice widths follow the same token-proportional math as bar lengths,
**with one floor: a live accent2 slice always renders at least 1 cell
wide** regardless of its token proportion — the minimum-visible-event
rule (§3's "min 1 cell" for bar width, applied here to the highlight).
Its purpose is the tiny-request case: a request small enough to move the
bar by a sub-cell fraction (a few hundred tokens against a `scale 2M`
bar) would otherwise paint a zero-width highlight and land invisibly,
which for a "a request landing is *seen*" meter (§1) is a failure. The
1-cell floor plus the ~1 s bold emphasis (§6) together guarantee every
captured request is visibly seen; the floor borrows its cell from the
segment it sits in, so between-bar bar *lengths* stay mathematically
exact.
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
alpha). This ~1 s emphasis is also the *primary* arrival signal for tiny
requests (§5's minimum-visible 1-cell accent2 floor): when a landing
request is too small to lengthen the bar, the single accent2 cell
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
for now — the rolling 5-min floor + focus-gain reset is Stage D, as are
springs, the 15s relative-time ticker, and the local-/UTC-midnight
rollover timers (window *math* already exists in core; only the timers
are deferred). Verified offline: `go build`/`vet`/`test` green (new tests
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

### Stage D — Live UX

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
`bar.output→blue`, `accent.session→yellow`, `accent.latest→magenta`),
and in some terminal themes two of those slots are near-identical — the
input/output segments or the two accents collapse into each other and
the app's core distinctions stop reading. Let the user re-slot which of
*their theme's* 16 colors each token grabs, seeing the change live, and
persist it. This buys back the lost distinctions **without spending the
theme-native inheritance** that makes ANSI-16 the right default in the
first place. Sequenced after the details screen (Stage E) — it depends on
the finished accent system (Stage D) to preview accents meaningfully.

**Scope guard — remap within ANSI-16 only, never to hex.** A token may
be pointed at a different one of the 16 ANSI colors; it may **not** be
set to a 256/truecolor hex value. Hex would let the app clash with the
terminal theme instead of inheriting it — the exact pitfall §5 exists to
avoid. Truecolor theming is explicitly out of scope (a possible
far-later, opt-in escalation with ANSI-16 staying the default — not now).

**Done when:** a `theme`/`c` binding opens a picker screen listing the
semantic tokens (§5) with each one's current ANSI color; changing a
token's color updates a **live sample of the real meter** (a rendered
bar with real segments + both accents, not abstract swatches) in the
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
and the config / tmux focus-events / debug-logging docs are written.

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

**2026-07-07 additions (pre-Stage-D):** two Stage-D accent refinements —
the minimum-visible 1-cell accent2 floor + ~1 s bold emphasis for tiny
requests (§3/§5/§6), and focus-gain accent clearing on a ~1 s
`accentClearDelay` debounce rather than instantly (§5) — plus a new
**Stage E.1 live theme picker** (ANSI-16 re-slotting only, live preview,
`[colors]` config; §5/§7/§10), sequenced after the details screen. All
agreed with the user; both accent refinements are explicitly "tune by
feel later" once the app has seen real use.
