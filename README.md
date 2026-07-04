# Burnbar - LLM cost metering app

## Introduction

This project aims to solve the problem of LLM usage cost awareness. When
using an LLM through an API key, it is easy for the costs to skyrocket
quickly, especially when working on a complex feature. AI provider
dashboards provide some measure of control, but are impractical to check
all the time. Burnbar is architected differently - it allows for convenient
live monitoring of LLM usage & its costs, as soon as the requests are made.

This is possible because of the OpenRouter Broadcast feature. The usual
analytics endpoint is not suitable, because it has an effective refresh
rate of 24 hours (you can only see data from the previous day). However,
with the Broadcast feature, OpenRouter fires an OTLP trace of every request
to a configured webhook destination as soon as the request finishes. We
point it at a Supabase Edge Function, which parses the trace and writes
request data (model, tokens, cost, timestamps) to a PostgreSQL database.
The frontends subscribe to this via Supabase Realtime, showing accurate
token / currency cost within 1-5 seconds after the request has concluded.

The practical effect is a real-time mini-dashboard that shows the most
important LLM usage metrics at a glance - accurate input / output tokens
and currency spend per model within a set timeframe, as well as the
remaining OpenRouter credit balance. The obvious limitation is that it
only works for requests funneled through OpenRouter - but it's a conscious
compromise. Other LLM providers do not support the Broadcast feature, and
their analytics APIs are either restricted or lacking, so the only way to
achieve real-time metrics would be through a proxy - but that is not ideal,
since it requires the user to reconfigure all their tools to the proxy, and
any traffic that goes to the provider directly is lost. We might consider
adding a proxy option to support other providers in the future - but since
OpenRouter is the dominant choice among developers who want to use a
variety of models with a single API key, it is our primary focus for MVP.

## Apps

- **Backend:** Supabase (Edge Function + PostgreSQL + Realtime),
  self-hosted by each user on their own (free-tier) Supabase project.
- **CLI app (first frontend):** cross-platform TUI built in Go with the
  Charm stack (Bubble Tea + Lip Gloss + Harmonica) for a fast, polished,
  animated terminal experience distributed as a single static binary.
- **macOS menubar app (core product):** native Swift & SwiftUI app
  (window view with polished UI). This is the primary daily-driver
  frontend that the project is building towards from the start.

The CLI is built first because it validates the entire backend pipeline
end-to-end with the least platform friction, and remains the
cross-platform alternative once the menubar app ships.

## UI & UX

- The core of the app is a set of progress bars that show the user's LLM
usage - each bar represents a single model and shows input tokens, output
tokens, and currency spend within the set period of time (default today).
- The period of time can be adjusted in the UI, switching between one day, one week, or one month.
- The remaining credit balance is shown in the top.
- The progress bars are relative to each other, maintaining correct
proportions to reflect how usage is spread across models.
- The app renders a progress bar for each model that has been used within the set period of time, ordered from the most to least used.
- Usage added from the most recent request is highlighted in the progress bars with a distinct color - accent1 for all requests since the window was last opened, accent2 for the most recently synced request.
- The UI includes a small section with timestamps in the bottom that show the time of the last request and time of last sync.

## Technical Considerations

- It is technically possible that some requests are not captured by the
Broadcast feature due to errors / downtime (OpenRouter documents no
delivery guarantees or retries). Burnbar therefore splits its data by
source: past days are served from OpenRouter's analytics endpoint (the
authoritative record, synced daily by a scheduled job into its own
table), while today is served live from broadcast data. Today's numbers
are best-effort and become authoritative the next day when the analytics
data lands - no diffing or reconciliation logic needed. The analytics
endpoint requires a separate management key, which is stored exclusively
as an Edge Function secret on the user's own Supabase instance and never
touches the frontends.
- To minimize monitoring latency, the data transmitted through the
WebSocket (Supabase Realtime) should be minimal (most recent request
only). Frontends fetch a small baseline (30 days of per-model daily
aggregates via a single database view) on launch or manual refresh, and
layer live request events on top in memory - the dataset is small enough
that no local database is needed.
- Burnbar only needs token counts, costs, timing, and model information -
never prompt or completion content. The setup instructions therefore
require enabling OpenRouter's **Privacy Mode** on the Broadcast
destination, so prompt/completion text never leaves OpenRouter.
- The credit balance is fetched by the frontends directly from the
OpenRouter credits API using the user's key stored locally (macOS
Keychain / local config for the CLI) - it never touches the backend.
- The webhook Edge Function is protected by a shared secret passed via a
custom header configured on the Broadcast destination.

## Project Philosophy

Burnbar is open source and built for **personal, self-hosted use only**.
It is not a commercial product and there is no hosted instance, no
accounts, and no subscriptions - by design. Every user clones the repo
and deploys the backend to their own Supabase project (the free tier is
far more than enough for a single person's usage), following the
step-by-step setup instructions provided in this repo.

This single-user, self-host-only model keeps the project radically
simple: no authentication system, no rate limiting, no multi-tenancy, no
custody of anyone else's API keys, and no hosting costs that scale with
adoption. Your usage data lives in your own database, end to end.

## Roadmap

Development is organized into phases with detailed checklists in
[SPEC.md](./SPEC.md), which serves as the working document for
implementation across sessions.
