#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "== health =="
curl -sf "$BASE_URL/healthz" | tee /dev/stderr
echo

echo "== create suite =="
SUITE=$(curl -sf -X POST "$BASE_URL/v1/suites" \
  -H 'Content-Type: application/json' \
  -d '{"name":"smoke-suite","description":"smoke test"}')
echo "$SUITE"
SUITE_ID=$(echo "$SUITE" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)

echo "== add case =="
curl -sf -X POST "$BASE_URL/v1/suites/$SUITE_ID/cases" \
  -H 'Content-Type: application/json' \
  -d '{"name":"hello","prompt":"Reply with exactly: hello","validators":[{"type":"contains","value":"hello"}],"timeout_ms":15000}'
echo

echo "== create run (requires worker + provider) =="
echo "Submit a run manually against lmstudio or gemini once those are available."
echo "Suite ID: $SUITE_ID"
