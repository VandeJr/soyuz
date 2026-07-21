#!/usr/bin/env bash
# S9 bootstrap gate: export hello run manifest → apply → link → prints hello.
# Aligns with helloRunBuildManifestRoundTripReady in src/driver/export_test.sy.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
LINKED="$TMP/app"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/tools/export-hello-run-manifest.sh" "$MANIFEST" "$PREFIX"

# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"
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

bash "$ROOT/tools/runtime-run-link.sh" "$PREFIX/out.ll" "$PREFIX" "$LINKED"
OUT="$("$LINKED")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ driver run-manifest-round-trip check (bootstrap) OK"
