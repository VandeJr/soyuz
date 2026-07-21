#!/usr/bin/env bash
# S10 bootstrap gate: std/string.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STRING="$ROOT/std/string.sy"

if [[ ! -f "$STRING" ]]; then
  echo "stdlib string.sy ausente: $STRING" >&2
  exit 1
fi
if ! grep -q 'extend String' "$STRING"; then
  echo "stdlib string.sy sem extend String" >&2
  exit 1
fi
if ! grep -q 'pub class StringBuilder' "$STRING"; then
  echo "stdlib string.sy sem StringBuilder" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/string.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib string check (bootstrap) OK"
