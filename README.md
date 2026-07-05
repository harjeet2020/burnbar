# Burnbar - LLM cost metering app
See the tokens you burn in real time.

## Introduction

This project aims to solve the problem of LLM usage cost awareness. When
using an LLM through an API key, it is easy for the costs to skyrocket
quickly, especially when working on a complex feature. AI provider
dashboards provide some measure of control, but are impractical to check
all the time. Burnbar is architected differently - it allows for convenient
live monitoring of LLM usage & its costs, as soon as the requests are made.

## How it works

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
only works for requests made through OpenRouter - but it's a conscious
compromise. Other LLM providers do not support the Broadcast feature, and
their analytics APIs are either restricted or lacking. A proxy could work, but would require reconfiguring all tools to funnel requests through it. Taking these into account, we decided to work with OpenRouter exclusively - it is the developer default, after all.

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
delivery guarantees or retries). Burnbar accepts this trade-off by
design: it is a **live meter, not an accounting system**. Broadcast data
is the app's single source of truth and is kept for 30 days (matching
the app's largest timeframe window; a scheduled job prunes older rows).
The OpenRouter dashboard remains the authoritative record for billing
and long-term statistics, and the credit balance shown in the app always
reflects your true remaining credits regardless of any missed capture.
The upside of building on broadcast data exclusively is that it is far
richer than OpenRouter's analytics API: per-request timing, the actual
routed provider (including quantization variant), cached and reasoning
token counts, and the quoted unit prices are all captured.
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

## Setting up your own instance

Everything below happens once, takes about 15 minutes, and requires only
free accounts. You will need the [Supabase CLI](https://supabase.com/docs/guides/local-development/cli/getting-started)
installed, a [Supabase](https://supabase.com) account, and an
[OpenRouter](https://openrouter.ai) account.

1. **Create a Supabase project** in the [dashboard](https://supabase.com/dashboard)
   (the free tier is plenty). This is where your usage data will live.
2. **Clone this repo and configure your environment:** copy the committed
   template with `cp .env.example .env` and fill in the values following
   the comments in the file - your project URL and publishable (anon) key
   from the Supabase dashboard, an OpenRouter inference API key, and a
   fresh webhook secret generated with `openssl rand -hex 32`. The real
   `.env` is gitignored so your secrets never end up in a commit.
3. **Link the repo to your project:** `supabase login`, then
   `supabase link --project-ref <your-project-ref>` (the ref is in your
   project's dashboard URL).
4. **Push the database schema:** `supabase db push` creates the tables,
   the `usage_daily` view, row-level security policies, and the realtime
   publication.
5. **Push the server-side secret:**
   `supabase secrets set BURNBAR_WEBHOOK_SECRET=<your-generated-secret>` -
   this is what authenticates webhook deliveries from OpenRouter.
6. **Deploy the edge function:**
   `supabase functions deploy ingest`. JWT verification is
   already disabled for the webhook receiver via `supabase/config.toml`
   (OpenRouter cannot send a Supabase JWT - the secret header is the
   authentication).
7. **Connect OpenRouter Broadcast:** in
   [OpenRouter settings](https://openrouter.ai/settings/broadcast), add a
   destination pointing at
   `https://<your-project-ref>.supabase.co/functions/v1/ingest`, add a
   custom header named `X-Burnbar-Secret` with your generated secret as
   the value, and switch **Privacy Mode ON** so prompt and completion text
   never leave OpenRouter. Saving fires a test probe at your function and
   should report the connection as verified.
8. **Verify the pipeline:** make any OpenRouter request (or run
   `scripts/test-request.sh`) and watch the row appear in the `requests`
   table in your Supabase dashboard within seconds. Point a frontend at
   your instance (each reads the same values you put in `.env`) and the
   bars light up live.

## Roadmap

Development is organized into phases with detailed checklists in
[SPEC.md](./SPEC.md), which serves as the working document for
implementation across sessions.
