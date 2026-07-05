/**
 * Unit tests for the OTLP parser, driven by the real captured Broadcast
 * payloads in `supabase/tests/fixtures/`.
 *
 * @module supabase/functions/ingest/parse_test
 *
 * @remarks
 * Run with `deno test --allow-read supabase/functions/ingest/` — the
 * parser is pure, so no Supabase stack, network, or database is needed.
 * `--allow-read` is only for loading the fixture files.
 *
 * Coverage per SPEC.md Phase 1: happy path for every captured model,
 * missing required attribute → skip, multi-span payloads, empty/malformed
 * payloads, and missing nullable attributes → `null` columns.
 */

import { assert, assertEquals, assertStrictEquals } from "jsr:@std/assert@1";
import { parseTrace, type RequestRow } from "./parse.ts";

/** The five captured fixtures — one per model/provider combination. */
const FIXTURES = [
  "claude-haiku-4.5.json",
  "deepseek-v4-flash.json",
  "gpt-5.4-mini.json",
  "kimi-k2.7-code.json",
  "qwen3-coder-next.json",
] as const;

/**
 * Loads and parses one captured fixture payload.
 *
 * @param name - Fixture filename inside `supabase/tests/fixtures/`.
 * @returns The parsed JSON payload, typed loosely on purpose — the parser
 *   must accept `unknown`.
 */
async function loadFixture(name: string): Promise<unknown> {
  const url = new URL(`../../tests/fixtures/${name}`, import.meta.url);
  return JSON.parse(await Deno.readTextFile(url));
}

/**
 * Returns a deep copy of a fixture with one span attribute removed.
 *
 * @param payload - A parsed fixture payload.
 * @param key - Attribute key to delete from every span.
 * @returns The mutated deep copy; the original is untouched.
 */
function withoutAttribute(payload: unknown, key: string): unknown {
  const copy = structuredClone(payload) as {
    resourceSpans: { scopeSpans: { spans: { attributes: { key: string }[] }[] }[] }[];
  };
  for (const rs of copy.resourceSpans) {
    for (const ss of rs.scopeSpans) {
      for (const span of ss.spans) {
        span.attributes = span.attributes.filter((attr) => attr.key !== key);
      }
    }
  }
  return copy;
}

Deno.test("every fixture parses into exactly one complete row", async (t) => {
  for (const fixture of FIXTURES) {
    await t.step(fixture, async () => {
      const rows = parseTrace(await loadFixture(fixture));
      assertEquals(rows.length, 1, `${fixture} should yield one row`);
      const row = rows[0];

      // Not-null core columns.
      assert(row.trace_id.length > 0, "trace_id");
      assert(row.span_id.length > 0, "span_id");
      assert(row.model.includes("/"), "model should be an author/name slug");
      assert(row.input_tokens > 0, "input_tokens");
      assert(row.output_tokens > 0, "output_tokens");
      assert(row.cost_usd > 0, "cost_usd");
      assert(!Number.isNaN(Date.parse(row.requested_at)), "requested_at is ISO");

      // §4-confirmed finding: every mapped attribute is present in all
      // five fixtures, so none of the nullable columns should be null here.
      for (const key of [
        "author",
        "provider",
        "provider_slug",
        "cached_tokens",
        "reasoning_tokens",
        "input_cost_usd",
        "output_cost_usd",
        "input_unit_price",
        "output_unit_price",
        "duration_ms",
      ] as const satisfies readonly (keyof RequestRow)[]) {
        assert(row[key] !== null, `${key} should be reported in ${fixture}`);
      }

      // Internal consistency.
      assert(row.reasoning_tokens! <= row.output_tokens, "reasoning ⊆ output");
      assert(row.cached_tokens! <= row.input_tokens, "cached ⊆ input");
      assert(row.duration_ms! > 0, "duration_ms positive");
    });
  }
});

Deno.test("deepseek fixture maps every column to the exact §4 values", async () => {
  const [row] = parseTrace(await loadFixture("deepseek-v4-flash.json"));
  assertEquals(row, {
    trace_id: "8ebcf0a15806fbd08fd19acea5a0c443",
    span_id: "8ebcf0a15806fbd0",
    model: "deepseek/deepseek-v4-flash",
    author: "deepseek",
    provider: "Novita",
    provider_slug: "novita/fp8",
    input_tokens: 25,
    output_tokens: 236,
    cached_tokens: 0, // reported zero — NOT null (§4 semantics)
    reasoning_tokens: 101,
    cost_usd: 0.00006958,
    input_cost_usd: 0.0000035,
    output_cost_usd: 0.00006608,
    input_unit_price: 1.4e-7,
    output_unit_price: 2.8e-7,
    // 1783220991321000000 ns → 1783220991321 ms, via BigInt (exceeds 2^53 as ns).
    requested_at: new Date(1783220991321).toISOString(),
    duration_ms: 3366, // (1783220994687 − 1783220991321) ms
  } satisfies RequestRow);
});

Deno.test("span missing a required attribute is skipped, not thrown", async () => {
  const payload = withoutAttribute(
    await loadFixture("deepseek-v4-flash.json"),
    "gen_ai.usage.total_cost",
  );
  assertEquals(parseTrace(payload), []);
});

Deno.test("missing nullable attributes produce null columns, row still parses", async () => {
  let payload = await loadFixture("deepseek-v4-flash.json");
  for (const key of [
    "gen_ai.provider.name",
    "trace.metadata.openrouter.provider_name",
    "trace.metadata.openrouter.provider_slug",
    "gen_ai.usage.input_tokens.cached",
    "gen_ai.usage.output_tokens.reasoning",
    "gen_ai.usage.input_cost",
    "gen_ai.usage.output_cost",
    "trace.metadata.openrouter.input_unit_price",
    "trace.metadata.openrouter.output_unit_price",
  ]) {
    payload = withoutAttribute(payload, key);
  }
  // Also drop the span end time → duration_ms must be null.
  const clone = structuredClone(payload) as {
    resourceSpans: { scopeSpans: { spans: Record<string, unknown>[] }[] }[];
  };
  delete clone.resourceSpans[0].scopeSpans[0].spans[0].endTimeUnixNano;

  const [row] = parseTrace(clone);
  assertStrictEquals(row.author, null);
  assertStrictEquals(row.provider, null);
  assertStrictEquals(row.provider_slug, null);
  assertStrictEquals(row.cached_tokens, null);
  assertStrictEquals(row.reasoning_tokens, null);
  assertStrictEquals(row.input_cost_usd, null);
  assertStrictEquals(row.output_cost_usd, null);
  assertStrictEquals(row.input_unit_price, null);
  assertStrictEquals(row.output_unit_price, null);
  assertStrictEquals(row.duration_ms, null);
  // Core columns unaffected.
  assertEquals(row.model, "deepseek/deepseek-v4-flash");
  assertEquals(row.input_tokens, 25);
});

Deno.test("multi-span payload yields one row per span, skipping only bad spans", async () => {
  const base = structuredClone(await loadFixture("deepseek-v4-flash.json")) as {
    resourceSpans: { scopeSpans: { spans: Record<string, unknown>[] }[] }[];
  };
  const spans = base.resourceSpans[0].scopeSpans[0].spans;
  const second = structuredClone(spans[0]);
  second.spanId = "ffffffffffffffff";
  const broken = structuredClone(spans[0]);
  delete broken.traceId; // required → this one must be skipped
  spans.push(second, broken);

  const rows = parseTrace(base);
  assertEquals(rows.length, 2);
  assertEquals(rows[0].span_id, "8ebcf0a15806fbd0");
  assertEquals(rows[1].span_id, "ffffffffffffffff");
});

Deno.test("empty and malformed payloads yield no rows and no throw", () => {
  assertEquals(parseTrace({}), []);
  assertEquals(parseTrace(null), []);
  assertEquals(parseTrace([]), []);
  assertEquals(parseTrace("not otlp"), []);
  assertEquals(parseTrace({ resourceSpans: "nope" }), []);
  assertEquals(parseTrace({ resourceSpans: [{ scopeSpans: [{ spans: [null, 42] }] }] }), []);
});

Deno.test("string-encoded intValue (canonical OTLP int64) is accepted", async () => {
  const clone = structuredClone(await loadFixture("deepseek-v4-flash.json")) as {
    resourceSpans: {
      scopeSpans: {
        spans: { attributes: { key: string; value: Record<string, unknown> }[] }[];
      }[];
    }[];
  };
  for (const attr of clone.resourceSpans[0].scopeSpans[0].spans[0].attributes) {
    if (typeof attr.value.intValue === "number") {
      attr.value.intValue = String(attr.value.intValue);
    }
  }
  const [row] = parseTrace(clone);
  assertEquals(row.input_tokens, 25);
  assertEquals(row.output_tokens, 236);
  assertEquals(row.reasoning_tokens, 101);
});
