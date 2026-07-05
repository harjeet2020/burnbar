# Project Memory

## Overview

Burnbar is an open-source, self-host-only real-time LLM cost meter for
OpenRouter users: the Broadcast feature webhooks OTLP traces to a Supabase
Edge Function, and frontends (Go/Charm TUI first, then the core-product
macOS SwiftUI menubar app) render live per-model token/spend bars via
Supabase Realtime. Currently mid Phase 1 (backend ingest pipeline) —
SPEC.md is the phase-by-phase runbook.

## Current Status

Working on Phase 1 (backend ingest). The delivery pipeline is proven
end-to-end: a log-only `ingest` edge function is deployed (secret-header
auth, `verify_jwt = false` via config.toml) and OpenRouter Broadcast
(Privacy Mode ON, `X-Burnbar-Secret` header) delivers OTLP payloads to it
within seconds. Five fixtures captured; the attribute→column mapping in
SPEC §4 is confirmed across 5 models/providers. The architecture was
redesigned this session to **broadcast-only**: `analytics_daily`,
`sync-analytics`, and the management key are gone; `requests` (enriched:
author/provider/provider_slug, cached+reasoning tokens, split costs, unit
prices) is the single source of truth with 30-day retention via a monthly
pg_cron prune. Next: the real migration, then the OTLP parser +
idempotent insert + Deno tests.

## Project Roadmap

- **Now:** Phase 1 — backend ingest (migration: `requests` + `usage_daily` view + RLS + realtime + prune cron → `parseTrace` parser → idempotent insert → fixture-driven Deno tests → e2e verify)
- **Next:** Phase 2 — Go TUI (Charm stack, Realtime subscription with pre-approved polling fallback, animated per-model bars, credits header). MVP = Phase 1 + 2
- **Later:** Phase 4 — macOS menubar app (core product; Phase 3 is now just an optional post-MVP audit script, may never be built)
- **Later:** Phase 5 — self-host setup docs & release polish

## Last Session Summary — 2026-07-05

Deployed the log-only `ingest` edge function and wired OpenRouter
Broadcast end-to-end (smoke tests + live deliveries verified in function
logs); captured five OTLP fixtures that confirmed the full attribute
mapping and revealed broadcast is far richer than the analytics API.
That finding triggered a major redesign — broadcast-only, analytics
sync/table/management-key deleted, `requests` enriched and pruned monthly
at 30 days — recorded in a fully rewritten SPEC.md. Also created
`.env(.example)` scaffolding and the README self-host setup section.

## Completed Last Session

- `.env.example` (committed template) + `.env` (verified gitignored); webhook secret generated and pushed via `supabase secrets set`
- Log-only `ingest` function: 405/401 handling, constant-time secret compare, `X-Test-Connection` probe, raw-body logging with byte-count truncation detector; `[functions.ingest] verify_jwt = false` in config.toml; deployed; all 5 curl smoke tests pass
- OpenRouter Broadcast configured by user (Privacy Mode ON, `X-Burnbar-Secret`); deliveries verified in `function_edge_logs` (payloads arrive complete — no scratch table needed)
- `scripts/test-request.sh`: default model → `deepseek/deepseek-v4-flash`, new prompt, `max_tokens` 300; fired against 5 budget models
- Five fixtures in `supabase/tests/fixtures/` (deepseek, qwen, kimi, claude-haiku, gpt-mini); SPEC §4 mapping confirmed everywhere; optional attrs vary outside the mapping (audio/image); kimi fixture proves cache discounts ⇒ split-cost columns justified; claude routed to `amazon-bedrock/global` (slug suffix = deployment variant, not just quantization)
- Web-verified both OpenRouter analytics endpoints (`/activity`: per-(date, model, endpoint) incl. provider_name; `/analytics/query`: no provider dimension) — documented in Decision Log
- **Broadcast-only redesign**: dropped `analytics_daily` + `sync-analytics` + management key; `requests` = single source of truth (accepted silent-loss trade-off; credits balance + OpenRouter dashboard are financial ground truth); 30-day retention via monthly pg_cron; SPEC.md fully rewritten (new §2/§4/§5, Phase 3 → optional audit, 5 new decision-log entries) + README/.env.example consistency fixes
- README "Setting up your own instance" section (8 steps, written as-if-complete for future self-hosters)

## Up Next

- [ ] Commit this session's work (user; note: `supabase db push` step in README is still aspirational — no migration exists yet)
- [ ] Migration per SPEC §4: `requests` table, `usage_daily` view, indexes, RLS (anon read-only), realtime publication, pg_cron prune schedule; `supabase db push`
- [ ] `parseTrace(otlpJson) -> RequestRow[]` pure function (BigInt nano-timestamps, tolerant of missing optional attrs) + replace log-only body with `on conflict do nothing` insert; Deno unit tests against the 5 fixtures
- [ ] Redeploy + e2e verify: request → correct row within seconds; confirm a realtime INSERT event is received
