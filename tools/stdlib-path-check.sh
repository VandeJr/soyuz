#!/usr/bin/env bash
# S10 bootstrap gate: std/path.sy ported; codegen IR declares path string FFI (m3 subset).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PATH_MOD="$ROOT/std/path.sy"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -f "$PATH_MOD" ]]; then
  echo "stdlib path.sy ausente: $PATH_MOD" >&2
  exit 1
fi
if ! grep -q 'pub class Path' "$PATH_MOD"; then
  echo "stdlib path.sy sem Path" >&2
  exit 1
fi
if ! grep -q 'pub fn path(' "$PATH_MOD"; then
  echo "stdlib path.sy sem path()" >&2
  exit 1
fi

cd "$ROOT"
if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build falhou após port std/path.sy" >&2
  soyuz build 2>&1 | tail -5 >&2
  exit 1
fi

if [[ -d "$GO_REF/internal/codegen" ]]; then
  cd "$GO_REF"
  go test ./internal/codegen/ -run TestPathExternFnsPresent -count=1 >/dev/null
fi
echo "→ stdlib path check (bootstrap) OK"
