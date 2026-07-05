/**
 * Burnbar `ingest` edge function — **log-only fixture-capture build**.
 *
 * @module supabase/functions/ingest
 *
 * @remarks
 * This is the Phase 1 temporary version of the webhook receiver for
 * OpenRouter's Broadcast feature. Its only job is to prove the delivery
 * pipeline (auth, test-connection probe, request handling) and to
 * `console.log` the raw OTLP payload so we can capture real fixtures —
 * OpenRouter's OTLP attribute names are under-documented, so the captured
 * payloads are the source of truth for the `requests` table shape and the
 * parser (see SPEC.md §Phase 1). **It inserts nothing into the database.**
 *
 * Request lifecycle:
 *   1. Reject any method other than POST/PUT with 405.
 *   2. Verify the shared secret in the `X-Burnbar-Secret` header against
 *      the `BURNBAR_WEBHOOK_SECRET` function secret (constant-time).
 *   3. Answer OpenRouter's `X-Test-Connection: true` probe (sent when the
 *      Broadcast destination is saved) with 200 and an empty-handed log.
 *   4. Log the raw body verbatim for fixture capture and return 200.
 *
 * Deployed with JWT verification disabled (`verify_jwt = false` in
 * supabase/config.toml) because OpenRouter cannot send a Supabase JWT —
 * the shared-secret header is the sole authentication mechanism.
 */

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
 * Handles one incoming webhook delivery (or probe) from OpenRouter.
 *
 * @param req - The incoming HTTP request.
 * @returns 200 on success/probe, 401 on bad secret, 405 on bad method,
 *   500 if the function is deployed without its secret configured.
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

  // Fixture capture: log the payload verbatim, exactly as received.
  // The logged length lets us detect log-viewer truncation when copying.
  const rawBody = await req.text();
  console.log(`Received payload: content-type=${req.headers.get("content-type")}, bytes=${rawBody.length}`);
  console.log(rawBody);

  return jsonResponse(200, { ok: true });
}

Deno.serve(handleRequest);
