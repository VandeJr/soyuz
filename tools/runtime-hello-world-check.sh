#!/usr/bin/env bash
# S9 bootstrap gate: bootstrap codegen IR + soyuz runtime link scripts run hello-world.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/tools/runtime-export-pipeline-manifest.sh" "$MANIFEST" "$PREFIX" >/dev/null
bash "$ROOT/tools/apply-path-index-manifest.sh" "$MANIFEST" >/dev/null
LINKED="$TMP/hello"
bash "$ROOT/tools/runtime-run-link.sh" "$PREFIX/out.ll" "$PREFIX" "$LINKED"
OUT="$("$LINKED")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ runtime hello-world check (bootstrap) OK"
