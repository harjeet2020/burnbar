# Burnbar TUI

This document describes the Go terminal frontend as it exists today: what
it looks like, how it behaves, and why it's built the way it is. It used
to be the implementation runbook for building this app; now that the app
is finished, its job is to explain it — to someone cloning the repo who
wants to understand the TUI before reading code, and as a design
reference for building other frontends (a native macOS menubar app is
next) against the same [backend contract](../ARCHITECTURE.md).

Where a paragraph says "see `file.go`," that file is the authoritative
detail — this document explains intent and behavior, not line-by-line
mechanics. Section numbers below are stable and match the `tui/SPEC.md
§N` references sprinkled through the Go source comments.

## 1. Product Shape

Burnbar's TUI is a long-running, full-screen terminal app that lives in
a corner of a terminal all day — most likely right next to whatever
editor or agent session is actually burning the tokens. It runs on the
alternate screen, has one primary screen (the meter) and three
drill-downs (model details, a recent-request popup, a live theme
picker), and never asks to be the center of attention. Everything about
its design follows from that one placement decision: it must be calm at
rest (no flicker, no motion for motion's sake), alive the instant
something happens (a request landing is *felt*, not just eventually
reflected), and always honest about its own state — connection health,
data staleness, and lag are visible at a glance, never hidden behind a
silently-stale number.

It's built on the Charm stack: Bubble Tea v2 for the Elm-style
model/update/view loop, Lip Gloss v2 for styling, Bubbles for help/key
handling, and Harmonica for the spring physics behind every animation.
`go.mod` pins the exact versions; `main.go` is the entry point.

## 2. Screen Anatomy

The main screen is four fixed regions, top to bottom, that never move,
reorder, or disappear — only the middle region's *content* responds to
terminal size (`internal/ui/layout.go`):

1. **Header** (three rows): the wordmark on the left; the credits
   balance and its age on the right (`$12.43 · 2m`); a blank spacer;
   then the timeframe selector (`today` / `week` / `month`, cycled with
   `t`, the active one in reverse video) and the active window's total
   in the current display mode's unit.
2. **Bars list**: one block per model used in the active window, sorted
   by the active mode's metric descending, scrolling inside a viewport
   with `▼ N more` / `▲ N more` indicators when the models overflow the
   available height. A block is a label row (model name, both
   denominations) plus a bar row.
3. **Status row**: the wall time and relative age of the last request in
   the window, the meter's lag (how long between a request finishing and
   the bar reflecting it), the current bar scale (muted, doubles as the
   mode indicator), and the connection state — `● live`, `◌ polling`,
   `○ reconnecting… (Ns)`, or `✗ offline` — always rendered as symbol
   *and* word, never color alone.
4. **Hint row**: the live keymap, collapsing under width pressure by
   priority rather than the naive `…` truncation a stock help bubble
   would apply (§4, `internal/ui/keymap.go`).

The full keymap: `t` cycles the timeframe, `m` toggles cost/token
display mode, `j/k` (arrows work too, undocumented) move the selection,
`enter`/`l` opens model details, `esc`/`h` backs out, `-`/`+`/`0` zoom
the bar scale out/in/reset, `i` opens the recent-request popup, `r`
forces a refresh, `p` toggles the live data source, `c` opens the color
picker, `?` opens the full help overlay, `q`/`ctrl+c` quits. Mouse input
is a pure augmentation on top — wheel scroll, click-to-select,
click-through to details, clicking a timeframe — every action has a
keyboard path and none require the mouse.

**Model details** (`enter`/`l`, `internal/ui/details.go`) replaces the
bars region with a full drill-down for the selected model over the
active window: a stats grid (requests, average duration, cache-hit
percentage, reasoning share, then cost/tokens/average-cost-per-request/
average-tokens-per-request/effective rate per million, each split into
total·input·output) and a provider-split table sorted by cost, showing
which actual provider (and quantization variant) served the requests
and at what share. The whole block renders at its natural width (capped
and centered, never stretched edge to edge) and both the grid and the
table degrade gracefully as width shrinks — the provider table sheds
columns one at a time before falling back to a stacked per-provider
list on very narrow terminals. `—` stands in for anything the payload
never reported; it is never confused with a real zero. Selection
follows the model across re-sorts, and the screen stays live.

**The recent-request popup** (`i`, `internal/ui/modal.go`) is a
centered overlay answering the question a live meter raises but the
bars alone can't: "what did the last thing that just happened actually
cost?" It shows the most recent *burst* (§7) — model, routed provider,
request count and time span if more than one call landed together,
token breakdown, cost split, wall time, and meter lag. It updates live
and reads the same coalesced burst that drives the bar highlight (§3);
it never feeds the authoritative totals. `i` or `esc` closes it.

**The color picker** (`c`, `internal/ui/theme.go`) lets you re-slot
which of your terminal's 16 ANSI colors each semantic token uses (§5),
previewed live against a real rendered bar before you commit. `h`/`l`
cycle a row's color, `space` toggles the bright variant, `j`/`k` move
between rows, `d` resets a row to its default, `s` saves to
`config.toml`, `esc` discards the draft and reverts.

## 3. The Bars

Every model gets one bar, and the app never mixes money and volume in
the same bar — instead it has two full display modes, toggled with `m`,
each **single-denomination end to end**: length, interior split, sort
order, and the burst highlight all speak one unit, so `m` swaps between
two internally-coherent views rather than blending them.

- **Cost mode** (default, since Burnbar is fundamentally a money meter):
  bar length is proportional to window cost; the interior splits into
  input-cost and output-cost; the highlight shows the latest burst's
  cost split. Answers "how fast is my money going, and on what."
- **Token mode**: length is proportional to total tokens; the split and
  highlight follow token volume instead. Answers "how much am I
  actually using."

Sort follows the active mode (cost or total-tokens, descending, ties
broken by name) so the longest bar always sits on top regardless of
mode; the label row always shows *both* denominations so switching
modes never hides information, just re-anchors which one is bold.

**Scale is automatic and shared.** All bars share one scale, drawn from
a 1–2–5 ladder in the active mode's unit (`$0.001, $0.002, $0.005, …` or
`10K, 20K, 50K, …`) — the smallest ladder value under which the largest
bar sits at 80% or less. Every bar's width is a linear function of that
one shared scale, with a one-cell minimum for any nonzero model so a
heavily-used free model never visually disappears in cost mode. As
usage climbs past 80% of the current scale, every bar shrinks together
in one animated "zoom out" — proportions between bars are exact at all
times, because they all share the same scale (`internal/core/bars.go`).

**Manual zoom** (`-`/`+`/`0`) exists because a single dominant model
plus a long tail of small ones squashes the tail into indistinguishable
one-cell stubs under pure auto-scaling — no single linear scale shows
both ends of a skewed distribution at once. `-` zooms out to the next
larger rung (shrinks everything, useful for seeing rare large
outliers), `+`/`=` zooms in (grows the tail, but now-oversized bars
clamp at full width and lose their mutual distinction), `0` returns to
auto. A manual scale is transient view state: it resets to auto on
refresh, a timeframe switch, or a mode toggle, since each of those
already re-baselines the view. The active scale is always shown, muted,
in the status row, with a `·manual` suffix while pinned.

**The split inside a bar** sits at the input share of the bar's own
denomination — the true summed input/output cost, not `unit price ×
token count`, so cache discounts and per-token pricing are already
baked in without any special-casing. If a model is free or didn't
report split costs, the boundary falls back to the token-volume split;
the bar's total length is always honest regardless.

**The highlight** is one thing: the most recent burst (§7), drawn as a
brighter shade of the segment it belongs to (its input value brightens
the input segment's trailing edge, its output value the output
segment's), sized proportionally in the active mode's unit with the
same one-cell floor as everything else. A new burst gets a brief bold
pulse on arrival, then settles into the plain bright shade and persists
until superseded or cleared by a manual refresh.

## 4. Responsive Behavior

The layout has no fixed dimensions — it derives from the terminal size
on every resize event, and resizing snaps rather than animates
(`internal/ui/layout.go`). A width ladder controls how much detail each
row shows: full labels and a verbose token split above roughly 110
columns; full model names but a single aggregate token figure (never a
misleading truncated in→out shorthand) from 80–109; middle-truncated
names and no secondary denomination from 60–79; name, primary metric,
and bar only below that; and, under a 40×14 floor, a centered "terminal
too small" message rather than a garbled layout. The four screen
regions (§2) exist at every breakpoint above the floor — only how many
model blocks the bars list can show changes with available height.

## 5. Color, Accents & Theming

Burnbar draws from the terminal's own 16-color ANSI palette only — no
hardcoded hex, so the app always inherits whatever theme the user
already has (`internal/ui/styles.go`). Every glyph (`█ ▒ ▸ ● ◌ ▼` and
friends) has a plain-ASCII fallback, and `NO_COLOR` or a dumb terminal
stays fully legible because every piece of meaning (segment, selection,
status) is also carried by shape or text, never color alone.

| Token | Default ANSI | Used for |
|---|---|---|
| `accent.primary` | cyan | wordmark, active timeframe, selection, input segment |
| `accent.primary.bright` | bright cyan | burst highlight, input side |
| `bar.output` | blue | output segment |
| `bar.output.bright` | bright blue | burst highlight, output side |
| `text.primary` | default fg | names, values |
| `text.muted` | bright black | token counts, timestamps, hints, scale |
| `status.ok`/`warn`/`error` | green/yellow/red | connection dot, error banner |

Because a fixed ANSI-16 mapping can collide with some terminal themes
(two tokens reading as near-identical colors), the `c` color picker
(§2) lets you remap any token to any of your theme's 16 colors, previewed
live, and persisted to `config.toml`'s `[colors]` table
(`internal/ui/theme.go`, `internal/data/colors.go`). Env-var config
overrides still win over the file; missing or partial `[colors]` falls
back to the default slot per token, and the ASCII/`NO_COLOR` fallback
path is untouched by theming.

## 6. Motion

Every bar's width animates toward its target via its own Harmonica
spring, critically damped so it settles without overshoot
(`internal/ui/anim.go`). A scale step retargets every spring at once —
the collective "zoom out" is the single most satisfying moment in the
app to watch happen. A new model's bar grows in from zero; a model
that drops out of the window shrinks to zero and is then removed (row
reordering itself is never animated, only bar length is). The whole
animation loop runs at roughly 30fps only while at least one spring is
still moving, plus a slow 15-second ticker to keep relative timestamps
and the credits age fresh — true idle is genuinely 0fps.

Two refinements make the motion read cleanly instead of choppily. First,
the bar's leading edge renders in eighth-cell increments using the
fractional block glyphs (`▏▎▍▌▋▊▉█`), the same technique tools like
`btop` use, so travel doesn't visibly quantize to whole cells. Second,
because the input/output split boundary and the highlight edges can't
be sub-cell-animated the same way (there's no partial glyph between a
solid `█` and a dithered `▒`), a bar in flight instead renders as one
flat, uniform color with no interior detail — fading in through a
density ramp (`░▒▓`), holding solid while the length animates, then
fading back out to reveal the true split only once the bar is at rest.
This also incidentally kills a highlight-flicker bug that existed
before: a bar's interior can no longer be computed against a
mid-flight width. Batch changes that recut every visible bar at once
(a timeframe switch, mode toggle, zoom, refresh, or scheduled rollover)
synchronize this fade across every bar so a multi-bar resize reads as
one coordinated motion instead of each bar finishing independently; a
single live request arriving is deliberately left unsynchronized, since
one bar visibly reacting quicker than another is itself information
(which model got hit, and how hard).

## 7. Data & State

Three in-memory stores feed one pure aggregation function
(`internal/core/rows.go`, `aggregate.go`): a **baseline** (31 days of
`usage_daily` rows, UTC-day grain), a **rows** store (raw `requests`
rows from the today-slice fetch plus live arrivals, deduplicated by
`(trace_id, span_id)`), and derived view state (selected window, mode,
manual scale). `Aggregate(baseline, rows, window, now) → []ModelStat` is
the one function everything on screen flows through — zero-guarded
everywhere, so a divide-by-zero anywhere in the UI renders `—` instead
of crashing or lying.

**Windows.** `today` is the user's *local* calendar day, computed
exclusively from the raw rows store (which can be re-cut to any local
midnight); `week` and `month` are the current UTC calendar week
(Monday–Sunday) and UTC calendar month, computed from baseline + live
rows layered together. UTC-anchoring week/month (rather than the user's
local calendar) keeps the boundary aligned with the baseline view's
UTC-day grain, avoiding a refetch on every timeframe switch — the
tradeoff is explained further in [`ARCHITECTURE.md`](../ARCHITECTURE.md).
Switching the timeframe (`t`) is pure client-side re-aggregation, never
a network round trip. Rollover timers re-aggregate at local midnight
(for `today`) and UTC midnight (for `week`/`month`, since every UTC
midnight is a candidate week/month boundary too).

**Startup and the live layer.** On launch, the app loads config (a
plain, actionable error before the alternate screen is entered if
something's missing), renders immediately with placeholders, then
connects to the live source and subscribes *before* fetching the
baseline and today-slice — a row landing mid-fetch might be briefly
double-counted, which self-heals on the next refresh, whereas
fetching first could silently miss it. Live delivery defaults to a
Phoenix-channel WebSocket (`github.com/nshafer/phx`,
`internal/data/realtime.go`) carrying Supabase Realtime's
`postgres_changes` INSERT events; every successful (re)join refetches
the baseline and today-slice and clears any live rows, so a missed
event during a disconnect heals itself via re-baselining rather than
gap-tracking. A polling backup (PostgREST every 20 seconds) is always
available and toggled at runtime with `p` — it is a manual, visible
alternative, never a silent auto-failover. (An earlier community
Realtime client was tried and rejected outright — its reconnect logic
was structurally broken and it wrote to stderr, corrupting the
alternate screen; see the git history around `internal/data/realtime.go`
for the postmortem, kept there rather than here since it's now purely
historical.)

**Bursts.** Agentic tool loops can fire a dozen rapid OpenRouter calls
for what a human thinks of as one action, each landing as its own row.
A single "most recent request" highlight would then show only the
last, often-tiny call. `latestBurst` (`internal/core/burst.go`) instead
walks backward from the newest live arrival and coalesces a run of
same-model rows arriving within a few seconds of each other into one
display object — request count, span, and summed tokens/cost — that
feeds both the bar highlight and the recent-request popup. This
grouping is display-only and heuristic by necessity (OpenRouter's
payload carries no conversation identifier to group on honestly); a
mis-grouped burst only ever slightly misattributes the highlight, never
the authoritative totals, which is what makes the heuristic acceptable.

**Credits.** The remaining balance (`total_credits − total_usage`) is
fetched directly from `GET /api/v1/credits` using the user's own
OpenRouter key — never through the backend — and shown as-is with its
age, never locally decremented (OpenRouter's own caching means a local
subtraction would produce visible "bounce" artifacts). It refreshes on
launch and manual refresh, on a burst-grouped debounce roughly 70
seconds after live events land (since OpenRouter's cache takes about a
minute to catch up), and otherwise every 5 minutes as an idle
heartbeat (`internal/data/credits.go`, `credits_sched.go`).

**Configuration** lives in `~/.config/burnbar/config.toml`
(gitignored; `config.example.toml` in this directory is the committed
template) with every key overridable by an environment variable:

| TOML key | Env override | Required |
|---|---|---|
| `supabase_url` | `BURNBAR_SUPABASE_URL` | yes |
| `supabase_anon_key` | `BURNBAR_SUPABASE_ANON_KEY` | yes |
| `openrouter_api_key` | `BURNBAR_OPENROUTER_API_KEY` | no — credits header shows `—` with a hint when absent |
| `live_source` | `BURNBAR_LIVE_SOURCE` | no — `poll` or `realtime`; `p` toggles at runtime; default `realtime` |

An optional `[colors]` table (§5) holds any custom palette saved from
the `c` color picker.

## 8. Errors & Resilience

The app is designed to degrade visibly rather than crash or lie
silently:

| Situation | Behavior |
|---|---|
| Invalid or missing config | Plain-text actionable error printed before the alternate screen opens; exits non-zero |
| No network at launch | App still starts; the bars region shows `✗ offline — retrying in Ns` and recovers automatically |
| Baseline/today-slice fetch fails | Retries with backoff, keeps showing stale data tagged `data from HH:MM`, surfaces an error in the status row |
| Realtime connection drops | `○ reconnecting…` with backoff; re-baselines on successful rejoin |
| Credits fetch fails | Keeps the last value with a growing age; a 401 specifically hints "check openrouter_api_key" |
| Empty window | A friendly "no usage in this window yet" message, not treated as an error |
| Terminal too small | The centered minimum-size message from §4 |

Beyond those expected cases, the app guards against the unexpected
without ever crashing: malformed or incomplete realtime payloads are
logged and skipped, duplicate events are absorbed by the dedupe store,
clock skew that would make the meter lag negative clamps to `—`
instead, and any panic is caught by Bubble Tea with the terminal
restored before anything is printed. `Ctrl+C` always quits cleanly.
Nothing is ever written to stdout, since the terminal itself is the UI;
set `BURNBAR_DEBUG=1` to route Bubble Tea's internal log to
`~/.local/state/burnbar/debug.log` instead.

## 9. Formatting Rules

All numbers on screen use adaptive precision rather than a fixed number
of decimals, so a value is always exactly as readable as it needs to be
and no more (`internal/core/format.go`): token counts as `842`, `12.5K`,
`1.2M`, `3.4B`; costs as `$123`, `$12.34`, `$0.4821`, `$0.000073`
(cost formatting widens its precision as the number shrinks, since a
sub-cent-per-request model would otherwise round to `$0.00`); durations
as `840ms`, `2.1s`, `1m 12s`; and timestamps as `HH:MM:SS` plus a
relative age that the 15-second ticker keeps fresh. All width
calculations go through `lipgloss.Width()` rather than raw string
length, so wide glyphs and ANSI escape codes never throw off alignment.
