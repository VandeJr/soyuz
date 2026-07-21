#!/usr/bin/env bash
# S10 bootstrap gate: feature-tests/pipes.sy type-checks and builds via bootstrap soyuz.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/feature-tests/pipes.sy"
OUT="$(mktemp)"
trap 'rm -f "$OUT"' EXIT

if [[ ! -f "$SRC" ]]; then
  echo "feature test ausente: $SRC" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do feature test" >&2
  exit 1
fi

if ! soyuz build "$SRC" -o "$OUT" >/dev/null 2>&1; then
  echo "soyuz build falhou em feature-tests/pipes.sy" >&2
  soyuz build "$SRC" -o "$OUT" 2>&1 | tail -8 >&2
  exit 1
fi
echo "→ stdlib feature pipes check (bootstrap) OK"
