#!/usr/bin/env bash
# Export RunBuildPlan-shaped hello manifest (validated in src/driver/run_test.sy).
# Usage: export-hello-run-manifest.sh <manifest-out> <prefix-dir>
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST=${1:?manifest output path}
PREFIX=${2:?absolute prefix for manifest file paths}

TMP="$(mktemp -d)"
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
echo "→ hello run manifest exported to $MANIFEST ($count arquivos)"
