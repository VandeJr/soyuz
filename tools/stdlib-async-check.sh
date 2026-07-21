#!/usr/bin/env bash
# S10 bootstrap gate: std/async.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ASYNC="$ROOT/std/async.sy"

if [[ ! -f "$ASYNC" ]]; then
  echo "stdlib async.sy ausente: $ASYNC" >&2
  exit 1
fi
if ! grep -q 'pub fn parallelMap' "$ASYNC"; then
  echo "stdlib async.sy sem parallelMap" >&2
  exit 1
fi
if ! grep -q 'pub fn pipeline' "$ASYNC"; then
  echo "stdlib async.sy sem pipeline" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/async.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib async check (bootstrap) OK"
