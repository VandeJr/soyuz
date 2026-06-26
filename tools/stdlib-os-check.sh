#!/usr/bin/env bash
# S10 bootstrap gate: std/os.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OS="$ROOT/std/os.sy"

if [[ ! -f "$OS" ]]; then
  echo "stdlib os.sy ausente: $OS" >&2
  exit 1
fi
if ! grep -q 'pub fn args()' "$OS"; then
  echo "stdlib os.sy sem args" >&2
  exit 1
fi
if ! grep -q 'pub fn getenv' "$OS"; then
  echo "stdlib os.sy sem getenv" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/os.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib os check (bootstrap) OK"
