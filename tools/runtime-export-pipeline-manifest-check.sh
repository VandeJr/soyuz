#!/usr/bin/env bash
# S9 bootstrap gate: export hello pipeline manifest and link via apply-manifest.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/tools/runtime-export-pipeline-manifest.sh" "$MANIFEST" "$PREFIX" >/dev/null
count=$(grep -c '^===FILE===$' "$MANIFEST" || true)
if [[ "$count" -ne 14 ]]; then
  echo "esperado 14 entradas no manifest, obteve $count" >&2
  exit 1
fi

bash "$ROOT/tools/apply-path-index-manifest.sh" "$MANIFEST" >/dev/null
LINKED="$TMP/hello"
bash "$ROOT/tools/runtime-run-link.sh" "$PREFIX/out.ll" "$PREFIX" "$LINKED"
OUT="$("$LINKED")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ runtime export-pipeline-manifest check (bootstrap) OK"
