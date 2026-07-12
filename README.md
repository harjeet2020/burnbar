# Burnbar

**Watch the tokens you burn, live.**

Burnbar is a real-time cost meter for LLM usage through
[OpenRouter](https://openrouter.ai) — a set of proportional bars,
updating within seconds of a request finishing, showing exactly what
you're spending and on which model, right now.

## The problem

Working with LLM APIs, it's easy to lose track of cost until it's
already a surprise. Provider dashboards exist, but checking one means
tabbing away from your terminal or editor mid-flow — and most of them
only refresh usage data once a day, so the number you'd see is already
stale by the time you look. That gap matters more than it used to: a
coding agent looping through tool calls, or a long agentic session
fanning out dozens of requests per minute, can rack up real cost in the
time it takes to context-switch to a browser tab and back.

Burnbar closes that gap by putting the meter where you're already
looking — a small, always-visible window that updates itself, so cost
awareness costs you nothing.

## How it works

OpenRouter has a feature most tooling doesn't use yet: **Broadcast**,
which pushes a detailed trace of every request to a webhook the moment
it completes, instead of making you poll a once-a-day analytics
endpoint. Burnbar points that webhook at a small Supabase backend —
one edge function, one Postgres table — which parses each trace and
writes it to a database in a couple of seconds. Frontends subscribe to
that database over a realtime connection, so a bar updates within
seconds of the request that fed it actually finishing.

```
Your OpenRouter usage → Broadcast → Supabase → live in your terminal
```

The full backend design — schema, ingest contract, retention, and
exactly how a frontend should query it — is written up in
[`ARCHITECTURE.md`](./ARCHITECTURE.md). The short version: it's
self-hosted per-user, holds only what's needed to show cost and usage
(never prompt or completion content), and is small enough to read in
one sitting.

## What you get

- One bar per model you've used in the current window, sized
  proportionally to cost (or token volume, toggled on demand) so you
  can see at a glance where the money is actually going.
- A live highlight on whatever just landed — the meter doesn't just
  eventually catch up, it visibly reacts.
- Your remaining OpenRouter credit balance, shown alongside its own
  freshness.
- A per-model drill-down: request count, average duration, cache-hit
  rate, and a breakdown by the actual provider OpenRouter routed you to
  (including which quantization variant).
- Today / this week / this month, switchable instantly with no
  reload — it's already sitting in memory.

## What it isn't

- **Not an accounting system.** Broadcast delivery has no retry
  guarantee, so an occasional request can go uncounted. Burnbar accepts
  that tradeoff deliberately — it's a live meter, not a billing ledger.
  Your OpenRouter dashboard remains the source of truth for actual
  billing, and your credit balance always reflects it exactly, no
  matter what the bars show.
- **Not multi-provider.** It works with OpenRouter specifically, because
  Broadcast is what makes near-real-time tracking possible at all — no
  other provider offers an equivalent, and a proxy-based approach would
  mean reconfiguring every tool you use to point through it. Given that
  OpenRouter is already most people's default gateway to everything else,
  this felt like the right place to draw the line rather than a limitation
  to work around.
- **Not a hosted product.** There's no Burnbar cloud service, no
  account to make, no subscription. You deploy your own backend to your
  own free-tier Supabase project, and your usage data never leaves it.

## Frontends

- **CLI (available now)** — a fast, animated terminal app built in Go
  with the [Charm](https://charm.land) stack (Bubble Tea, Lip Gloss,
  Harmonica), distributed as a single static binary. Full behavior is
  documented in [`tui/SPEC.md`](./tui/SPEC.md).
- **macOS menubar app (in progress)** — a native SwiftUI app that lives
  in your menu bar, meant as the primary daily-driver frontend. This is
  the direction the project is building toward; the CLI exists first
  because it validates the entire backend end to end with the least
  platform friction, and remains the cross-platform option once the
  menubar app ships.

## Try it

Getting a live meter running takes two parts: deploying your own tiny
backend, and running a frontend against it.

1. Follow [`docs/SETUP.md`](./docs/SETUP.md) to deploy the backend to
   your own Supabase project and connect OpenRouter Broadcast — about
   15 minutes, entirely on free tiers.
2. Run the CLI:

   ```bash
   cd tui
   go run .
   ```

   (or `go build -o burnbar . && ./burnbar` for a standalone binary).
   It reads its config from `~/.config/burnbar/config.toml` — see
   [`tui/config.example.toml`](./tui/config.example.toml) and
   [`docs/SETUP.md`](./docs/SETUP.md) for what goes in it.

Make a request through OpenRouter and watch the bar move.

## Project philosophy

Burnbar is open source (MIT-licensed) and built for personal,
self-hosted use — not as a commercial product. There's no account
system, no rate limiting, no multi-tenancy, and no custody of anyone's
API keys but your own, because every user runs their own copy end to
end. That constraint is what keeps the whole thing simple enough to
actually read and trust.

## Further reading

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — the full backend design:
  schema, ingest contract, realtime, and the query pattern any frontend
  should follow.
- [`docs/SETUP.md`](./docs/SETUP.md) — step-by-step self-hosting guide.
- [`tui/SPEC.md`](./tui/SPEC.md) — what the terminal app does and how,
  in detail.
