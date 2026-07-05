-- Burnbar backend schema (SPEC.md §4–§5).
--
-- One migration creates the entire backend surface: the `requests` table
-- (single source of truth, capturing everything the OpenRouter Broadcast
-- payload offers — webhooks cannot be backfilled), the `usage_daily`
-- aggregate view (the frontends' only read surface), row-level security,
-- the realtime publication, and the 30-day retention cron.

-- ---------------------------------------------------------------------------
-- requests: one row per OpenRouter Broadcast span (= one LLM request).
--
-- Nullable vs `not null default 0` is deliberate: NULL means "the payload
-- did not report this attribute", 0 means "reported as zero". The not-null
-- core columns double as the parser's validation contract — a span missing
-- one of those is logged and skipped, never inserted.
--
-- Money columns are unconstrained numeric on purpose: numeric(p,s) silently
-- rounds on insert, and unit prices reach ~1e-8 USD/token, so payload
-- values are stored verbatim (capture-now-or-never).
-- ---------------------------------------------------------------------------
create table public.requests (
  id                bigint generated always as identity primary key,
  trace_id          text not null,
  span_id           text not null,
  model             text not null,           -- UI grouping key (alias slug, e.g. "deepseek/deepseek-v4-flash")
  author            text,                    -- model author ("deepseek")
  provider          text,                    -- routed provider display name ("Novita")
  provider_slug     text,                    -- incl. deployment variant ("novita/fp8", "amazon-bedrock/global")
  input_tokens      integer not null default 0,
  output_tokens     integer not null default 0, -- includes reasoning tokens
  cached_tokens     integer,
  reasoning_tokens  integer,
  cost_usd          numeric not null default 0,
  input_cost_usd    numeric,
  output_cost_usd   numeric,
  input_unit_price  numeric,                 -- quoted USD per input token
  output_unit_price numeric,
  requested_at      timestamptz not null,    -- OTLP span START time (day-bucketing key, UTC)
  duration_ms       integer,
  inserted_at       timestamptz not null default now(),
  unique (trace_id, span_id)                 -- dedupe on webhook redelivery
);

-- Time-range filtering (window queries, polling fallback, prune).
create index requests_requested_at_idx on public.requests (requested_at desc);
-- Per-model time-range queries (equality on model, range on time).
create index requests_model_time_idx on public.requests (model, requested_at desc);

-- ---------------------------------------------------------------------------
-- Row-level security: the anon key ships inside the frontends and is
-- effectively public, so anon gets read-only access and nothing else.
-- Writes happen exclusively through the edge function's service role,
-- which bypasses RLS. Realtime respects these policies too.
-- ---------------------------------------------------------------------------
alter table public.requests enable row level security;

create policy "anon can read requests"
  on public.requests
  for select
  to anon
  using (true);

-- ---------------------------------------------------------------------------
-- usage_daily: the frontends' single read surface — per-day, per-model,
-- PER-PROVIDER aggregates. Grain includes provider_slug because OpenRouter
-- routes one model across providers with different rates; the split powers
-- the details screen.
--
-- ADDITIVE SUMS ONLY: averages/percentages don't compose across days, so
-- all ratios (cache %, reasoning %, effective rates, avg duration,
-- provider share) are derived client-side from these sums at the selected
-- window. Live bars select the narrow columns and sum across provider
-- rows; the details screen selects everything.
--
-- security_invoker makes the view run with the caller's privileges, so it
-- enforces the RLS policy on the underlying table.
-- ---------------------------------------------------------------------------
create view public.usage_daily with (security_invoker = true) as
select (requested_at at time zone 'utc')::date as day,
       model,
       provider_slug,
       count(*)              as request_count,
       sum(input_tokens)     as input_tokens,
       sum(output_tokens)    as output_tokens,
       sum(cached_tokens)    as cached_tokens,
       sum(reasoning_tokens) as reasoning_tokens,
       sum(cost_usd)         as cost_usd,
       sum(input_cost_usd)   as input_cost_usd,
       sum(output_cost_usd)  as output_cost_usd,
       sum(duration_ms)      as duration_ms_sum,
       count(duration_ms)    as timed_request_count -- rows that reported a duration (for avg)
from public.requests
group by 1, 2, 3;

-- ---------------------------------------------------------------------------
-- Realtime: publish INSERTs on `requests` (views cannot be published; the
-- per-row event payload satisfies the minimal-WebSocket requirement and
-- drives the accent highlight UX).
-- ---------------------------------------------------------------------------
alter publication supabase_realtime add table public.requests;

-- ---------------------------------------------------------------------------
-- Retention: the app never looks back more than 30 days (older stats live
-- in the OpenRouter dashboard). Monthly prune at 03:00 UTC on the 1st;
-- between runs the table may hold up to ~60 days of rows — harmless, all
-- queries are time-bounded. pg_cron ships with Supabase but is not enabled
-- by default, so the migration enables it itself (self-hosters never touch
-- the dashboard).
-- ---------------------------------------------------------------------------
create extension if not exists pg_cron;

select cron.schedule(
  'burnbar-prune-requests',
  '0 3 1 * *',
  $$ delete from public.requests
     where requested_at < now() - interval '30 days' $$
);
