# Project Memory

## Overview

Burnbar is an open-source, self-host-only real-time LLM cost meter for
OpenRouter users: the Broadcast feature webhooks OTLP traces to a Supabase
Edge Function, and frontends (Go/Charm TUI first, then the core-product
macOS SwiftUI menubar app) render live per-model token/spend bars via
Supabase Realtime. Currently at the start of implementation — SPEC.md is
the phase-by-phase runbook.

## Current Status

Working on Phase 0→1 (scaffolding → backend ingest pipeline). Repo
scaffolding is committed and pushed to a public GitHub repo; the Supabase
cloud project is created and linked (cloud-first workflow — Broadcast
needs a public URL, so local Docker is optional). The architecture was
redesigned this session: diff-based reconciliation is gone, replaced by a
source-split model (`analytics_daily` for past UTC days via a daily 01:00
UTC full-window-upsert cron, broadcast `requests` for today with 3-day
retention, merged by a `usage_daily` view with analytics-wins precedence).
Next: log-only `ingest` edge function to capture real OTLP fixtures.

## Project Roadmap

- **Now:** Phase 1 — backend ingest pipeline (log-only ingest → capture multi-model OTLP fixtures → schema migration + view → parser + idempotent insert → e2e verify)
- **Next:** Phase 2 — Go TUI (Charm stack, Realtime subscription with pre-approved polling fallback, animated per-model bars, credits header)
- **Later:** Phase 3 — `sync-analytics` daily cron (full 30-day upsert + broadcast pruning)
- **Later:** Phase 4–5 — macOS menubar app (core product, in-memory state, no SQLite), then self-host setup docs & release polish

## Last Session Summary — 2026-07-04

Scaffolded the repo (layout, .gitignore, MIT license, `scripts/test-request.sh`);
user created the public GitHub repo and the linked Supabase cloud project.
Redesigned the data architecture: replaced diff-based reconciliation with a
source-split model (analytics for past days, broadcast for today, merged in
one SQL view), verified OpenRouter analytics timing (~30 min after UTC
midnight) and credits caching (~60s) via web research, dropped SQLite from
the macOS app, and rewrote SPEC.md/README.md with the new design plus six
new decision-log entries.

## Completed Last Session

- Repo scaffolding: layout per SPEC §6, `.gitignore`, MIT LICENSE, `scripts/test-request.sh` (model slug overridable, for multi-model fixture capture); committed & pushed to public GitHub repo (user)
- Supabase cloud project created and linked via `supabase init` + `supabase link` (user); cloud-first workflow decided since Broadcast requires a public destination
- Design: source-split model replaces reconciliation — `analytics_daily` (authoritative, kept forever) + `requests` (broadcast, 3-day retention) + `usage_daily` view (`security_invoker`, analytics-wins precedence, no double counting)
- Research-verified: analytics `/api/v1/activity` covers completed UTC days, ~30 min availability after UTC midnight, buckets by request START time (matches OTLP span start); credits endpoint works with regular key, cached ~60s; no rate-limit concerns for metadata polling
- Decisions: daily 01:00 UTC full-30-day-window upsert cron (no retry logic — self-healing), management key server-side only, macOS app in-memory state (no GRDB), credits displayed as polled (60s, no local decrement; event-triggered re-poll is pre-approved polish)
- SPEC.md + README.md rewritten accordingly (new §2 diagram, full §4 SQL, Phase 1/2/3/4 checklists, risks, decision log)

## Up Next

- [ ] Commit pending SPEC.md/README.md changes + `supabase/config.toml` (user)
- [ ] Write & deploy log-only `ingest` edge function (secret-header auth, `X-Test-Connection` handling, `--no-verify-jwt`); guide user through OpenRouter Broadcast setup (Privacy Mode ON, custom secret header)
- [ ] Capture OTLP fixtures from 2–3 different models via `scripts/test-request.sh`, save to `supabase/tests/fixtures/`, compare shapes to confirm the attribute→column mapping
- [ ] Migration: `requests` + `analytics_daily` + `usage_daily` view + RLS + realtime publication, then the real OTLP parser with idempotent insert
