# Self-Hosting Burnbar

Burnbar has no hosted instance — every user deploys their own backend to
their own free Supabase project and points their own frontend at it.
This guide walks through that setup end to end. Budget about 15 minutes;
everything here is free.

You'll need:

- A [Supabase](https://supabase.com) account (the free tier is more
  than enough for a single person's usage).
- An [OpenRouter](https://openrouter.ai) account, with an existing
  inference API key or the ability to create one.
- The [Supabase CLI](https://supabase.com/docs/guides/local-development/cli/getting-started)
  installed locally.
- `git` and a clone of this repository.

If you only want to see how the pieces fit together first, read
[`ARCHITECTURE.md`](../ARCHITECTURE.md) before starting — this guide
assumes you've at least skimmed it.

## 1. Create a Supabase project

Create a new project in the [Supabase dashboard](https://supabase.com/dashboard).
This is where your usage data will live — nowhere else. Any region is
fine; pick one close to you if it matters for latency.

## 2. Configure your local environment

From the repo root:

```bash
cp .env.example .env
```

Fill in the values `.env.example` documents:

- `SUPABASE_URL` and `SUPABASE_ANON_KEY` — from your project's dashboard
  under **Project Settings → API**. `SUPABASE_URL` looks like
  `https://<project-ref>.supabase.co`; the anon key is listed under
  **API Keys → "anon public"**.
- `OPENROUTER_API_KEY` — a regular OpenRouter inference key (**Settings →
  Keys → Create API Key**). This is used locally by
  `scripts/test-request.sh` and by frontends to poll your credit balance
  — it is never sent to your Supabase project.
- `BURNBAR_WEBHOOK_SECRET` — a fresh random value you generate yourself:

  ```bash
  openssl rand -hex 32
  ```

  This one secret is the *only* thing standing between the public
  internet and your `ingest` function, so treat it like a password. You
  will paste this same value into two other places in step 5 and step 7
  below — it must match exactly in all three.

`.env` is gitignored — your secrets never end up in a commit as long as
you don't rename or force-add it.

## 3. Link the repo to your project

```bash
supabase login
supabase link --project-ref <your-project-ref>
```

The project ref is the short ID in your project's dashboard URL
(`https://supabase.com/dashboard/project/<project-ref>`).

## 4. Push the database schema

```bash
supabase db push
```

This runs the migrations in `supabase/migrations/`, which create the
`requests` table, the `usage_daily` view, the row-level security
policies, the realtime publication, and the retention cron job — the
full schema described in [`ARCHITECTURE.md`](../ARCHITECTURE.md) §2–§6.
Nothing here needs manual SQL.

## 5. Set the webhook secret on Supabase's side

```bash
supabase secrets set BURNBAR_WEBHOOK_SECRET=<the value from step 2>
```

This is what the `ingest` function checks incoming Broadcast deliveries
against — see [`ARCHITECTURE.md`](../ARCHITECTURE.md) §4.

## 6. Deploy the edge function

```bash
supabase functions deploy ingest
```

JWT verification is already disabled for this function via
`supabase/config.toml` (`[functions.ingest] verify_jwt = false`) —
OpenRouter has no way to send a Supabase JWT, so the shared secret from
step 5 is the function's only authentication. You don't need to change
anything here; it's already configured correctly for a fresh project.

## 7. Connect OpenRouter Broadcast

In [OpenRouter's Broadcast settings](https://openrouter.ai/settings/broadcast):

1. Add a new destination pointing at:

   ```
   https://<your-project-ref>.supabase.co/functions/v1/ingest
   ```

2. Add a custom header:

   ```
   X-Burnbar-Secret: <the same value from step 2 and step 5>
   ```

3. Turn **Privacy Mode ON**. This is required, not optional — Burnbar
   never needs your prompt or completion text, and Privacy Mode is what
   guarantees that text never leaves OpenRouter's servers in the first
   place.

Saving the destination fires OpenRouter's connection-test probe at your
function immediately; it should report as verified within a few
seconds. If it doesn't, see Troubleshooting below.

## 8. Verify the pipeline

Make any request through OpenRouter — through your own app, or with the
bundled test script:

```bash
OPENROUTER_API_KEY=sk-or-... ./scripts/test-request.sh
```

(Pass a different model slug as an argument to test other models,
e.g. `./scripts/test-request.sh openai/gpt-4o-mini`.)

Within a few seconds, check the `requests` table in your Supabase
dashboard's Table Editor — you should see a new row with the model,
token counts, and cost populated. That confirms the entire path:
OpenRouter → Broadcast → `ingest` → Postgres.

## 9. Point a frontend at your instance

The Go TUI reads the same four values you already put in `.env` from its
own local config file rather than the repo's `.env` (frontends are meant
to be configured independently of the backend deploy, since they may run
on an entirely different machine):

```bash
cd tui
mkdir -p ~/.config/burnbar
cp config.example.toml ~/.config/burnbar/config.toml
```

Edit `~/.config/burnbar/config.toml` and fill in `supabase_url`,
`supabase_anon_key`, and (optionally) `openrouter_api_key` with the same
values from your `.env`. Then:

```bash
go run .
```

or build a standalone binary:

```bash
go build -o burnbar .
./burnbar
```

The bars should light up live as soon as a request lands. See
[`tui/SPEC.md`](../tui/SPEC.md) for everything the app does once it's
running.

## Troubleshooting

- **The Broadcast destination won't verify.** Double-check the URL has
  no typos and ends in `/functions/v1/ingest`, and that the
  `X-Burnbar-Secret` header value matches exactly what you ran
  `supabase secrets set` with (whitespace included — copy-paste it
  rather than retyping). You can check what's currently set with
  `supabase secrets list` (values themselves aren't shown, but you can
  confirm the key exists).
- **A test request doesn't produce a row.** Check the function's logs in
  the Supabase dashboard under **Edge Functions → ingest → Logs** — the
  function logs every rejected secret, every parse failure, and every
  insert outcome, so the reason is almost always visible there directly.
- **Rows appear but a frontend shows nothing.** Confirm
  `supabase_url`/`supabase_anon_key` in the frontend's config match the
  same project you deployed to, and that you haven't accidentally left
  a stale value from a previous project in an environment variable
  (environment variables always override the config file — see
  [`tui/SPEC.md`](../tui/SPEC.md) §7 for the precedence table).
- **Credits show `—` in the TUI.** This means `openrouter_api_key` is
  empty or invalid in your config; it's optional, so the app runs fine
  without it, but the credit balance can't be fetched without a real
  key.
- **You want to start over.** `supabase db push` and
  `supabase functions deploy ingest` are both safe to re-run — pushing
  migrations again is a no-op if nothing changed, and redeploying the
  function simply replaces it.
