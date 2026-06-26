#!/usr/bin/env bash
# S10 bootstrap gate: std/error.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ERROR="$ROOT/std/error.sy"

if [[ ! -f "$ERROR" ]]; then
  echo "stdlib error.sy ausente: $ERROR" >&2
  exit 1
fi
if ! grep -q 'pub interface Error' "$ERROR"; then
  echo "stdlib error.sy sem interface Error" >&2
  exit 1
fi
if ! grep -q 'pub fn noneError' "$ERROR"; then
  echo "stdlib error.sy sem noneError" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/error.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib error check (bootstrap) OK"
