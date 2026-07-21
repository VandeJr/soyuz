#!/usr/bin/env bash
# S9 bootstrap gate: driver stub IR manifest + runtime seed → clang link.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
STUB_LL="$TMP/stub.ll"
trap 'rm -rf "$TMP"' EXIT

# Matches src/driver/codegen.sy stubModuleIr(1).
cat >"$STUB_LL" <<'EOF'
; soyuz codegen stub (1 top-level funcs)
target triple = "x86_64-unknown-linux-gnu"
define i32 @main() {
entry:
  ret i32 0
}
EOF

# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"
: >"$MANIFEST"
manifest_append_file "$MANIFEST" "$PREFIX/out.ll" "$STUB_LL"
bash "$ROOT/tools/runtime-export-runtime-manifest.sh" "$MANIFEST" "$PREFIX" --append >/dev/null

count=$(grep -c '^===FILE===$' "$MANIFEST" || true)
if [[ "$count" -ne 14 ]]; then
  echo "esperado 14 entradas (ll + runtime), obteve $count" >&2
  exit 1
fi

bash "$ROOT/tools/apply-path-index-manifest.sh" "$MANIFEST" >/dev/null
LINKED="$TMP/app"
bash "$ROOT/tools/runtime-run-link.sh" "$PREFIX/out.ll" "$PREFIX" "$LINKED"
"$LINKED"
echo "→ driver link stub check (bootstrap) OK"
