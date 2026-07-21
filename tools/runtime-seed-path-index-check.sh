#!/usr/bin/env bash
# S9 bootstrap gate: seed runtime path-index manifest from soyuz-go src.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/runtime.manifest"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/tools/runtime-export-runtime-manifest.sh" "$MANIFEST" "$PREFIX" >/dev/null
count=$(grep -c '^===FILE===$' "$MANIFEST" || true)
if [[ "$count" -ne 13 ]]; then
  echo "esperado 13 entradas runtime, obteve $count" >&2
  exit 1
fi
bash "$ROOT/tools/apply-path-index-manifest.sh" "$MANIFEST" >/dev/null
for name in soyuz.h rc.c std_test.c; do
  if [[ ! -f "$PREFIX/$name" ]]; then
    echo "arquivo não materializado: $PREFIX/$name" >&2
    exit 1
  fi
done
echo "→ runtime seed path-index check (bootstrap) OK"
