#!/usr/bin/env bash
# S9 bootstrap gate: full-build manifest (hello codegen IR + runtime) → clang link → prints hello.
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
LINKED="$TMP/app"
bash "$ROOT/tools/runtime-run-link.sh" "$PREFIX/out.ll" "$PREFIX" "$LINKED"
OUT="$("$LINKED")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ driver full-build check (bootstrap) OK"
