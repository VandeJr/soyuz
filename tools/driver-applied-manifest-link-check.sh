#!/usr/bin/env bash
# S9 bootstrap gate: manifest apply → verify paths on disk → clang link → prints hello.
# Aligns with helloFullBuildAppliedLinkPlanReady in src/driver/export_test.sy.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
HELLO_LL="$TMP/hello.ll"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/tools/export-driver-hello-ir.sh" "$HELLO_LL"

# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"
: >"$MANIFEST"
manifest_append_file "$MANIFEST" "$PREFIX/out.ll" "$HELLO_LL"
bash "$ROOT/tools/runtime-export-runtime-manifest.sh" "$MANIFEST" "$PREFIX" --append >/dev/null

count=$(grep -c '^===FILE===$' "$MANIFEST" || true)
if [[ "$count" -ne 14 ]]; then
  echo "esperado 14 entradas (ll + runtime), obteve $count" >&2
  exit 1
fi

bash "$ROOT/tools/apply-path-index-manifest.sh" "$MANIFEST" >/dev/null

applied_count=$(manifest_verify_applied_paths "$MANIFEST")
if [[ "$applied_count" -ne 14 ]]; then
  echo "esperado 14 arquivos materializados, obteve $applied_count" >&2
  exit 1
fi

if ! grep -q 'c"hello' "$PREFIX/out.ll"; then
  echo "out.ll sem literal c\"hello" >&2
  exit 1
fi

LINKED="$TMP/app"
bash "$ROOT/tools/runtime-run-link.sh" "$PREFIX/out.ll" "$PREFIX" "$LINKED"
OUT="$("$LINKED")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ driver applied-manifest-link check (bootstrap) OK"
