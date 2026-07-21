#!/usr/bin/env bash
# S9 bootstrap gate: soyuz run hello_minimal.sy prints hello (Go bootstrap codegen today).
# Aligns with defaultBootstrapHelloRunCommand in src/driver/cli.sy.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HELLO="$ROOT/tools/fixtures/hello_minimal.sy"

if [[ ! -f "$HELLO" ]]; then
  echo "fixture não encontrado: $HELLO" >&2
  exit 1
fi
if ! command -v soyuz >/dev/null 2>&1; then
  echo "soyuz bootstrap não encontrado no PATH" >&2
  exit 1
fi

OUT="$(soyuz run "$HELLO" 2>/dev/null | tail -n 1)"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ driver bootstrap-hello-run check (bootstrap) OK"
