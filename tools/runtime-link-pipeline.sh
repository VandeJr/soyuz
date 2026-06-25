#!/usr/bin/env bash
# S9: full link pipeline — materialize runtime, write ll, clang link, run binary.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
OPT_FLAG=${1:-}

bash "$ROOT/tools/apply-runtime-write-plan.sh" "$TMP" >/dev/null

cat >"$TMP/out.ll" <<'EOF'
target triple = "x86_64-unknown-linux-gnu"
define i32 @main() {
entry:
  ret i32 0
}
EOF

bash "$ROOT/tools/runtime-run-link.sh" "$TMP/out.ll" "$TMP" "$TMP/app" $OPT_FLAG
"$TMP/app"
echo "→ runtime link pipeline OK"
