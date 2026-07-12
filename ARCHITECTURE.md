# Burnbar Architecture

Burnbar's backend is a small, self-contained Supabase project: one table,
one view, one edge function, and one cron job. This document describes
that backend in full — the data flow, the schema, the ingest contract,
and the query pattern every frontend follows — so it can serve as the
implementation reference for any new client (the [Go TUI](./tui/SPEC.md)
is the first; a native macOS menubar app is next). If you're setting up
your own instance rather than building against it, see
[`docs/SETUP.md`](./docs/SETUP.md) instead; for the product pitch, see
[`README.md`](./README.md).

## 1. The End-to-End Flow

```
OpenRouter Broadcast (Privacy Mode ON, X-Burnbar-Secret header)
    │  POST OTLP JSON per completed request
    ▼
Edge Function `ingest` — verify secret, parse, idempotent insert
    ▼
Postgres `requests` — single source of truth (13-month pg_cron prune)
    │
    ├─ `usage_daily` view ── PostgREST ───► frontends (baseline fetch)
    └─ Realtime `requests` INSERTs ───────► frontends (live layer)

Frontends poll OpenRouter /api/v1/credits directly (key stays local).
```

OpenRouter's **Broadcast** feature is what makes this possible: it fires
an OTLP trace of every completed request at a webhook the instant the
request finishes, rather than the analytics API's next-day refresh.
Burnbar points that webhook at a Supabase Edge Function, which parses
the trace and writes one row per request into Postgres. From there, two
independent paths serve frontends: a small daily-aggregate view for
historical context, and a Realtime subscription for anything that just
happened. Credit balance is a third, entirely separate path — frontends
hit OpenRouter's credits endpoint directly with the user's own key,
which never touches this backend at all.

Broadcast delivery is best-effort with no retries — a deliberate,
accepted tradeoff. A missed row only means a bar undercounts; it never
corrupts the credit balance or the OpenRouter dashboard, both of which
remain the actual financial ground truth. Burnbar is a **live meter, not
an accounting system**, and every design choice below follows from that.

## 2. Schema

One migration
(`supabase/migrations/20260705163715_create_requests.sql`) creates the
entire backend surface. The `requests` table is the single source of
truth — one row per OpenRouter Broadcast span:

| Column | Type | Notes |
|---|---|---|
| `trace_id`, `span_id` | `text` | Unique together; the dedupe key for idempotent inserts |
| `model` | `text` | The alias slug frontends group bars by, e.g. `deepseek/deepseek-v4-flash` |
| `author`, `provider`, `provider_slug` | `text` | Model author, routed provider display name, and provider slug including deployment variant (`novita/fp8`, `amazon-bedrock/global`) |
| `input_tokens`, `output_tokens` | `integer not null default 0` | Output includes reasoning tokens |
| `cached_tokens`, `reasoning_tokens` | `integer`, nullable | NULL means unreported, not zero |
| `cost_usd`, `input_cost_usd`, `output_cost_usd` | `numeric` | Actual settled cost, not `unit_price × tokens` — already reflects cache discounts |
| `input_unit_price`, `output_unit_price` | `numeric`, nullable | Quoted USD per token at request time |
| `requested_at` | `timestamptz not null` | OTLP span **start** time; the day-bucketing key, in UTC |
| `duration_ms` | `integer`, nullable | Wall time of the request |
| `inserted_at` | `timestamptz not null default now()` | |

Two details are load-bearing for anyone extending this schema:

- **NULL vs. 0 is a meaningful distinction, not an implementation
  accident.** NULL means the Broadcast payload didn't report that
  attribute at all; `0` means it was reported as zero. A frontend that
  conflates the two will render a false `0%` cache-hit rate for a model
  that simply never reports cache stats, instead of the honest `—`.
- **Money columns are unconstrained `numeric`, not `numeric(p,s)`.**
  `numeric(p,s)` silently rounds on insert, and OpenRouter's quoted unit
  prices reach roughly 1e-8 USD/token — precision that must survive
  intact since Broadcast data can never be backfilled (capture-now-or-
  never). Frontends are free to aggregate in float64 for display; the
  stored values themselves stay exact.

Two indexes support the query patterns in §5: `requested_at desc` for
time-range scans, and `(model, requested_at desc)` for per-model
time-range scans.

Row-level security is on, with a single policy: `anon` gets read-only
`select`. Every frontend ships this anon key, so it is treated as
effectively public — nothing sensitive is reachable through it, and all
writes happen exclusively through the edge function's service-role
client, which bypasses RLS entirely and is never exposed to a frontend.

## 3. The `usage_daily` View

```sql
create view public.usage_daily with (security_invoker = true) as
select (requested_at at time zone 'utc')::date as day,
       model, provider_slug,
       count(*) as request_count,
       sum(input_tokens) as input_tokens, sum(output_tokens) as output_tokens,
       sum(cached_tokens) as cached_tokens, sum(reasoning_tokens) as reasoning_tokens,
       sum(cost_usd) as cost_usd, sum(input_cost_usd) as input_cost_usd, sum(output_cost_usd) as output_cost_usd,
       sum(duration_ms) as duration_ms_sum, count(duration_ms) as timed_request_count
from public.requests
group by 1, 2, 3;
```

This view is the **only** surface frontends read for anything beyond
"today" — grain is (UTC day, model, provider_slug), and every column is
an additive sum. That's deliberate: 31 days of daily-per-model-per-
provider rows is a few kilobytes, cheap enough to fetch in full on every
launch, with no local database needed on any frontend. `security_invoker
= true` means the view runs with the calling role's own privileges, so
it inherits the `requests` table's RLS policy rather than needing one of
its own.

**Averages and percentages never live in this view**, and never should.
Cache-hit rate, effective $/token rate, average request duration,
provider cost share — none of these compose across days by simple
averaging. A frontend that averaged five days' worth of "average
duration" values would get a number with no defined meaning. Instead,
every ratio is **derived client-side from sums, at whatever window the
user has selected**:

- cache % = `sum(cached_tokens) / sum(input_tokens)`
- effective rate = `sum(cost_usd) / sum(tokens)`
- avg duration = `sum(duration_ms) / count(duration_ms)` (using
  `timed_request_count`, since not every row reports duration)
- provider share = one provider's row sum ÷ the model's total across
  all its provider rows

This is why the view carries sums and counts rather than pre-computed
ratios — it's the only shape that lets a client re-derive any ratio at
any window without going back to raw rows.

## 4. Ingest: the `ingest` Edge Function

`supabase/functions/ingest/index.ts` is the only write path into the
database, and the only public-facing surface of the whole backend.

**Authentication** is a single shared secret, `BURNBAR_WEBHOOK_SECRET`,
sent as a custom `X-Burnbar-Secret` header on the Broadcast destination
and compared in constant time (both sides SHA-256'd first, then XOR'd
byte-by-byte, so response timing never leaks how many characters of the
secret an attacker has guessed). There is no Supabase JWT involved —
OpenRouter cannot send one — so `verify_jwt = false` is set for this
function in `supabase/config.toml`, and the shared secret is the sole
gate. A missing or wrong secret gets a `401`; a missing server-side
secret (misconfiguration) gets a `500` and refuses everything rather
than run open.

**The connection-test probe.** When a Broadcast destination is saved or
edited, OpenRouter fires an empty request carrying
`X-Test-Connection: true` and only checks for a 2xx. The function
answers that immediately, before touching the parser or database.

**Parsing** (`supabase/functions/ingest/parse.ts`) is a pure function,
`parseTrace(otlpJson) -> RequestRow[]`, tolerant by contract: unknown or
missing OTLP attributes are skipped and logged, never thrown — the
Broadcast schema is fixture-confirmed against real payloads from several
models and providers but is not formally documented by OpenRouter, so
the parser has to survive attributes it's never seen rather than hard-
fail on them. It handles both OTLP integer encodings and BigInt
nanosecond timestamps.

**The insert is idempotent**: `upsert(...,  { onConflict:
"trace_id,span_id", ignoreDuplicates: true })`, which Postgres executes
as `on conflict do nothing`. A redelivered webhook (or a retry from a
flaky connection) can never double-count a row.

**Status-code contract:** partial or malformed data is *never* a 5xx —
the function parses what it can, logs what it can't, and returns 200,
because OpenRouter doesn't retry either way and a 5xx streak would read
as an outage in OpenRouter's own dashboard for no benefit. The one
exception is a *total* database failure (e.g., a paused project), which
returns 500 purely so it's visible in the function logs — the row is
lost regardless, since Broadcast never retries.

## 5. Realtime and the Frontend Query Pattern

`alter publication supabase_realtime add table public.requests;` — views
can't be published, so the *raw table's* INSERT events are what
frontends subscribe to. Every frontend should **subscribe before
fetching**: if a row lands mid-fetch, it may be briefly double-counted
in memory until the next refresh discards and re-aggregates, which is
the safe direction to err in — fetching first risks silently missing a
row that landed in the gap, with no self-healing mechanism for it.

The query pattern every frontend should follow:

- **For `week`/`month` (or any non-"today" window): query `usage_daily`
  for the last 31 days.** That's the entire baseline fetch — it happens
  on launch, on manual refresh, and on reconnect, and nothing else.
  `week`/`month` aggregate client-side over the current **UTC calendar
  week (Monday–Sunday) and UTC calendar month**, layering any live
  events on top. UTC-anchoring these windows (rather than the user's
  local calendar) is a deliberate tradeoff: it keeps the boundary
  aligned with `usage_daily`'s UTC-day grain, so switching timeframes
  never requires a refetch — the cost is that a week/month boundary
  might not land exactly at local midnight for the user, which is judged
  acceptable for a live meter.
- **For `today`: query the `requests` table directly, filtered to rows
  since the user's local midnight** — never the view, since view rows
  are UTC-day grain and can't be re-cut to a local-timezone boundary.
  This is a small, cheap query (at most a day's worth of rows) run
  alongside the baseline fetch.
- **Layer live Realtime INSERT events on top of both, in memory.** A
  manual refresh discards every live-layered row and re-fetches both the
  baseline and the today-slice from scratch — live state is intentionally
  disposable and never the thing being reconciled against.
- **Never average across days.** Every ratio (§3) is computed client-side
  from the sums in view at the currently selected window, recomputed on
  every timeframe switch rather than cached per-window.
- **Day bucketing always uses `requested_at`** (the OTLP span's *start*
  time), in UTC — this matches how OpenRouter's own dashboard buckets
  usage, so Burnbar's day totals should agree with what OpenRouter shows
  for the same day.

A community-maintained Realtime client (`supabase-community/realtime-go`)
was evaluated and rejected for the Go TUI: its reconnect logic was
structurally broken (a reconnect attempt set a flag that then caused
every subsequent reconnect attempt to reject itself, so *any* dropped
socket became permanent), and it logged through the standard `log`
package, which corrupted the TUI's alternate-screen rendering. The TUI
now speaks the underlying Phoenix channel protocol directly via
`github.com/nshafer/phx`, which survives reconnects and sleep/wake
cleanly. Any frontend building its own Realtime client should budget for
verifying reconnect behavior explicitly rather than trusting a library's
claims — this is the one place in the stack that turned out to need it.

## 6. Retention

A `pg_cron` job (`burnbar-prune-requests`, enabled by the migration
itself since it isn't on by default) runs monthly and deletes `requests`
rows older than **13 months**. This is decoupled from the display
windows above — Burnbar has no yearly display view, and isn't planning
one, since it's a live meter rather than an analytics product — but
keeping 13 months of raw rows around means future static analytics
(month-over-month trend views, say) could be built without first having
to re-plumb retention. Thirteen rather than twelve preserves one month
of redundancy against the cron's own monthly scheduling slack. Between
runs the table may briefly hold slightly more than 13 months of data;
every query in this document is time-bounded, so that's harmless.

## 7. Credits

Frontends fetch `GET /api/v1/credits` **directly from OpenRouter**,
using a key the user supplies locally (never sent to or stored by this
backend) and display `total_credits − total_usage` exactly as returned,
with its fetch age shown alongside it. There is no local decrementing of
the balance as requests come in: OpenRouter caches this endpoint's
response for roughly 60 seconds, and subtracting live request costs from
a stale cached anchor produces a visible "bounce" when the cache
eventually catches up. The recommended cadence — on launch, on manual
refresh, a debounced poll roughly 70 seconds after a burst of live
events (long enough to clear OpenRouter's cache), and an idle heartbeat
every few minutes otherwise — balances freshness against not hammering
an endpoint that can't return fresher data anyway. See
`tui/internal/data/credits.go` and `credits_sched.go` for the reference
implementation of that cadence.

## 8. Configuration & Secrets

| Name | Where it lives | Purpose |
|---|---|---|
| `BURNBAR_WEBHOOK_SECRET` | Supabase function secret + Broadcast's `X-Burnbar-Secret` header + local `.env` | Sole authentication for the public `ingest` webhook |
| `OPENROUTER_API_KEY` | Frontend-local only (config file, Keychain, or dev `.env`) | Credits polling + `scripts/test-request.sh`; never sent to the backend |
| `SUPABASE_URL`, `SUPABASE_ANON_KEY` | Frontend-local config | PostgREST + Realtime access |

No OpenRouter *management* key exists anywhere in this system — only a
regular inference key, used solely to poll the user's own credit
balance. `.env.example` documents every backend-side variable; each
frontend documents its own local config separately (see
[`tui/SPEC.md` §7](./tui/SPEC.md) for the TUI's `config.toml`).

## 9. Design Constraints Worth Knowing Before You Extend This

A few decisions here aren't obvious from the schema alone, and are easy
to accidentally undo while adding a feature:

- **Broadcast-only, on purpose.** There's no reconciliation against
  OpenRouter's analytics API and no plan to add one. Broadcast data is
  far richer than the analytics endpoints (per-request timing, actual
  routed provider and quantization variant, cached/reasoning token
  counts, quoted unit prices — none of which the analytics API exposes),
  and webhook data can't be backfilled after the fact, so capturing it
  well the first time matters more than reconciling it later. Occasional
  silent loss from a missed webhook delivery is an accepted cost of that
  tradeoff, not an oversight.
- **Self-hosted, single-user, by design — not a limitation to fix.**
  There's no auth system, no multi-tenancy, and no hosted instance
  because every user runs their own Supabase project. This is what keeps
  the whole backend to one migration and one function.
- **The Supabase workflow is cloud-first.** Broadcast requires a
  publicly reachable URL, so there's no meaningful local-only dev loop
  for the ingest path; the repo stays the source of truth via `db push`
  and `functions deploy` against a real linked project. A local Docker
  stack only helps for offline parser unit tests.
- **Privacy Mode is required, not optional**, on the Broadcast
  destination — Burnbar has no use for prompt or completion content, and
  the setup instructions insist on it so that content never leaves
  OpenRouter in the first place.
