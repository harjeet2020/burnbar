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
Postgres `requests` table  ── realtime publication enabled
    │                        ┌──────────────────────────────┐
    ├── Supabase Realtime ──►│ Go TUI (Bubble Tea)          │
    │      (insert events)   │ macOS menubar (SwiftUI)      │
    ├── PostgREST ──────────►│  - initial window aggregates │
    │      (on open/resync)  │  - periodic resync           │
    └── Edge Function `reconcile` (cron, daily)
           compares yesterday vs OpenRouter analytics API,
           corrects DB retroactively

Frontends also poll OpenRouter `/api/v1/credits` directly
(user's key stored locally; never sent to the backend).
```

## 3. Stack & Rationale

| Layer | Choice | Why |
|---|---|---|
| Backend | Supabase: Postgres + Edge Functions (Deno/TypeScript) + Realtime + Cron | One free-tier project covers ingest, storage, live push, and scheduling. TypeScript edge functions match the maintainer's stack. |
| CLI TUI | Go + Bubble Tea + Lip Gloss + Harmonica | Best-in-class TUI polish and spring animations; single static binary; cross-platform; highly agent-writable and reviewable. |
| macOS app | Swift + SwiftUI (`MenuBarExtra`), supabase-swift, GRDB (SQLite) | Native menubar UX is the core product. Official Supabase Swift SDK covers Realtime + PostgREST. GRDB for the local cache. |
| Docs | Markdown in-repo | Self-host setup guide is a first-class deliverable. |

Conventions: TypeScript edge functions use strict mode, JSDoc on all
exported symbols, small pure functions for parsing (testable without a
running server). Go follows standard idioms + doc comments on exported
identifiers. Swift follows Swift API Design Guidelines + doc comments.

## 4. Data Model (initial)

```sql
create table requests (
  id            bigint generated always as identity primary key,
  trace_id      text not null,            -- OTLP trace id (idempotency key)
  span_id       text not null,
  model         text not null,            -- e.g. "anthropic/claude-sonnet-5"
  provider      text,                     -- upstream provider if present
  input_tokens  integer not null default 0,
  output_tokens integer not null default 0,
  cost_usd      numeric(12, 8) not null default 0,
  requested_at  timestamptz not null,     -- span start time
  duration_ms   integer,
  source        text not null default 'broadcast',  -- 'broadcast' | 'reconciliation'
  inserted_at   timestamptz not null default now(),
  unique (trace_id, span_id)              -- dedupe on webhook redelivery
);
create index requests_requested_at_idx on requests (requested_at desc);
create index requests_model_time_idx on requests (model, requested_at desc);
```

- Aggregation for the bar chart is a PostgREST query (or a SQL view
  `usage_by_model(window)`) grouping by model over the selected window —
  decide view vs. client-side aggregation in Phase 2 based on payload size.
- Reconciliation writes `source = 'reconciliation'` correction rows rather
  than mutating broadcast rows, so origins stay auditable. Daily totals =
  sum of both.
- Realtime: enable the `supabase_realtime` publication for `requests`
  (INSERT events only). Payload is one row — satisfies the "minimal
  WebSocket payload" requirement from the README.

## 5. Configuration & Secrets

| Name | Where | Purpose |
|---|---|---|
| `BURNBAR_WEBHOOK_SECRET` | Supabase Edge Function secret + OpenRouter Broadcast custom header | Authenticates webhook calls |
| `OPENROUTER_MANAGEMENT_KEY` | Supabase Edge Function secret | Reconciliation reads `/api/v1/activity` |
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
│   │   └── reconcile/         (daily analytics reconciliation)
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
- [ ] Migration: `requests` table, indexes, RLS policies, realtime publication (per §4–§5)
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
- [ ] Initial data load: PostgREST query for per-model aggregates in the active window (day default)
- [ ] Live updates: subscribe to `requests` INSERTs via Supabase Realtime. **Try `supabase-community/realtime-go` first; if it proves unreliable (community-maintained — see Risks), fall back to polling PostgREST every 2s for rows newer than the last seen `inserted_at`** — still within the 1–5s latency budget. Record the outcome in the Decision Log.
- [ ] Credits: poll `GET /api/v1/credits` every 60s; show balance in header
- [ ] Bars UI (per README UI/UX contract): one bar per model, sorted by usage desc, proportional widths, input/output tokens + spend labels; accent1 = usage since app start, accent2 = most recent request
- [ ] Timeframe switching (keys `d` / `w` / `m`) triggering a re-fetch
- [ ] Footer: last request time + last sync time
- [ ] Animations: Harmonica springs for bar-width transitions and new-request highlight decay
- [ ] Graceful states: empty window, connection lost/retry (with visible status), terminal resize
- [ ] `go test` for aggregation/formatting logic (token/cost formatting, proportional-width math)

**Definition of done:** run `burnbar` in a terminal, make an OpenRouter
request, watch the bar animate within seconds. This is the weekend-MVP
finish line together with Phase 1.

### Phase 3 — Reconciliation
- [ ] `reconcile` edge function: fetch previous UTC day from `GET /api/v1/activity` (management key), aggregate our `requests` for the same day per model, diff
- [ ] On mismatch: insert `source='reconciliation'` correction rows (delta per model); log a summary
- [ ] Idempotent per day (track reconciled dates in a small `reconciliation_runs` table)
- [ ] Schedule daily via Supabase Cron (~02:00 UTC, after the analytics day closes)
- [ ] Unit tests: no-drift, missing-requests, and already-reconciled cases

### Phase 4 — macOS menubar app (core product)
- [ ] Xcode project in `macos/`: SwiftUI `MenuBarExtra` (window style), macOS 14+
- [ ] Settings window: Supabase URL/anon key, OpenRouter key → Keychain
- [ ] supabase-swift: initial aggregate fetch + Realtime subscription (mirror the TUI's data layer semantics)
- [ ] GRDB SQLite cache: store request rows locally; instant render from cache on open, then resync deltas from PostgREST (per README technical considerations)
- [ ] Bars UI per README contract (proportional bars, accent1/accent2, timeframe picker, credits header, timestamps footer) — polished animations, respects light/dark mode
- [ ] Credits polling; menubar icon shows compact state (e.g. today's spend)
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
  Reconciliation (Phase 3) is the mitigation, not an optional extra.
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
