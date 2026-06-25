#!/usr/bin/env bash
# S9 bootstrap gate: bootstrap codegen IR + soyuz runtime link scripts run hello-world.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HELLO="$ROOT/tools/fixtures/hello_minimal.sy"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [[ ! -f "$HELLO" ]]; then
  echo "fixture não encontrado: $HELLO" >&2
  exit 1
fi
if ! command -v soyuz >/dev/null 2>&1; then
  echo "soyuz bootstrap não encontrado no PATH" >&2
  exit 1
fi

OUT_BIN="$TMP/bootstrap-out"
SOYUZ_KEEP_IR=1 soyuz build "$HELLO" -o "$OUT_BIN" >/dev/null
if [[ ! -f /tmp/soyuz_debug.ll ]]; then
  echo "IR debug não gerado (defina SOYUZ_KEEP_IR=1 no bootstrap)" >&2
  exit 1
fi

RT="$TMP/rt"
bash "$ROOT/tools/apply-runtime-write-plan.sh" "$RT" >/dev/null
LINKED="$TMP/hello"
bash "$ROOT/tools/runtime-run-link.sh" /tmp/soyuz_debug.ll "$RT" "$LINKED"
OUT="$("$LINKED")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ runtime hello-world check (bootstrap) OK"
