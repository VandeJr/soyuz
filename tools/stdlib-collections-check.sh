#!/usr/bin/env bash
# S10 bootstrap gate: std/collections.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COLLECTIONS="$ROOT/std/collections.sy"

if [[ ! -f "$COLLECTIONS" ]]; then
  echo "stdlib collections.sy ausente: $COLLECTIONS" >&2
  exit 1
fi
if ! grep -q 'pub fn range(' "$COLLECTIONS"; then
  echo "stdlib collections.sy sem range" >&2
  exit 1
fi
if ! grep -q 'pub fn rangeStep' "$COLLECTIONS"; then
  echo "stdlib collections.sy sem rangeStep" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/collections.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib collections check (bootstrap) OK"
