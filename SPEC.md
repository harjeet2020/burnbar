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

Burnbar is a **live meter, not an accounting system**: broadcast data is
the app's single source of truth, the OpenRouter dashboard remains the
authoritative record for billing and long-term stats, and the credits
endpoint always reflects the true balance regardless of what Burnbar
captured. Single-user, self-hosted, open source. No auth system, no
multi-tenancy, no hosted instance. See README for philosophy and UI/UX
description — the UI/UX section of the README is the design contract for
both frontends.

## 2. Architecture

```
OpenRouter (Broadcast, Privacy Mode ON, custom auth header)
    │  POST OTLP JSON per completed request
    ▼
Supabase Edge Function `ingest`  ── verifies shared-secret header
    │  parse OTLP → normalized row, idempotent insert
    ▼
Postgres `requests` — single source of truth
    │  (30-day retention via monthly pg_cron prune)
    │
`usage_daily` view — per-day, per-model aggregates over `requests`
    │                        ┌──────────────────────────────┐
    ├── PostgREST ──────────►│ Go TUI (Bubble Tea)          │
    │  (view; open/refresh)  │ macOS menubar (SwiftUI)      │
    ├── Supabase Realtime ──►│  - baseline from usage_daily │
    │  (requests INSERTs)    │  - live adds via realtime    │
    │                        └──────────────────────────────┘

Frontends also poll OpenRouter `/api/v1/credits` directly
(user's key stored locally; never sent to the backend).
```

**Broadcast-only model:** every number in the app comes from broadcast
rows in `requests`. There is no second ingestion path, no analytics sync,
no reconciliation, and no OpenRouter management key anywhere in the
system. OpenRouter documents no delivery guarantees or retries for
Broadcast, so occasional silent gaps are possible (e.g. an outage on
either side, or a paused free-tier project) — this is an **accepted
trade-off**: captured fixtures showed broadcast delivers far richer data
than the analytics API (routed provider + quantization, cached tokens,
reasoning tokens, split costs, unit prices, per-request timing), and for
a spend-awareness tool a missed row only means a bar undercounts. The
financial ground truth (credit balance, OpenRouter dashboard) is
unaffected by any missed delivery.

**Day-bucketing invariant:** all bucketing uses **request start time in
UTC** — `requested_at` is the OTLP span start time
(`startTimeUnixNano`). This matches how OpenRouter's own dashboard
buckets usage, so day totals line up when eyeballing the two.

## 3. Stack & Rationale

| Layer | Choice | Why |
|---|---|---|
| Backend | Supabase: Postgres + Edge Function (Deno/TypeScript) + Realtime + pg_cron | One free-tier project covers ingest, storage, live push, and scheduled retention. TypeScript edge functions match the maintainer's stack. Retention is a single SQL statement scheduled with pg_cron — no second function needed. |
| CLI TUI | Go + Bubble Tea + Lip Gloss + Harmonica | Best-in-class TUI polish and spring animations; single static binary; cross-platform; highly agent-writable and reviewable. |
| macOS app | Swift + SwiftUI (`MenuBarExtra`), supabase-swift | Native menubar UX is the core product. Official Supabase Swift SDK covers Realtime + PostgREST. In-memory state only — the 30-day dataset is tiny (see Decision Log 2026-07-04); no local DB. |
| Docs | Markdown in-repo | Self-host setup guide is a first-class deliverable. |

Conventions: TypeScript edge functions use strict mode, JSDoc on all
exported symbols, small pure functions for parsing (testable without a
running server). Go follows standard idioms + doc comments on exported
identifiers. Swift follows Swift API Design Guidelines + doc comments.

## 4. Data Model

The `requests` table captures **everything the broadcast payload
offers** — webhook data cannot be backfilled, so anything not stored at
delivery time is lost forever (capture-now-or-never). Confirmed against
the captured fixture(s) in `supabase/tests/fixtures/`; OpenRouter uses
the OpenTelemetry GenAI semantic conventions (`gen_ai.*`) plus
`trace.metadata.openrouter.*` extensions.

### Attribute → column mapping (from fixtures)

| Column | OTLP source |
|---|---|
| `trace_id` | `span.traceId` |
| `span_id` | `span.spanId` |
| `model` | attr `gen_ai.request.model` (alias slug, e.g. `deepseek/deepseek-v4-flash` — NOT the versioned permaslug; groups naturally in the UI) |
| `author` | attr `gen_ai.provider.name` (model author, e.g. `deepseek`) |
| `provider` | attr `trace.metadata.openrouter.provider_name` (who actually served it, e.g. `Novita`) |
| `provider_slug` | attr `trace.metadata.openrouter.provider_slug` (may include a deployment-variant suffix: quantization `novita/fp8`, `deepinfra/fp4`, region `amazon-bedrock/global` — or none, e.g. `openai`) |
| `input_tokens` | attr `gen_ai.usage.input_tokens` |
| `output_tokens` | attr `gen_ai.usage.output_tokens` (**includes** reasoning tokens) |
| `cached_tokens` | attr `gen_ai.usage.input_tokens.cached` |
| `reasoning_tokens` | attr `gen_ai.usage.output_tokens.reasoning` |
| `cost_usd` | attr `gen_ai.usage.total_cost` |
| `input_cost_usd` | attr `gen_ai.usage.input_cost` |
| `output_cost_usd` | attr `gen_ai.usage.output_cost` |
| `input_unit_price` | attr `trace.metadata.openrouter.input_unit_price` (USD per input token, as quoted) |
| `output_unit_price` | attr `trace.metadata.openrouter.output_unit_price` |
| `requested_at` | `span.startTimeUnixNano` — arrives as a **string** (nanosecond epochs exceed JS 2⁵³), parse via `BigInt`, store as `timestamptz` |
| `duration_ms` | `(endTimeUnixNano − startTimeUnixNano) / 1e6` |

```sql
create table requests (
  id                bigint generated always as identity primary key,
  trace_id          text not null,
  span_id           text not null,
  model             text not null,             -- UI grouping key (alias slug)
  author            text,                      -- model author ("deepseek")
  provider          text,                      -- routed provider ("Novita")
  provider_slug     text,                      -- incl. quantization ("novita/fp8")
  input_tokens      integer not null default 0,
  output_tokens     integer not null default 0,   -- includes reasoning tokens
  cached_tokens     integer,
  reasoning_tokens  integer,
  cost_usd          numeric(12, 8) not null default 0,
  input_cost_usd    numeric(12, 8),
  output_cost_usd   numeric(12, 8),
  input_unit_price  numeric,                   -- unconstrained: rates go as low as ~1e-8
  output_unit_price numeric,
  requested_at      timestamptz not null,      -- span START time (bucketing key)
  duration_ms       integer,
  inserted_at       timestamptz not null default now(),
  unique (trace_id, span_id)                   -- dedupe on webhook redelivery
);
create index requests_requested_at_idx on requests (requested_at desc);
create index requests_model_time_idx on requests (model, requested_at desc);

-- Single read surface for frontends: per-day, per-model aggregates.
-- security_invoker makes the view enforce the underlying table's RLS.
create view usage_daily with (security_invoker = true) as
select (requested_at at time zone 'utc')::date as day,
       model,
       sum(input_tokens)  as input_tokens,
       sum(output_tokens) as output_tokens,
       sum(cost_usd)      as cost_usd,
       count(*)           as request_count
from requests
group by 1, 2;

-- Retention: the app never looks back more than 30 days (older stats live
-- in the OpenRouter dashboard). Monthly prune; between runs the table may
-- hold up to ~60 days of rows — harmless, the view/window queries are
-- time-bounded anyway.
select cron.schedule(
  'burnbar-prune-requests',
  '0 3 1 * *',   -- 03:00 UTC on the 1st of each month
  $$ delete from public.requests
     where requested_at < now() - interval '30 days' $$
);
```

- **Nullable vs. `default 0` is deliberate:** `NULL` means "the payload
  did not report this attribute", `0` means "reported as zero". The
  not-null core columns (`model`, tokens, cost, `requested_at`) double as
  the parser's validation contract — missing those → log + skip the
  span; missing a nullable column → insert anyway.
- Frontends query `usage_daily` via PostgREST on open / manual refresh /
  timeframe switch (≤30 days × models rows — aggregate client-side), then
  layer Realtime `requests` INSERT events on top in memory. A refresh
  discards the in-memory deltas and re-baselines from the view.
- Realtime: enable the `supabase_realtime` publication for `requests`
  (INSERT events only; views cannot be subscribed to — by design the
  subscription is on the table). Payload is one row — satisfies the
  "minimal WebSocket payload" requirement from the README. Per-request
  events also drive the accent1/accent2 highlight UX.
- Rich columns (`provider*`, `cached_tokens`, unit prices, split costs)
  are not surfaced in MVP UI; they accumulate for future stats views
  (e.g. average resolved cost per model, cache hit rate) within the
  30-day window.

## 5. Configuration & Secrets

| Name | Where | Purpose |
|---|---|---|
| `BURNBAR_WEBHOOK_SECRET` | Supabase Edge Function secret + OpenRouter Broadcast custom header (`X-Burnbar-Secret`) + local `.env` | Authenticates webhook calls — the function URL is public (`verify_jwt = false` in `supabase/config.toml`), the secret is the sole gate |
| `OPENROUTER_API_KEY` | Frontend-local only (Keychain on macOS; config file `~/.config/burnbar/config.toml` for TUI; `.env` for dev scripts) | Credits polling + `scripts/test-request.sh` |
| `SUPABASE_URL`, `SUPABASE_ANON_KEY` | Frontend-local config + `.env` | Realtime + PostgREST access |

No OpenRouter **management key** exists anywhere in the system — the
analytics sync that needed it was removed (see Decision Log 2026-07-05).
If the optional audit script (Phase 3) is ever built, it would take a
management key as a runtime argument locally, never stored.

`.env.example` (committed) documents every variable; the real `.env` is
gitignored. RLS: single-user project, so enable RLS with a permissive
read-only policy for `anon` on `requests` (no insert/update — writes go
through the service role inside the edge function). Cheap
defense-in-depth.

## 6. Repository Layout

```
burnbar/
├── README.md
├── SPEC.md
├── LICENSE                    (MIT)
├── .env.example               (committed template; real .env gitignored)
├── docs/
│   └── SETUP.md               (self-host walkthrough)
├── supabase/
│   ├── config.toml            (incl. [functions.ingest] verify_jwt = false)
│   ├── migrations/
│   ├── functions/
│   │   └── ingest/            (webhook receiver + OTLP parser)
│   └── tests/fixtures/        (captured real Broadcast payloads)
├── scripts/
│   └── test-request.sh        (fire a cheap OpenRouter request for manual testing)
├── tui/                       (Go module)
└── macos/                     (Xcode project)
```

---

## 7. Phases & Checklists

### Phase 0 — Scaffolding
- [x] `git init` + push public repo to GitHub via `gh` (user-run)
- [x] `.gitignore` (env files, `.DS_Store`, Go/Xcode artifacts, `supabase/.temp`)
- [x] MIT `LICENSE`
- [x] Repo layout as in §6 (empty dirs with `.gitkeep` where needed)
- [x] `scripts/test-request.sh` — manual-test helper that fires a cheap OpenRouter request (model slug overridable per invocation, for capturing payloads from different models)
- [x] Create Supabase **cloud** project (dashboard, user-run) + `supabase init` + `supabase link` — cloud-first because Broadcast requires a public destination URL (see Decision Log 2026-07-04); local `supabase start` (Docker) is optional and only needed for local parser testing later
- [x] Initial commit

### Phase 1 — Backend ingest pipeline
- [x] `.env.example` + `.env` scaffolding (all variables documented; `.env` verified gitignored)
- [x] `ingest` edge function skeleton: reject non-POST/PUT; verify `X-Burnbar-Secret` header against `BURNBAR_WEBHOOK_SECRET` (constant-time compare); return 2xx for OpenRouter's `X-Test-Connection: true` probe; `verify_jwt = false` via config.toml
- [x] Deploy log-only version (logs raw body + byte length, inserts nothing); curl smoke tests pass (405 / 401 / 401 / probe 200 / 200)
- [x] Configure Broadcast on OpenRouter (Privacy Mode ON, `X-Burnbar-Secret` header); delivery verified end-to-end in function logs
- [x] Capture first real Broadcast payload → `supabase/tests/fixtures/deepseek-v4-flash.json`; attribute→column mapping documented in §4 *(payload confirmed complete in logs — byte count matches content-length; no scratch table needed)*
- [x] Capture remaining fixtures (qwen, kimi, claude-haiku, gpt-mini) and diff shapes — **§4 mapping confirmed across all 5 models/providers**: every mapped attribute present everywhere. Optional attrs DO vary outside our mapping (`input_tokens.audio` absent for OpenAI/Anthropic; `output_tokens.image` OpenAI-only) — validates the tolerant parser + nullable columns. Kimi fixture proves cache discounts make `unit_price × tokens ≠ actual cost` (split-cost columns justified). Anthropic cache-*write* attribute unobserved (no cache-write traffic in fixture) — unknown, tolerated by design
- [ ] Migration: `requests` table, `usage_daily` view, indexes, RLS policies, realtime publication, pg_cron prune schedule (per §4–§5); `supabase db push`
- [ ] OTLP parser as a pure function: `parseTrace(otlpJson) -> RequestRow[]` (a payload's `resourceSpans` may contain multiple spans); tolerant of unknown/missing attributes (log + skip only when a not-null column is missing, never 5xx on partial data); mapping comment block kept in sync with §4
- [ ] Replace log-only body: insert with `on conflict (trace_id, span_id) do nothing` (idempotent redelivery); keep payload logging until e2e verified
- [ ] Deno unit tests for the parser against the fixtures (happy path per model, missing cost, multi-span, empty payload, missing nullable attrs)
- [ ] Deploy; verify end-to-end: OpenRouter request → correct row in `requests` within seconds
- [ ] Verify a realtime subscription receives the INSERT event

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

### Phase 3 — Optional audit tooling (post-MVP, may never be needed)
The daily analytics sync this phase originally held was removed (Decision
Log 2026-07-05). Retention shipped as a pg_cron statement in the Phase 1
migration. What remains here is strictly optional observability:
- [ ] Local audit script (not deployed): fetch `GET /api/v1/activity`
  (management key passed as an argument, never stored) and compare 30-day
  per-model totals against `usage_daily`; report drift %, write nothing.
  Only worth building if broadcast losses turn out to be noticeable in
  practice.

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
- [ ] `docs/SETUP.md`: create Supabase project → link → `db push` → deploy `ingest` → set webhook secret → configure OpenRouter Broadcast (Privacy Mode, header) → configure frontends. Target: a stranger completes it in <15 minutes. (No management key anywhere in setup.)
- [ ] `justfile` (or Makefile) for common tasks: db reset, deploy, test, build TUI
- [ ] TUI release builds (goreleaser: darwin/linux/windows)
- [ ] README final pass: screenshots/GIF of both frontends
- [ ] Decide macOS distribution: unsigned build vs. $99/yr Apple Developer notarization (user decision — ask, don't assume)

---

## 8. Risks & Open Questions

- **Broadcast delivery is best-effort and losses are silent.** OpenRouter
  documents no retries; a 5xx/timeout on our side or an outage on either
  side loses rows permanently, and a paused free-tier Supabase project
  would eat a multi-day hole. **Accepted by design** (Decision Log
  2026-07-05): Burnbar is a live meter; the credits balance and the
  OpenRouter dashboard are the financial ground truth. The Phase 3 audit
  script is the escape hatch if losses prove noticeable.
- **OTLP schema is under-documented.** The attribute mapping in §4 is
  confirmed by captured fixtures, not documentation. OpenRouter may
  change attribute names; the parser must fail soft (skip + log, never
  5xx) and fixtures must be kept current. Cross-provider shape
  consistency still being verified (4 fixtures pending).
- **Storage growth vs. free tier.** ~30–60 days of raw rows retained
  (monthly prune). Even heavy agentic use (~5k requests/day) stays around
  tens of MB — comfortably inside the 500MB free tier. If someone's usage
  breaks this assumption, tighten the cron to weekly; no schema change
  needed.
- **`realtime-go` maturity.** Community-maintained; the 2s-polling
  fallback is pre-approved (Phase 2) if it misbehaves.
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
| 2026-07-03 | ~~Reconciliation writes correction rows, not updates~~ | Superseded 2026-07-04 (source-split), then removed entirely 2026-07-05 (broadcast-only) |
| 2026-07-04 | Cloud-first Supabase workflow; local stack optional | Broadcast requires a publicly reachable destination, so fixtures/e2e verification must run against the deployed cloud function. Repo remains source of truth: schema ships via `supabase db push`, functions via `supabase functions deploy`. Local Docker stack only for offline parser tests |
| 2026-07-04 | ~~Source-split replaces diff-based reconciliation~~ | Superseded 2026-07-05 by broadcast-only — kept for history: past days from `analytics_daily`, today from broadcast, merged by view |
| 2026-07-04 | ~~Sync = daily 01:00 UTC full-window upsert~~ / ~~`analytics_daily` kept forever~~ / ~~Management key server-side only~~ | All superseded 2026-07-05: the analytics sync, its table, and the management key were removed from the system entirely |
| 2026-07-04 | Day bucketing = request **start** time, UTC | `requested_at` = OTLP span start; matches OpenRouter dashboard bucketing |
| 2026-07-04 | macOS app: in-memory state only, no SQLite/GRDB | 30-day dataset is a few KB of daily aggregates; fetch-on-launch is sub-second. Removes a dependency and a cache-invalidation layer. JSON snapshot cache is the pre-approved polish fallback |
| 2026-07-04 | Credits: display polled value as-is, 60s interval; no optimistic local decrement | OpenRouter caches credit values up to ~60s, so polling faster can't beat that floor, and subtracting broadcast costs from a stale anchor causes visible bounce-up artifacts. Worst-case display lag ≈ cache TTL + poll interval (~2 min) is acceptable — the usage bars, not the balance, are the real-time surface. Event-triggered debounced re-poll (~60–70s after a realtime event) is the pre-approved polish if fresher balance is wanted |
| 2026-07-05 | **Broadcast-only architecture**: dropped `analytics_daily`, `sync-analytics`, and the management key; `requests` is the single source of truth | First captured fixture proved broadcast is far richer than `/api/v1/activity` (provider slug w/ quantization, cached tokens, split costs, unit prices, per-request timing) — deleting broadcast rows in favor of analytics would discard unrecoverable data. Reconciliation can't bridge the shape mismatch (analytics lacks the rich columns, so "corrections" would corrupt them). Occasional silent loss is acceptable for a live meter: credits balance + OpenRouter dashboard remain the financial ground truth. Removes an entire edge function, a cron'd external dependency, the most dangerous secret, and all merge logic |
| 2026-07-05 | `requests` captures the full broadcast payload (author, provider, provider_slug, cached/reasoning tokens, split costs, unit prices) | Capture-now-or-never: webhooks can't be backfilled. Metadata columns are nullable (NULL = "not reported", 0 = "reported zero") so stats aren't corrupted when providers omit attributes. Store facts (actual split costs) rather than recompute them — cache discounts make `unit_price × tokens` inexact |
| 2026-07-05 | Retention: 30 days, pruned by a **monthly pg_cron job** (03:00 UTC on the 1st) | The app's largest window is one month; older stats live in the OpenRouter dashboard. Between monthly runs the table holds up to ~60 days — harmless since all queries are time-bounded. pg_cron keeps retention as one SQL statement inside the migration; no second edge function |
| 2026-07-05 | `requested_at` stored as `timestamptz` (parser converts OTLP's `startTimeUnixNano` string via `BigInt`) | OTLP serializes nano-epochs as strings (exceeds JS 2⁵³). Postgres-native timestamps buy `::date` bucketing, readable Studio output, natural time-range indexes, and ISO strings over PostgREST/Realtime that Go/Swift parse natively; sub-microsecond precision has no consumer |
| 2026-07-05 | Analytics endpoints researched (web-verified): `/api/v1/activity` returns per-(date, model, endpoint) rows incl. `provider_name`, `model_permaslug`, `reasoning_tokens`, `byok_usage_inference`; `/api/v1/analytics/query` offers metrics/dimensions (incl. `cache_hit_rate`) but **no provider dimension** and returns count metrics as strings | Documented for the optional Phase 3 audit script. Neither endpoint carries unit prices, split costs, or cached-token counts — confirming broadcast as the richest source |
