#!/usr/bin/env bash
# S10 bootstrap gate: std/fs.sy ported and type-check smoke via soyuz build.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FS="$ROOT/std/fs.sy"

if [[ ! -f "$FS" ]]; then
  echo "stdlib fs.sy ausente: $FS" >&2
  exit 1
fi
if ! grep -q 'pub fn readFile' "$FS"; then
  echo "stdlib fs.sy sem readFile" >&2
  exit 1
fi
if ! grep -q 'pub class File' "$FS"; then
  echo "stdlib fs.sy sem File" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/fs.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi
echo "→ stdlib fs check (bootstrap) OK"
