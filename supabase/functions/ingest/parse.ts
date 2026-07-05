/**
 * Pure OTLP-trace → `requests`-row parser for the Burnbar webhook.
 *
 * @module supabase/functions/ingest/parse
 *
 * @remarks
 * This module is deliberately dependency-free and side-effect-free (apart
 * from `console.warn` on skipped spans) so it can be unit-tested with plain
 * `deno test` against the captured fixtures in `supabase/tests/fixtures/`
 * — no running server or database needed.
 *
 * The parser is **tolerant by contract** (SPEC.md §4): OpenRouter's OTLP
 * attribute names are confirmed by captured fixtures, not documentation,
 * so unknown attributes are ignored and missing *nullable* attributes
 * produce `null` columns. Only when one of the not-null core columns
 * (`model`, tokens, cost, `requested_at`, span identity) is missing is a
 * span logged and skipped — and even then the rest of the payload still
 * parses. The caller must never turn a partial parse into a 5xx.
 *
 * Attribute → column mapping (kept in sync with SPEC.md §4):
 *
 * | Column              | OTLP source                                        |
 * |---------------------|----------------------------------------------------|
 * | `trace_id`          | `span.traceId`                                     |
 * | `span_id`           | `span.spanId`                                      |
 * | `model`             | attr `gen_ai.request.model`                        |
 * | `author`            | attr `gen_ai.provider.name`                        |
 * | `provider`          | attr `trace.metadata.openrouter.provider_name`     |
 * | `provider_slug`     | attr `trace.metadata.openrouter.provider_slug`     |
 * | `input_tokens`      | attr `gen_ai.usage.input_tokens`                   |
 * | `output_tokens`     | attr `gen_ai.usage.output_tokens` (incl. reasoning)|
 * | `cached_tokens`     | attr `gen_ai.usage.input_tokens.cached`            |
 * | `reasoning_tokens`  | attr `gen_ai.usage.output_tokens.reasoning`        |
 * | `cost_usd`          | attr `gen_ai.usage.total_cost`                     |
 * | `input_cost_usd`    | attr `gen_ai.usage.input_cost`                     |
 * | `output_cost_usd`   | attr `gen_ai.usage.output_cost`                    |
 * | `input_unit_price`  | attr `trace.metadata.openrouter.input_unit_price`  |
 * | `output_unit_price` | attr `trace.metadata.openrouter.output_unit_price` |
 * | `requested_at`      | `span.startTimeUnixNano` (string nano-epoch)       |
 * | `duration_ms`       | `(endTimeUnixNano − startTimeUnixNano) / 1e6`      |
 */

/**
 * One normalized row destined for the `requests` table.
 *
 * @remarks
 * Field names match the table's column names exactly so the array can be
 * passed straight to a PostgREST insert. `null` means "the payload did not
 * report this attribute" — never coerced to `0`, which would mean
 * "reported as zero" (SPEC.md §4).
 */
export interface RequestRow {
  /** OTLP trace id — half of the redelivery-dedupe key. */
  trace_id: string;
  /** OTLP span id — the other half of the dedupe key. */
  span_id: string;
  /** Model alias slug (e.g. `deepseek/deepseek-v4-flash`); the UI grouping key. */
  model: string;
  /** Model author (e.g. `deepseek`), or `null` if unreported. */
  author: string | null;
  /** Display name of the provider that served the request (e.g. `Novita`). */
  provider: string | null;
  /** Provider slug incl. deployment variant (e.g. `novita/fp8`). */
  provider_slug: string | null;
  /** Prompt tokens billed for the request. */
  input_tokens: number;
  /** Completion tokens billed — **includes** reasoning tokens. */
  output_tokens: number;
  /** Input tokens served from provider cache, or `null` if unreported. */
  cached_tokens: number | null;
  /** Reasoning share of `output_tokens`, or `null` if unreported. */
  reasoning_tokens: number | null;
  /** Total request cost in USD. */
  cost_usd: number;
  /** Input-side cost in USD (reflects cache discounts), or `null`. */
  input_cost_usd: number | null;
  /** Output-side cost in USD, or `null`. */
  output_cost_usd: number | null;
  /** Quoted USD per input token, or `null`. */
  input_unit_price: number | null;
  /** Quoted USD per output token, or `null`. */
  output_unit_price: number | null;
  /** Span start time as an ISO-8601 string (UTC); the day-bucketing key. */
  requested_at: string;
  /** Wall-clock duration in whole milliseconds, or `null` if the span had no end time. */
  duration_ms: number | null;
}

/**
 * Shape of one OTLP attribute value union as OpenRouter serializes it.
 *
 * @remarks
 * Captured fixtures show `intValue` as a JSON number, but the canonical
 * OTLP JSON encoding serializes 64-bit ints as *strings* — {@link toNumber}
 * accepts both so an upstream encoding change cannot break ingestion.
 */
interface OtlpAttributeValue {
  stringValue?: unknown;
  intValue?: unknown;
  doubleValue?: unknown;
}

/**
 * Coerces an OTLP scalar (JSON number or string-encoded number) to a
 * finite `number`.
 *
 * @param raw - The raw `intValue` / `doubleValue` payload field.
 * @returns The finite number, or `null` when absent or unparseable.
 */
function toNumber(raw: unknown): number | null {
  if (typeof raw === "number" && Number.isFinite(raw)) return raw;
  if (typeof raw === "string" && raw.trim() !== "") {
    const parsed = Number(raw);
    if (Number.isFinite(parsed)) return parsed;
  }
  return null;
}

/**
 * Flattens a span's `attributes` array into a key → value-union lookup.
 *
 * @param rawAttributes - The span's `attributes` field, if any.
 * @returns Map from attribute key to its OTLP value union; malformed
 *   entries are silently ignored.
 */
function attributeMap(rawAttributes: unknown): Map<string, OtlpAttributeValue> {
  const map = new Map<string, OtlpAttributeValue>();
  if (!Array.isArray(rawAttributes)) return map;
  for (const entry of rawAttributes) {
    if (
      entry !== null && typeof entry === "object" &&
      typeof (entry as { key?: unknown }).key === "string" &&
      (entry as { value?: unknown }).value !== null &&
      typeof (entry as { value?: unknown }).value === "object"
    ) {
      map.set(
        (entry as { key: string }).key,
        (entry as { value: OtlpAttributeValue }).value,
      );
    }
  }
  return map;
}

/**
 * Reads a string attribute.
 *
 * @param attrs - Flattened attribute lookup for one span.
 * @param key - Attribute key to read.
 * @returns The string value, or `null` when absent or not a string.
 */
function attrString(attrs: Map<string, OtlpAttributeValue>, key: string): string | null {
  const value = attrs.get(key)?.stringValue;
  return typeof value === "string" ? value : null;
}

/**
 * Reads a numeric attribute, accepting either OTLP numeric encoding.
 *
 * @param attrs - Flattened attribute lookup for one span.
 * @param key - Attribute key to read.
 * @returns The number, or `null` when absent or unparseable.
 */
function attrNumber(attrs: Map<string, OtlpAttributeValue>, key: string): number | null {
  const union = attrs.get(key);
  if (union === undefined) return null;
  const fromInt = toNumber(union.intValue);
  if (fromInt !== null) return fromInt;
  return toNumber(union.doubleValue);
}

/**
 * Parses an OTLP nano-epoch into a `BigInt`.
 *
 * @remarks
 * Nanosecond epochs (~1.8e18) exceed `Number.MAX_SAFE_INTEGER` (2^53), so
 * they must never pass through a JS `number` — OTLP serializes them as
 * strings for exactly this reason.
 *
 * @param raw - `startTimeUnixNano` / `endTimeUnixNano` as delivered.
 * @returns The nano-epoch, or `null` when absent or malformed.
 */
function toNanoEpoch(raw: unknown): bigint | null {
  if (typeof raw !== "string" || raw.trim() === "") return null;
  try {
    const nanos = BigInt(raw);
    return nanos > 0n ? nanos : null;
  } catch {
    return null;
  }
}

/**
 * Converts a nano-epoch to an ISO-8601 UTC string for a `timestamptz`
 * column (millisecond precision — sub-ms has no consumer, SPEC.md §9).
 *
 * @param nanos - Nanoseconds since the Unix epoch.
 * @returns ISO-8601 timestamp string.
 */
function nanosToIso(nanos: bigint): string {
  return new Date(Number(nanos / 1_000_000n)).toISOString();
}

/**
 * Parses a single OTLP span into a {@link RequestRow}.
 *
 * @param span - One raw span object from the payload.
 * @returns The normalized row, or `null` when a not-null core column is
 *   missing (the span is logged and skipped per the §4 validation contract).
 */
function parseSpan(span: Record<string, unknown>): RequestRow | null {
  const attrs = attributeMap(span.attributes);

  const traceId = typeof span.traceId === "string" ? span.traceId : null;
  const spanId = typeof span.spanId === "string" ? span.spanId : null;
  const model = attrString(attrs, "gen_ai.request.model");
  const inputTokens = attrNumber(attrs, "gen_ai.usage.input_tokens");
  const outputTokens = attrNumber(attrs, "gen_ai.usage.output_tokens");
  const costUsd = attrNumber(attrs, "gen_ai.usage.total_cost");
  const startNanos = toNanoEpoch(span.startTimeUnixNano);

  // Validation contract: these back not-null columns — missing any one
  // means the span can't produce a valid row. Skip it, never throw.
  if (
    traceId === null || spanId === null || model === null ||
    inputTokens === null || outputTokens === null || costUsd === null ||
    startNanos === null
  ) {
    const missing = [
      traceId === null && "traceId",
      spanId === null && "spanId",
      model === null && "gen_ai.request.model",
      inputTokens === null && "gen_ai.usage.input_tokens",
      outputTokens === null && "gen_ai.usage.output_tokens",
      costUsd === null && "gen_ai.usage.total_cost",
      startNanos === null && "startTimeUnixNano",
    ].filter(Boolean).join(", ");
    console.warn(`Skipping span (missing required: ${missing})`);
    return null;
  }

  const endNanos = toNanoEpoch(span.endTimeUnixNano);
  const durationMs = endNanos !== null && endNanos >= startNanos
    ? Number((endNanos - startNanos) / 1_000_000n)
    : null;

  return {
    trace_id: traceId,
    span_id: spanId,
    model,
    author: attrString(attrs, "gen_ai.provider.name"),
    provider: attrString(attrs, "trace.metadata.openrouter.provider_name"),
    provider_slug: attrString(attrs, "trace.metadata.openrouter.provider_slug"),
    input_tokens: inputTokens,
    output_tokens: outputTokens,
    cached_tokens: attrNumber(attrs, "gen_ai.usage.input_tokens.cached"),
    reasoning_tokens: attrNumber(attrs, "gen_ai.usage.output_tokens.reasoning"),
    cost_usd: costUsd,
    input_cost_usd: attrNumber(attrs, "gen_ai.usage.input_cost"),
    output_cost_usd: attrNumber(attrs, "gen_ai.usage.output_cost"),
    input_unit_price: attrNumber(attrs, "trace.metadata.openrouter.input_unit_price"),
    output_unit_price: attrNumber(attrs, "trace.metadata.openrouter.output_unit_price"),
    requested_at: nanosToIso(startNanos),
    duration_ms: durationMs,
  };
}

/**
 * Parses a full OTLP Broadcast payload into `requests` rows.
 *
 * @remarks
 * Walks `resourceSpans[] → scopeSpans[] → spans[]` — a single delivery may
 * carry multiple spans. Every structural level is checked defensively: a
 * payload of the wrong shape yields `[]`, never a throw, so the webhook
 * can always acknowledge with a 2xx on partial or malformed data.
 *
 * @param otlpJson - The already-`JSON.parse`d webhook body.
 * @returns One row per parseable span; unparseable spans are logged and
 *   skipped.
 */
export function parseTrace(otlpJson: unknown): RequestRow[] {
  const rows: RequestRow[] = [];
  const resourceSpans = (otlpJson as { resourceSpans?: unknown })?.resourceSpans;
  if (!Array.isArray(resourceSpans)) return rows;

  for (const resourceSpan of resourceSpans) {
    const scopeSpans = (resourceSpan as { scopeSpans?: unknown })?.scopeSpans;
    if (!Array.isArray(scopeSpans)) continue;
    for (const scopeSpan of scopeSpans) {
      const spans = (scopeSpan as { spans?: unknown })?.spans;
      if (!Array.isArray(spans)) continue;
      for (const span of spans) {
        if (span === null || typeof span !== "object") continue;
        const row = parseSpan(span as Record<string, unknown>);
        if (row !== null) rows.push(row);
      }
    }
  }
  return rows;
}
