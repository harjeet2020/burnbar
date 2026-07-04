# Project Memory

## Overview

Burnbar is an open-source, self-host-only real-time LLM cost meter for
OpenRouter users: the Broadcast feature webhooks OTLP traces to a Supabase
Edge Function, and frontends (Go/Charm TUI first, then the core-product
macOS SwiftUI menubar app) render live per-model token/spend bars via
Supabase Realtime. Currently in the planning-complete, pre-implementation
phase — SPEC.md is the phase-by-phase implementation runbook.

## Current Status

Working in the planning/specification domain, now finished: the idea was
validated with external research (Broadcast webhook, analytics/credits
APIs, Supabase limits, competitor landscape), scope was cut to
self-host-only (no hosted instance, no subscriptions), and the stack was
locked (Supabase backend; Go + Bubble Tea/Lip Gloss/Harmonica TUI; Swift
SwiftUI menubar app). No code exists yet — the repo contains only
README.md, SPEC.md, and this file, and is not yet a git repository.
Next up is implementation, starting with SPEC.md Phase 0 (scaffolding)
then Phase 1 (backend ingest pipeline); Phases 1+2 together are the
weekend-MVP finish line.

## Project Roadmap

- **Now:** Phase 0–1 — repo scaffolding + backend ingest pipeline (schema, `ingest` edge function, OTLP parser, real Broadcast fixture, end-to-end verify)
- **Next:** Phase 2 — Go TUI (Charm stack, Realtime subscription with pre-approved polling fallback, animated per-model bars, credits header)
- **Later:** Phase 3 — daily reconciliation edge function against OpenRouter analytics API
- **Later:** Phase 4–5 — macOS menubar app (core product), then self-host setup docs & release polish

## Last Session Summary — 2026-07-04

Evaluated the project idea end-to-end with web research (confirmed
Broadcast→webhook viability, OTLP payload contents, Supabase pricing
traps, and a crowded-but-not-identical competitor field), then decided to
cut the hosted instance entirely and go self-host-only. Rewrote README.md
to reflect the new philosophy and stack decisions, and created SPEC.md — a
detailed phase/checklist runbook (with data model, secrets, risks with
pre-approved fallbacks, and a decision log) designed for agents to resume
work across sessions.

## Completed Last Session

- Validated technical viability via research: OpenRouter Broadcast webhook (OTLP JSON, custom auth headers, Privacy Mode), analytics endpoint (management key, 24h lag), credits API, Supabase free-tier limits
- Market research: identified CodexBar, or-observer, and other menubar trackers; confirmed the real-time per-model niche is open
- Decision: cut hosted instance — open source, personal self-hosted use only
- Decision: Go + Charm stack (Bubble Tea/Lip Gloss/Harmonica) for the TUI, built first; macOS menubar app remains the core product
- Rewrote README.md (philosophy, apps order, technical considerations: Privacy Mode, webhook secret, frontend-side credits polling)
- Created SPEC.md with 6 phases, checklists, data model, risk register, and decision log

## Up Next

- [ ] Phase 0: `git init`, `.gitignore`, MIT LICENSE, repo layout (`supabase/`, `tui/`, `macos/`, `docs/`), `supabase init` + verify local `supabase start`, initial commit
- [ ] Phase 1: migration for `requests` table (indexes, RLS, realtime publication) + `ingest` edge function skeleton with secret-header auth and `X-Test-Connection` handling
- [ ] Phase 1: capture a real Broadcast OTLP payload as a test fixture and document the attribute→column mapping
- [ ] Phase 1: OTLP parser as a pure tested function + idempotent insert; deploy and verify end-to-end
