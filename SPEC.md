# Burnbar — Technical Spec & Implementation Plan

> **How to use this file (for AI agents):** This is the single source of
> truth for implementation work. At the start of a session, read this file
> and `README.md`, find the first unchecked item in the current phase, and
> continue from there. At the end of a session: check off completed items,
> add any new discoveries to **Decision Log** or **Risks & Open
> Questions**, and note partial work under the relevant checklist item.
> Do not reorder phases without recording why in the Decision Log.
> Never commit secrets; all keys live in `.env` files (gitignored) or
> Supabase secrets.

## 1. Product Summary

Real-time LLM cost meter for OpenRouter users. OpenRouter's Broadcast
feature POSTs an OTLP JSON trace to our Supabase Edge Function after every
LLM request. The function parses it and inserts a row into Postgres.
Frontends (Go TUI first, then a native macOS menubar app — the core
product) subscribe via Supabase Realtime and render per-model usage bars
(input tokens, output tokens, spend) for a selectable window (day / week /
month), plus the remaining OpenRouter credit balance.

Single-user, self-hosted, open source. No auth system, no multi-tenancy,
no hosted instance. See README for philosophy and UI/UX description —
the UI/UX section of the README is the design contract for both frontends.

## 2. Architecture

```
OpenRouter (Broadcast, Privacy Mode ON, custom auth header)
    │  POST OTLP JSON per completed request
    ▼
Supabase Edge Function `ingest`  ── verifies shared-secret header
    │  parse OTLP → normalized row
    ▼
Postgres `requests` (per-request broadcast rows; 3-day retention)
Postgres `analytics_daily` (authoritative daily aggregates; kept forever)
    ▲
    │  Edge Function `sync-analytics` (cron, daily 01:00 UTC):
    │  fetch full 30-day window from OpenRouter `/api/v1/activity`,
    │  upsert per (day, model); prune `requests` rows older than 3 days
    │
`usage_daily` view merges both tables — for each UTC day:
  the analytics_daily row if present, else aggregated broadcast rows
    │                        ┌──────────────────────────────┐
    ├── PostgREST ──────────►│ Go TUI (Bubble Tea)          │
    │  (view; open/refresh)  │ macOS menubar (SwiftUI)      │
    ├── Supabase Realtime ──►│  - baseline from usage_daily │
    │  (requests INSERTs)    │  - live adds via realtime    │
    │                        └──────────────────────────────┘

Frontends also poll OpenRouter `/api/v1/credits` directly
(user's key stored locally; never sent to the backend).
```

**Source-split model:** past UTC days are served from `analytics_daily`
(authoritative, from OpenRouter's analytics API), while today is served
from live broadcast rows. There is no diff-based reconciliation — for any
given day the view reads from exactly one source, so double counting is
structurally impossible. Today's numbers are best-effort (Broadcast has no
delivery guarantees) and become authoritative the next day when the
analytics row lands.

**Day-bucketing invariant:** all bucketing uses **request start time in
UTC**, in both sources — the analytics API aggregates by request start
time, and `requested_at` is the OTLP span start time. A request that
starts 23:58 and finishes 00:03 lands in the same day in both sources.

## 3. Stack & Rationale

| Layer | Choice | Why |
|---|---|---|
| Backend | Supabase: Postgres + Edge Functions (Deno/TypeScript) + Realtime + Cron | One free-tier project covers ingest, storage, live push, and scheduling. TypeScript edge functions match the maintainer's stack. |
| CLI TUI | Go + Bubble Tea + Lip Gloss + Harmonica | Best-in-class TUI polish and spring animations; single static binary; cross-platform; highly agent-writable and reviewable. |
| macOS app | Swift + SwiftUI (`MenuBarExtra`), supabase-swift | Native menubar UX is the core product. Official Supabase Swift SDK covers Realtime + PostgREST. In-memory state only — the 30-day dataset is tiny (see Decision Log 2026-07-04); no local DB. |
| Docs | Markdown in-repo | Self-host setup guide is a first-class deliverable. |

Conventions: TypeScript edge functions use strict mode, JSDoc on all
exported symbols, small pure functions for parsing (testable without a
running server). Go follows standard idioms + doc comments on exported
identifiers. Swift follows Swift API Design Guidelines + doc comments.

## 4. Data Model (initial)

```sql
-- Per-request rows from Broadcast. Only ever holds ~3 days of data
-- (pruned by sync-analytics); serves "today" and the pre-sync gap.
create table requests (
  id            bigint generated always as identity primary key,
  trace_id      text not null,            -- OTLP trace id (idempotency key)
  span_id       text not null,
  model         text not null,            -- e.g. "anthropic/claude-sonnet-5"
  provider      text,                     -- upstream provider if present
  input_tokens  integer not null default 0,
  output_tokens integer not null default 0,
  cost_usd      numeric(12, 8) not null default 0,
  requested_at  timestamptz not null,     -- span START time (bucketing key)
  duration_ms   integer,
  inserted_at   timestamptz not null default now(),
  unique (trace_id, span_id)              -- dedupe on webhook redelivery
);
create index requests_requested_at_idx on requests (requested_at desc);
create index requests_model_time_idx on requests (model, requested_at desc);

-- Authoritative daily aggregates from OpenRouter /api/v1/activity.
-- Upserted daily by sync-analytics; rows are kept forever (the API only
-- exposes 30 days — deleted rows would be unrecoverable; storage is
-- trivial and old rows enable future >30-day views).
create table analytics_daily (
  day             date not null,          -- completed UTC day
  model           text not null,
  input_tokens    bigint not null default 0,
  output_tokens   bigint not null default 0,
  reasoning_tokens bigint,                -- nullable; confirm vs fixtures
  cost_usd        numeric(12, 8) not null default 0,
  request_count   integer not null default 0,
  updated_at      timestamptz not null default now(),
  primary key (day, model)
);

-- Single read surface for frontends. Precedence: for each UTC day, the
-- analytics row wins if present; otherwise broadcast rows are aggregated.
-- Never sums both sources for the same day → no double counting.
-- security_invoker makes the view enforce the underlying tables' RLS.
create view usage_daily with (security_invoker = true) as
select day, model, input_tokens, output_tokens, cost_usd, request_count,
       'analytics' as source
from analytics_daily
union all
select (requested_at at time zone 'utc')::date as day,
       model,
       sum(input_tokens), sum(output_tokens), sum(cost_usd), count(*),
       'broadcast'
from requests
where (requested_at at time zone 'utc')::date
      not in (select day from analytics_daily)
group by 1, 2;
```

- Frontends query `usage_daily` via PostgREST on open / manual refresh /
  timeframe switch (≤30 days × models rows — aggregate client-side), then
  layer Realtime `requests` INSERT events on top in memory. A refresh
  discards the in-memory deltas and re-baselines from the view.
- Realtime: enable the `supabase_realtime` publication for `requests`
  (INSERT events only; views cannot be subscribed to — by design the
  subscription is on the table). Payload is one row — satisfies the
  "minimal WebSocket payload" requirement from the README. Per-request
  events also drive the accent1/accent2 highlight UX.
- Retention: `requests` pruned to 3 days by `sync-analytics` (buffer for
  the pre-sync gap and failed runs); `analytics_daily` never pruned.

## 5. Configuration & Secrets

| Name | Where | Purpose |
|---|---|---|
| `BURNBAR_WEBHOOK_SECRET` | Supabase Edge Function secret + OpenRouter Broadcast custom header | Authenticates webhook calls |
| `OPENROUTER_MANAGEMENT_KEY` | Supabase Edge Function secret **only** — never in frontends | `sync-analytics` reads `/api/v1/activity`. Management keys can create/delete API keys, so they stay server-side (one copy, smallest blast radius) |
| `OPENROUTER_API_KEY` | Frontend-local only (Keychain on macOS; config file `~/.config/burnbar/config.toml` for TUI) | Credits polling from frontends |
| `SUPABASE_URL`, `SUPABASE_ANON_KEY` | Frontend-local config | Realtime + PostgREST access |

RLS: since this is single-user and the anon key only ever lives on the
user's own machines, enable RLS with a permissive read-only policy for
`anon` on `requests` (no insert/update — writes go through the service
role inside edge functions). Cheap defense-in-depth.

## 6. Repository Layout

```
burnbar/
├── README.md
├── SPEC.md
├── LICENSE                    (MIT)
├── docs/
│   └── SETUP.md               (self-host walkthrough)
├── supabase/
│   ├── migrations/
│   ├── functions/
│   │   ├── ingest/            (webhook receiver + OTLP parser)
│   │   └── sync-analytics/    (daily analytics upsert + broadcast pruning)
│   └── tests/fixtures/        (captured real Broadcast payloads)
├── scripts/
│   └── test-request.sh        (fire a cheap OpenRouter request for manual testing)
├── tui/                       (Go module)
└── macos/                     (Xcode project)
```

---

## 7. Phases & Checklists

### Phase 0 — Scaffolding
- [ ] `git init` + push public repo to GitHub via `gh` (user-run)
- [x] `.gitignore` (env files, `.DS_Store`, Go/Xcode artifacts, `supabase/.temp`)
- [x] MIT `LICENSE`
- [x] Repo layout as in §6 (empty dirs with `.gitkeep` where needed)
- [x] `scripts/test-request.sh` — manual-test helper that fires a cheap OpenRouter request (model slug overridable per invocation, for capturing payloads from different models)
- [ ] Create Supabase **cloud** project (dashboard, user-run) + `supabase init` + `supabase link` — cloud-first because Broadcast requires a public destination URL (see Decision Log 2026-07-04); local `supabase start` (Docker) is optional and only needed for local parser testing later
- [ ] Initial commit

### Phase 1 — Backend ingest pipeline
- [ ] Migration: `requests` + `analytics_daily` tables, `usage_daily` view, indexes, RLS policies, realtime publication (per §4–§5). The view ships in Phase 1 even though `analytics_daily` stays empty until Phase 3 — it falls back to broadcast rows entirely, so Phase 2 frontends build against the final read surface from day one
- [ ] `ingest` edge function skeleton: reject non-POST/PUT; verify `BURNBAR_WEBHOOK_SECRET` header; return 2xx for OpenRouter's `X-Test-Connection: true` probe (empty payload)
- [ ] **Capture real Broadcast payloads:** deploy a temporary logging version (log the raw body, insert nothing yet), enable Broadcast on OpenRouter pointing at the deployed cloud function, fire cheap requests via `scripts/test-request.sh` against **2–3 different models/providers**, save the raw OTLP JSON files to `supabase/tests/fixtures/`. Compare shapes across models — consistency (or lack of it) drives the table shape and parsing logic. Document the exact attribute→column field mapping in a comment block in the parser. *(The OTLP attribute names are not fully documented; the fixtures are the source of truth.)*
- [ ] OTLP parser as a pure function: `parseTrace(otlpJson) -> RequestRow[]` (a payload's `resourceSpans` may contain multiple spans); tolerant of unknown/missing attributes (log + skip, never 5xx on partial data)
- [ ] Insert with `on conflict (trace_id, span_id) do nothing` (idempotent redelivery)
- [ ] Deno unit tests for the parser against the fixture (happy path, missing cost, multi-span, empty payload)
- [ ] Deploy to a real Supabase project; configure Broadcast (Privacy Mode ON, secret header); verify end-to-end: OpenRouter request → row visible in table within seconds
- [ ] Verify a psql/Studio realtime subscription receives the INSERT event

**Definition of done:** making an OpenRouter request results in a correct
row and a realtime event, with tests passing via `supabase functions serve`
locally.

### Phase 2 — Go TUI (first frontend, MVP target)
- [ ] Go module scaffold in `tui/`; Bubble Tea program with Elm-architecture layout (model/update/view split into files)
- [ ] Config loading: `~/.config/burnbar/config.toml` + env-var overrides (Supabase URL/anon key, OpenRouter key)
- [ ] Initial data load: PostgREST query on the `usage_daily` view; aggregate per-model over the active window client-side (day default). Same query on manual refresh / timeframe switch, discarding in-memory realtime deltas
- [ ] Live updates: subscribe to `requests` INSERTs via Supabase Realtime. **Try `supabase-community/realtime-go` first; if it proves unreliable (community-maintained — see Risks), fall back to polling PostgREST every 2s for rows newer than the last seen `inserted_at`** — still within the 1–5s latency budget. Record the outcome in the Decision Log.
- [ ] Credits: poll `GET /api/v1/credits` every 60s; show balance in header. Display the polled value as-is — no local computation from broadcast costs (stale-anchor bounce; see Decision Log). Optional polish (pre-approved, both frontends): on a realtime event, schedule one debounced extra poll ~60–70s later to catch the refreshed cache right after spend happens — better than shortening the blind interval. Rate limits are a non-issue (no documented caps on metadata endpoints; 60s polling ≈ 2 req/min across two clients)
- [ ] Bars UI (per README UI/UX contract): one bar per model, sorted by usage desc, proportional widths, input/output tokens + spend labels; accent1 = usage since app start, accent2 = most recent request
- [ ] Timeframe switching (keys `d` / `w` / `m`) triggering a re-fetch
- [ ] Footer: last request time + last sync time
- [ ] Animations: Harmonica springs for bar-width transitions and new-request highlight decay
- [ ] Graceful states: empty window, connection lost/retry (with visible status), terminal resize
- [ ] `go test` for aggregation/formatting logic (token/cost formatting, proportional-width math)

**Definition of done:** run `burnbar` in a terminal, make an OpenRouter
request, watch the bar animate within seconds. This is the weekend-MVP
finish line together with Phase 1.

### Phase 3 — Analytics sync
- [ ] `sync-analytics` edge function: fetch the **full 30-day window** from `GET /api/v1/activity` (management key), upsert every row into `analytics_daily` on `(day, model)`, then prune `requests` rows older than 3 days
- [ ] Stateless and idempotent by design — **no retry or catch-up logic.** Every run is a complete repair: a missed/failed run, a too-early fetch, or late-completing requests are all fixed by the next run, and until then the `usage_daily` view keeps serving affected days from broadcast rows
- [ ] Schedule daily at 01:00 UTC via Supabase Cron (docs recommend waiting ~30 min after the UTC boundary; we double it)
- [ ] Log a warning (observability only) if the response lacks a row set for yesterday
- [ ] Unit tests: response→row mapping, empty response, overwrite-on-upsert, pruning boundary

### Phase 4 — macOS menubar app (core product)
- [ ] Xcode project in `macos/`: SwiftUI `MenuBarExtra` (window style), macOS 14+
- [ ] Settings window: Supabase URL/anon key, OpenRouter key → Keychain
- [ ] supabase-swift: `usage_daily` fetch on launch/wake/refresh + Realtime subscription (mirror the TUI's data layer semantics); in-memory state only — no local database
- [ ] Optional polish (only if launch feels slow): cache the last rendered snapshot to a JSON file for instant first paint; still re-fetch immediately
- [ ] Bars UI per README contract (proportional bars, accent1/accent2, timeframe picker, credits header, timestamps footer) — polished animations, respects light/dark mode
- [ ] Credits polling: fetch on popover open, then every 60s **while open** — no background polling (balance is only visible in the popover header; OpenRouter caches values up to 60s anyway, so faster polling gains nothing). Menubar icon shows compact state (e.g. today's spend) from realtime data, not credits
- [ ] Launch-at-login option; reconnect handling on wake-from-sleep
- [ ] Build/run instructions for people cloning the repo (unsigned local builds); code signing & notarization deferred (see Risks)

### Phase 5 — Self-host docs & release polish
- [ ] `docs/SETUP.md`: create Supabase project → run migrations → deploy functions → set secrets → configure OpenRouter Broadcast (Privacy Mode, header) → configure frontends. Target: a stranger completes it in <15 minutes.
- [ ] `justfile` (or Makefile) for common tasks: db reset, deploy, test, build TUI
- [ ] TUI release builds (goreleaser: darwin/linux/windows) 
- [ ] README final pass: screenshots/GIF of both frontends
- [ ] Decide macOS distribution: unsigned build vs. $99/yr Apple Developer notarization (user decision — ask, don't assume)

---

## 8. Risks & Open Questions

- **OTLP schema is under-documented.** Exact attribute names for tokens/
  cost are confirmed only by the captured fixture (Phase 1). OpenRouter may
  change them; the parser must fail soft and the fixture must be kept
  current.
- **No Broadcast delivery guarantees.** OpenRouter documents no retries.
  Under the source-split model this only affects *today's* numbers, which
  are explicitly best-effort and become authoritative the next day when
  the analytics row lands. Acceptable for a spend-awareness tool.
- **Analytics availability timing.** Docs recommend waiting ~30 min after
  the UTC boundary (aggregation is by request start time; long-running
  requests trickle in). We sync at 01:00 UTC. If a run fires too early or
  fails, the view falls back to broadcast rows for that day (3-day
  retention buffer) and the next run repairs everything (full-window
  upsert). No push notification for analytics availability exists.
- **Reasoning tokens.** `/api/v1/activity` reports them separately;
  whether the Broadcast OTLP payload does too is unknown until fixtures
  are captured. `analytics_daily.reasoning_tokens` is nullable pending
  that.
- **`realtime-go` maturity.** Community-maintained; the 2s-polling
  fallback is pre-approved (Phase 2) if it misbehaves.
- **`/api/v1/activity` needs a management key** (separate from inference
  keys) and covers completed UTC days only. Setup docs must walk through
  creating one.
- **Credits endpoint staleness.** OpenRouter caches credit values up to
  ~60s; the balance display should not promise more freshness than that.
- **macOS distribution.** Unsigned apps trigger Gatekeeper warnings;
  notarization costs $99/yr. Deferred to Phase 5; user decides.

## 9. Decision Log

| Date | Decision | Rationale |
|---|---|---|
| 2026-07-03 | Cut hosted instance; self-host only | Realtime concurrent-connection pricing scales with active users; avoids auth/multi-tenancy/key custody entirely |
| 2026-07-03 | TUI first, in Go + Charm stack | Validates pipeline end-to-end with least friction; best TUI animation ecosystem; single-binary distribution; agent-friendly |
| 2026-07-03 | macOS menubar app is the core product | Primary daily-driver for the maintainer; TUI remains the cross-platform alternative |
| 2026-07-03 | Privacy Mode required in setup | Burnbar never needs prompt/completion content; trust win |
| 2026-07-03 | Credits polled from frontends, not backend | Keeps user's inference key off the server entirely |
| 2026-07-03 | Reconciliation writes correction rows, not updates | Auditability; broadcast data stays immutable |
| 2026-07-04 | Cloud-first Supabase workflow; local stack optional | Broadcast requires a publicly reachable destination, so fixtures/e2e verification must run against the deployed cloud function. Repo remains source of truth: schema ships via `supabase db push`, functions via `supabase functions deploy`. Local Docker stack only for offline parser tests |
| 2026-07-04 | Source-split replaces diff-based reconciliation | Past UTC days from `analytics_daily` (authoritative), today from broadcast `requests`; merged by `usage_daily` view with analytics-wins precedence. Kills diffing, correction rows, and `reconciliation_runs`; per-day single-source makes double counting structurally impossible |
| 2026-07-04 | Sync = daily 01:00 UTC, full-30-day-window upsert (not yesterday-only insert) | Every run is a complete repair: missed runs, early fetches, and late-completing requests self-heal without retry/catch-up state. Yesterday-only insert would leave permanent holes on failure |
| 2026-07-04 | `analytics_daily` rows kept forever | The API only exposes 30 days — deleted rows are unrecoverable. Storage is trivial (~thousands of rows/yr); preserves the option of >30-day views |
| 2026-07-04 | Management key stays server-side (Edge Function secret only) | It can create/delete API keys — far more dangerous than an inference key. One copy on the server beats a copy per frontend device |
| 2026-07-04 | Day bucketing = request **start** time, UTC, in both sources | Matches `/api/v1/activity` semantics; a request straddling midnight lands in the same day in both sources, so totals converge |
| 2026-07-04 | macOS app: in-memory state only, no SQLite/GRDB | 30-day dataset is a few KB of daily aggregates; fetch-on-launch is sub-second. Removes a dependency and a cache-invalidation layer. JSON snapshot cache is the pre-approved polish fallback |
| 2026-07-04 | Credits: display polled value as-is, 60s interval; no optimistic local decrement | OpenRouter caches credit values up to ~60s, so polling faster can't beat that floor, and subtracting broadcast costs from a stale anchor causes visible bounce-up artifacts. Worst-case display lag ≈ cache TTL + poll interval (~2 min) is acceptable — the usage bars, not the balance, are the real-time surface. Event-triggered debounced re-poll (~60–70s after a realtime event) is the pre-approved polish if fresher balance is wanted |
