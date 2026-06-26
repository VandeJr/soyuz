#!/usr/bin/env bash
# S12: fixed-point bootstrap verification (incremental).
# Step 1: library verify message matches bootstrap soyuz vs standalone main.sy (vN).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v soyuz >/dev/null 2>&1; then
  echo "soyuz bootstrap não encontrado no PATH" >&2
  exit 1
fi

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do bootstrap-verify" >&2
  exit 1
fi

OUT="$ROOT/output"
rm -f "$OUT"

if ! soyuz build main.sy 2>&1; then
  echo "soyuz build main.sy falhou" >&2
  exit 1
fi

if [[ ! -x "$OUT" ]]; then
  echo "binário output ausente após soyuz build main.sy" >&2
  exit 1
fi

MARKER="verificada com sucesso"

BOOTSTRAP_OUT="$(soyuz build 2>&1 || true)"
STANDALONE_OUT="$("$OUT" build 2>&1 || true)"

if ! grep -q "$MARKER" <<<"$BOOTSTRAP_OUT"; then
  echo "bootstrap soyuz build sem mensagem de verify: $BOOTSTRAP_OUT" >&2
  exit 1
fi

if ! grep -q "$MARKER" <<<"$STANDALONE_OUT"; then
  echo "standalone output build sem mensagem de verify: $STANDALONE_OUT" >&2
  exit 1
fi

echo "→ bootstrap-verify library fixed-point (S12 step 1) OK"
