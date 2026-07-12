/**
 * Burnbar `ingest` edge function — the OpenRouter Broadcast webhook
 * receiver.
 *
 * @module supabase/functions/ingest
 *
 * @remarks
 * OpenRouter's Broadcast feature POSTs an OTLP JSON trace here after every
 * completed LLM request. The function authenticates the delivery, parses
 * it with {@link parseTrace}, and idempotently inserts the resulting rows
 * into the `requests` table (redeliveries dedupe on `(trace_id, span_id)`).
 *
 * Request lifecycle:
 *   1. Reject any method other than POST/PUT with 405.
 *   2. Verify the shared secret in the `X-Burnbar-Secret` header against
 *      the `BURNBAR_WEBHOOK_SECRET` function secret (constant-time).
 *   3. Answer OpenRouter's `X-Test-Connection: true` probe (sent when the
 *      Broadcast destination is saved) with 200.
 *   4. Parse the OTLP payload and upsert rows with duplicates ignored.
 *
 * Status-code contract (ARCHITECTURE.md §4): partial or malformed data is
 * **never** a 5xx — parse what we can, log the rest, return 200 (OpenRouter
 * doesn't retry, so failing loudly buys nothing and a 5xx streak looks like
 * an outage). Only a *total* database failure (e.g. paused project) returns
 * 500, purely so it stands out in the function logs.
 *
 * Deployed with JWT verification disabled (`verify_jwt = false` in
 * supabase/config.toml) because OpenRouter cannot send a Supabase JWT —
 * the shared-secret header is the sole authentication mechanism.
 */

import { createClient, type SupabaseClient } from "jsr:@supabase/supabase-js@2";
import { parseTrace } from "./parse.ts";

/** Custom header carrying the shared webhook secret, configured on the OpenRouter Broadcast destination. */
const SECRET_HEADER = "x-burnbar-secret";

/** Header OpenRouter sets (value `"true"`) on the connection-test probe fired when saving a Broadcast destination. */
const TEST_CONNECTION_HEADER = "x-test-connection";

/**
 * Builds a small JSON response.
 *
 * @param status - HTTP status code to send.
 * @param body - JSON-serializable payload.
 * @returns The assembled {@link Response}.
 */
function jsonResponse(status: number, body: Record<string, unknown>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

/**
 * Compares two secret strings in constant time.
 *
 * @remarks
 * Direct string comparison (`a === b`) short-circuits on the first
 * mismatching character, which in principle leaks how much of the secret
 * an attacker has guessed (a timing attack). Hashing both values first
 * gives fixed-length inputs, and the XOR loop always touches every byte,
 * so comparison time is independent of where the values differ.
 *
 * @param expected - The secret configured on the server.
 * @param provided - The secret supplied by the caller.
 * @returns `true` only if both values are identical.
 */
async function secretsMatch(expected: string, provided: string): Promise<boolean> {
  const encoder = new TextEncoder();
  const [expectedDigest, providedDigest] = await Promise.all([
    crypto.subtle.digest("SHA-256", encoder.encode(expected)),
    crypto.subtle.digest("SHA-256", encoder.encode(provided)),
  ]);

  const a = new Uint8Array(expectedDigest);
  const b = new Uint8Array(providedDigest);
  let difference = 0;
  for (let i = 0; i < a.length; i++) {
    difference |= a[i] ^ b[i];
  }
  return difference === 0;
}

/**
 * Lazily-created service-role Supabase client, reused across invocations
 * of a warm function instance.
 *
 * @remarks
 * The edge runtime auto-injects `SUPABASE_URL` and
 * `SUPABASE_SERVICE_ROLE_KEY` into every deployed function. The service
 * role bypasses RLS — which is exactly right here: `requests` only has a
 * read-only `anon` policy, and this function is the sole writer. The key
 * never leaves the server.
 */
let supabase: SupabaseClient | null = null;

/**
 * Returns the shared service-role client, creating it on first use.
 *
 * @returns The client, or `null` when the runtime env vars are missing
 *   (only plausible in a misconfigured local run).
 */
function getSupabase(): SupabaseClient | null {
  if (supabase !== null) return supabase;
  const url = Deno.env.get("SUPABASE_URL");
  const serviceRoleKey = Deno.env.get("SUPABASE_SERVICE_ROLE_KEY");
  if (!url || !serviceRoleKey) return null;
  supabase = createClient(url, serviceRoleKey, {
    auth: { persistSession: false },
  });
  return supabase;
}

/**
 * Handles one incoming webhook delivery (or probe) from OpenRouter.
 *
 * @param req - The incoming HTTP request.
 * @returns 200 on success/probe/partial data, 401 on bad secret, 405 on
 *   bad method, 500 on missing configuration or total database failure.
 */
async function handleRequest(req: Request): Promise<Response> {
  // OpenRouter Broadcast delivers via POST; PUT is tolerated per spec.
  if (req.method !== "POST" && req.method !== "PUT") {
    return jsonResponse(405, { error: "method not allowed" });
  }

  const expectedSecret = Deno.env.get("BURNBAR_WEBHOOK_SECRET");
  if (!expectedSecret) {
    // Deployment misconfiguration — refuse everything rather than run open.
    console.error("BURNBAR_WEBHOOK_SECRET is not set; rejecting request");
    return jsonResponse(500, { error: "server misconfigured" });
  }

  const providedSecret = req.headers.get(SECRET_HEADER);
  if (!providedSecret || !(await secretsMatch(expectedSecret, providedSecret))) {
    console.warn(
      `Rejected request: ${providedSecret ? "wrong" : "missing"} ${SECRET_HEADER} header`,
    );
    return jsonResponse(401, { error: "unauthorized" });
  }

  // OpenRouter fires this probe (empty payload) when the Broadcast
  // destination is saved; it only checks for a 2xx.
  if (req.headers.get(TEST_CONNECTION_HEADER) === "true") {
    console.log("Answered X-Test-Connection probe");
    return jsonResponse(200, { ok: true, probe: true });
  }

  const rawBody = await req.text();
  console.log(`Received payload: content-type=${req.headers.get("content-type")}, bytes=${rawBody.length}`);

  // Malformed JSON → acknowledge and log; a 5xx would achieve nothing
  // (OpenRouter doesn't retry) and partial data must never fail the hook.
  let payload: unknown;
  try {
    payload = JSON.parse(rawBody);
  } catch {
    console.warn("Payload is not valid JSON; nothing to ingest");
    return jsonResponse(200, { ok: true, parsed: 0, inserted: 0 });
  }

  const rows = parseTrace(payload);
  if (rows.length === 0) {
    console.warn("Payload contained no parseable spans");
    return jsonResponse(200, { ok: true, parsed: 0, inserted: 0 });
  }

  const db = getSupabase();
  if (db === null) {
    console.error("SUPABASE_URL / SUPABASE_SERVICE_ROLE_KEY missing; cannot insert");
    return jsonResponse(500, { error: "server misconfigured" });
  }

  // Idempotent insert: `ignoreDuplicates` is PostgREST's spelling of
  // `on conflict (trace_id, span_id) do nothing`, so webhook redelivery
  // can never double-count.
  const { error, count } = await db
    .from("requests")
    .upsert(rows, {
      onConflict: "trace_id,span_id",
      ignoreDuplicates: true,
      count: "exact",
    });

  if (error) {
    // Total DB failure — the one case that returns 5xx, purely for log
    // visibility (the row is lost either way; broadcast has no retries).
    console.error(`Insert failed: ${error.message}`);
    return jsonResponse(500, { error: "insert failed" });
  }

  const inserted = count ?? rows.length;
  console.log(`Ingested ${inserted}/${rows.length} span(s) (duplicates ignored)`);
  return jsonResponse(200, { ok: true, parsed: rows.length, inserted });
}

Deno.serve(handleRequest);
