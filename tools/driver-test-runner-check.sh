#!/usr/bin/env bash
# S11 bootstrap gate: soyuz test test_runner.sy runs lexer tests (parser+checker via library build).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do test_runner gate" >&2
  exit 1
fi

OUT="$(soyuz test test_runner.sy 2>&1)" || {
  echo "soyuz test test_runner.sy falhou" >&2
  echo "$OUT" | tail -15 >&2
  exit 1
}

if ! echo "$OUT" | grep -Eq '✓[[:space:]]+5 testes passaram'; then
  echo "soyuz test test_runner.sy não executou exatamente 5 testes" >&2
  echo "$OUT" | tail -15 >&2
  exit 1
fi

echo "→ driver test-runner check (bootstrap) OK"
