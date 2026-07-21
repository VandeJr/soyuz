#!/usr/bin/env bash
# S10 bootstrap gate: std/prelude.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PRELUDE="$ROOT/std/prelude.sy"

if [[ ! -f "$PRELUDE" ]]; then
  echo "stdlib prelude.sy ausente: $PRELUDE" >&2
  exit 1
fi
if ! grep -q 'pub interface Equals' "$PRELUDE"; then
  echo "stdlib prelude.sy sem Equals" >&2
  exit 1
fi
if ! grep -q 'pub fn range(' "$PRELUDE"; then
  echo "stdlib prelude.sy sem range" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/prelude.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib prelude check (bootstrap) OK"
