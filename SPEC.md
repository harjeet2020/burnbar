# Burnbar — Technical Spec & Implementation Plan

> **How to use this file (for AI agents):** This is the working runbook.
> At the start of a session, read this file and `README.md` (its UI/UX
> section is the design contract for both frontends), find the first
> unchecked item in the current phase, and continue from there. At the
> end of a session: check off completed items and note partial work under
> the relevant item; record newly agreed constraints in §6. Implemented
> details live in the code — the schema in `supabase/migrations/`, the
> OTLP attribute→column mapping in `supabase/functions/ingest/parse.ts` —
> look them up there instead of duplicating them here. Never commit
> secrets; all keys live in `.env` files (gitignored) or Supabase secrets.

## 1. System Overview

Real-time LLM cost meter for OpenRouter users — a **live meter, not an
accounting system**. Single-user, self-hosted, open source; no auth
system, no multi-tenancy, no hosted instance (see README for philosophy,
product description, and the UI/UX contract).

```
OpenRouter Broadcast (Privacy Mode ON, X-Burnbar-Secret header)
    │  POST OTLP JSON per completed request
    ▼
Edge Function `ingest` — verify secret, parse, idempotent insert
    ▼
Postgres `requests` — single source of truth (30-day pg_cron prune)
    │
    ├─ `usage_daily` view ── PostgREST ───► frontends (baseline fetch)
    └─ Realtime `requests` INSERTs ───────► frontends (live layer)

Frontends poll OpenRouter /api/v1/credits directly (key stays local).
```

Broadcast delivery is best-effort with no retries — an accepted
trade-off: a missed row only means a bar undercounts, while the credit
balance and the OpenRouter dashboard remain the financial ground truth.

**Stack:** Supabase backend (deployed — Phase 1 ✓); Go + Bubble Tea +
Lip Gloss + Harmonica TUI in `tui/`; Swift/SwiftUI `MenuBarExtra` app in
`macos/` (macOS 14+, supabase-swift). Conventions: doc comments on all
exported identifiers (JSDoc / Go doc / Swift markup); parse and
aggregation logic as small pure functions, testable without a server.

## 2. Frontend Data Contract

What Phases 2 & 4 build against (column details: see the migration):

- **Baseline:** query the `usage_daily` view — grain is (day, model,
  provider_slug) with **additive sums only** — on open / manual refresh
  / reconnect, plus a **today-slice** query of raw `requests` rows since
  local midnight (see Windows below); aggregate over the active window
  client-side (bars sum across provider rows; the details screen uses
  the provider split). ≤30 days of rows is a few KB — no local database
  needed. Timeframe switching is pure client-side re-aggregation, never
  a refetch.
- **Ratios are derived client-side** from sums at the selected window,
  never averaged across days (averages don't compose): cache % =
  cached/input, effective rate = cost/tokens, avg duration =
  duration_ms_sum/timed_request_count, provider share = provider row /
  model total.
- **Live layer:** Realtime INSERT events on `requests` (one row per
  event), layered in memory on top of the baseline. A refresh discards
  the deltas and re-baselines. Live events also drive the latest-burst
  highlight + recent-request modal (`tui/SPEC.md` §5/§7, 2026-07-08
  revision — one highlight of the most recent coalesced burst, replacing
  the earlier accent1/accent2 + focus design).
- **Subscribe before fetch:** a row landing mid-fetch is briefly
  double-counted and self-heals on the next refresh; fetch-first would
  silently undercount until then. Brief overcount beats silent
  undercount.
- **Day bucketing:** `requested_at` (OTLP span *start* time) in UTC —
  matches how OpenRouter's own dashboard buckets usage.
- **Windows:** `today` = the user's **local** calendar day, computed
  exactly from raw `requests` rows fetched since local midnight (view
  rows are UTC-day grain and can't be re-cut to local midnights);
  `week`/`month` = last 7/30 **UTC** days including the current UTC day,
  from the view + live events.
- **NULL vs 0:** NULL means "the payload did not report this attribute",
  0 means "reported as zero" — never conflate them in derived stats.
- **Credits:** poll `GET /api/v1/credits` directly with the user's local
  key and display the value as-is — no local decrement from broadcast
  costs (OpenRouter caches ~60s; subtracting from a stale anchor causes
  bounce-up artifacts). Cadence: on launch, on manual refresh,
  burst-grouped debounced polls ~70s after broadcast events (events
  <10s apart share one pushed timer; a gap ≥10s starts a separate
  debounce — exact rule in `tui/SPEC.md` §7; immediate polling would
  read the pre-spend cached value), and a 5-minute idle heartbeat.
  Always show the value's age next to it.

## 3. Configuration & Secrets

| Name | Where | Purpose |
|---|---|---|
| `BURNBAR_WEBHOOK_SECRET` | Supabase secret + Broadcast custom header (`X-Burnbar-Secret`) + `.env` | Sole gate on the public webhook (`verify_jwt = false` in `supabase/config.toml`) |
| `OPENROUTER_API_KEY` | Frontend-local only (macOS Keychain / `~/.config/burnbar/config.toml` / `.env` for dev scripts) | Credits polling + `scripts/test-request.sh` |
| `SUPABASE_URL`, `SUPABASE_ANON_KEY` | Frontend-local config + `.env` | PostgREST + Realtime access |

No OpenRouter **management key** exists anywhere in the system. RLS:
anon is read-only on `requests`; writes go through the service role
inside the edge function. `.env.example` (committed) documents every
variable.

---

## 4. Phases & Checklists

### Phase 0 — Scaffolding ✅
- [x] Repo scaffolding: git + public GitHub repo, `.gitignore`, MIT
  `LICENSE`, directory layout, `scripts/test-request.sh` (fires a cheap
  OpenRouter request, model slug overridable)
- [x] Supabase **cloud** project + `supabase init` + `supabase link` —
  cloud-first because Broadcast needs a publicly reachable URL; local
  Docker stack optional (offline parser testing only)

### Phase 1 — Backend ingest pipeline ✅
- [x] `.env.example` + `.env` scaffolding (all variables documented)
- [x] `ingest` skeleton: reject non-POST/PUT; constant-time
  `X-Burnbar-Secret` compare; 2xx for OpenRouter's
  `X-Test-Connection: true` probe; `verify_jwt = false`
- [x] Broadcast configured on OpenRouter (Privacy Mode ON, secret
  header); delivery verified end-to-end
- [x] 5 fixtures captured in `supabase/tests/fixtures/` (deepseek, qwen,
  kimi, claude-haiku, gpt-mini) — attribute mapping confirmed across all
  models/providers; optional attrs vary, validating the tolerant parser
- [x] Migration `20260705163715_create_requests.sql` pushed: `requests`
  table + indexes, `usage_daily` view, RLS (anon read-only), realtime
  publication, pg_cron monthly prune
- [x] Pure parser `functions/ingest/parse.ts`:
  `parseTrace(otlpJson) -> RequestRow[]` — tolerant of unknown/missing
  attributes (skip + log on missing required, never throws), both OTLP
  int encodings, `BigInt` nano-timestamps
- [x] Idempotent insert via `upsert` + `ignoreDuplicates` on
  (trace_id, span_id). Status contract: partial/malformed → 200 (parse
  what we can, log skips), **total** DB failure → 500 (log visibility
  only — OpenRouter doesn't retry either way)
- [x] 7 Deno parser tests green
  (`deno test --allow-read supabase/functions/ingest/`)
- [x] E2E verified: request → correct row in ~3s (split costs match
  OpenRouter's `cost_details` exactly), realtime INSERT received by an
  anon subscriber, view aggregates at the right grain
- [x] Verbose raw-body logging removed; final version deployed

**Definition of done (met):** an OpenRouter request produces a correct
row and a realtime event; parser tests pass via plain `deno test`.

### Phase 2 — Go TUI (first frontend, MVP target)

Fully specced in **[`tui/SPEC.md`](./tui/SPEC.md)** — design decisions,
data/state logic, error handling, and the staged implementation
checklist (Stages A–F) all live there; that file is the working
checklist for this phase. It builds against §2 of this file.

- [x] Design spec written and agreed (`tui/SPEC.md`) — all open
  questions resolved with the user 2026-07-05
- [ ] Stages A–F in `tui/SPEC.md` complete

**Definition of done:** run `burnbar` in a terminal, make an OpenRouter
request, watch the bar animate within seconds. This is the weekend-MVP
finish line together with Phase 1.

### Phase 3 — Optional audit tooling (post-MVP, may never be needed)
- [ ] Local audit script (not deployed): fetch `GET /api/v1/activity`
  (management key passed as an argument, never stored) and compare
  30-day per-model totals against `usage_daily`; report drift %, write
  nothing. Only worth building if broadcast losses turn out to be
  noticeable in practice. (Endpoint notes: `/api/v1/activity` returns
  per-(date, model, endpoint) rows; `/api/v1/analytics/query` has no
  provider dimension; neither carries unit prices, split costs, or
  cached-token counts.)

### Phase 4 — macOS menubar app (core product)
- [ ] Xcode project in `macos/`: SwiftUI `MenuBarExtra` (window style),
  macOS 14+
- [ ] Settings window: Supabase URL/anon key, OpenRouter key → Keychain
- [ ] supabase-swift: `usage_daily` fetch on launch/wake/refresh +
  Realtime subscription (mirror the TUI's data layer semantics per §2);
  in-memory state only — no local database
- [ ] Optional polish (only if launch feels slow): cache the last
  rendered snapshot to a JSON file for instant first paint; still
  re-fetch immediately
- [ ] Bars UI per README contract (proportional bars, accent1/accent2,
  timeframe picker, credits header, timestamps footer) — polished
  animations, respects light/dark mode
- [ ] Credits polling: per §2 cadence, but only **while the popover is
  open** (fetch on open, then event-debounced + 5-min heartbeat) — no
  background polling. Menubar icon shows compact state (e.g. today's
  spend) from realtime data, not credits
- [ ] Launch-at-login option; reconnect handling on wake-from-sleep
- [ ] Build/run instructions for cloners (unsigned local builds); code
  signing & notarization deferred (see §5)

### Phase 5 — Self-host docs & release polish
- [ ] `docs/SETUP.md`: create Supabase project → link → `db push` →
  deploy `ingest` → set webhook secret → configure OpenRouter Broadcast
  (Privacy Mode, header) → configure frontends. Target: a stranger
  completes it in <15 minutes
- [ ] `justfile` (or Makefile) for common tasks: db reset, deploy, test,
  build TUI
- [ ] TUI release builds (goreleaser: darwin/linux/windows)
- [ ] README final pass: screenshots/GIF of both frontends (the
  `vhs-cli-demos` skill can generate deterministic TUI captures)
- [ ] Decide macOS distribution: unsigned build vs. $99/yr Apple
  Developer notarization (user decision — ask, don't assume)

---

## 5. Live Risks

- **OTLP schema is fixture-confirmed, not documented.** OpenRouter may
  rename attributes; the parser fails soft (skip + log, never 5xx) and
  fixtures must be kept current if shapes drift.
- **`realtime-go` maturity.** Community-maintained; the 2s-polling
  fallback is pre-approved (Phase 2) if it misbehaves.
- **Credits endpoint staleness.** OpenRouter caches values up to ~60s;
  the balance display should not promise more freshness than that.
- **macOS distribution.** Unsigned apps trigger Gatekeeper warnings;
  notarization costs $99/yr. Deferred to Phase 5; user decides.

## 6. Design Decisions in Force

Constraints agreed with the user that still bind future work. (The full
dated decision log with rationale and superseded alternatives lives in
this file's git history, pre-2026-07-06.)

- **Broadcast-only architecture** — no analytics sync, no
  reconciliation, no management key anywhere. Broadcast is far richer
  than the analytics API, and webhook data can't be backfilled
  (capture-now-or-never); occasional silent loss is acceptable for a
  live meter.
- **TUI first** (validates the pipeline, stays as the cross-platform
  frontend); **macOS menubar app is the core product**.
- **Cloud-first Supabase workflow** — Broadcast needs a public URL, so
  e2e runs against the deployed function; repo stays source of truth
  via `db push` / `functions deploy`. Local Docker stack only for
  offline parser tests. The agent shell has no Supabase CLI token —
  `db push` / `functions deploy` are user-run (suggest `! <command>`).
- **Money columns are unconstrained `numeric`** — `numeric(p,s)`
  silently rounds on insert and unit prices reach ~1e-8/token; store
  payload values verbatim. Frontends may aggregate in float64 for
  display.
- **macOS app: in-memory state only** — the 30-day dataset is a few KB;
  a JSON snapshot cache is the pre-approved polish if launch feels slow.
- **Privacy Mode required in setup** — Burnbar never needs
  prompt/completion content.
- **Live layer: realtime + polling behind one `LiveSource`** (built in
  TUI Stage C). Both implementations ship; `live_source` config selects
  the mechanism. The 2s PostgREST poll is the **default** — robust and
  fully under our control; the `realtime` path (wrapping
  `supabase-community/realtime-go`) is an **opt-in** kept for when the
  library is fixed or replaced.
- **realtime-go go/no-go: NO-GO** (decided by the live spike, 2026-07-06).
  v0.1.1 (pre-v1) has a *structurally impossible* reconnect: on any
  socket close, `handleMessages()` calls `reconnect()`, which sets
  `isReconnecting=true` and then loops calling `Connect()` — but
  `Connect()` returns `"client is already reconnecting"` whenever that
  flag is set. So every retry fails instantly and it gives up after
  `MaxRetries` (5), permanently. Observed live: one Supabase
  `StatusNormalClosure` killed the feed, then 5×
  `"client is already reconnecting"` → `"Failed to reconnect after 5
  attempts"`. It also logs through `log.Default()` (stderr), which paints
  over the alt-screen TUI (bars vanished) — now neutralized by discarding
  the standard logger in `main()`. Neither defect is fixable through the
  library's public API. **Decision:** default flipped to `poll` (done);
  `realtime` remains available but is not recommended until the upstream
  reconnect is fixed or we ship our own thin Phoenix client.
