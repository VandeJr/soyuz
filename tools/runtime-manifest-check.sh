#!/usr/bin/env bash
# S9 bootstrap gate: apply path-index manifest and clang-link minimal binary.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
MANIFEST="$TMP/pipeline.manifest"
trap 'rm -rf "$TMP"' EXIT

cat >"$MANIFEST" <<EOF
===FILE===
$TMP/out.ll
===BODY===
target triple = "x86_64-unknown-linux-gnu"
define i32 @main() {
entry:
  ret i32 0
}
===END===
===FILE===
$TMP/rc.c
===BODY===
/* manifest smoke */
===END===
EOF

bash "$ROOT/tools/apply-path-index-manifest.sh" "$MANIFEST" >/dev/null
bash "$ROOT/tools/apply-runtime-write-plan.sh" "$TMP" >/dev/null
bash "$ROOT/tools/runtime-run-link.sh" "$TMP/out.ll" "$TMP" "$TMP/smoke"
"$TMP/smoke"
echo "→ runtime manifest check (bootstrap) OK"
