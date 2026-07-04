#!/usr/bin/env bash
#
# test-request.sh — fire a cheap OpenRouter chat completion to generate
# traffic for Burnbar testing (each request triggers a Broadcast webhook
# delivery once Broadcast is configured).
#
# Usage:
#   OPENROUTER_API_KEY=sk-or-... ./scripts/test-request.sh [model-slug]
#
#   model-slug  Optional OpenRouter model to hit. Defaults to a cheap
#               DeepSeek chat model. Pass different slugs to capture
#               OTLP payloads from different models/providers, e.g.:
#                 ./scripts/test-request.sh openai/gpt-4o-mini
#                 ./scripts/test-request.sh google/gemini-2.5-flash-lite
#
# The API key is read from the environment only — never hardcode it and
# never commit it. You can also put it in a gitignored .env file and run:
#   set -a; source .env; set +a; ./scripts/test-request.sh

set -euo pipefail

# Fail fast with a clear message if the key is missing.
: "${OPENROUTER_API_KEY:?Set OPENROUTER_API_KEY in your environment (see usage comment)}"

MODEL="${1:-deepseek/deepseek-chat}"

echo "→ Sending test request to model: ${MODEL}"

RESPONSE="$(curl --silent --show-error --fail-with-body \
  https://openrouter.ai/api/v1/chat/completions \
  -H "Authorization: Bearer ${OPENROUTER_API_KEY}" \
  -H "Content-Type: application/json" \
  -d @- <<JSON
{
  "model": "${MODEL}",
  "messages": [
    {
      "role": "user",
      "content": "In one short sentence: what does the 'const' keyword do in JavaScript?"
    }
  ],
  "max_tokens": 60
}
JSON
)"

# Pretty-print a compact summary when jq is available; otherwise dump raw JSON.
if command -v jq >/dev/null 2>&1; then
  echo "${RESPONSE}" | jq '{
    id,
    model,
    answer: .choices[0].message.content,
    usage
  }'
else
  echo "${RESPONSE}"
fi

echo "✓ Done. If Broadcast is configured, an OTLP trace should hit the ingest function within a few seconds."
