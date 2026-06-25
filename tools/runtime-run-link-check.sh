#!/usr/bin/env bash
# S9 bootstrap gate: runtime-run-link.sh matches buildClangArgs smoke path.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/tools/runtime-materialize-plan.sh" "$TMP" >/dev/null

cat >"$TMP/out.ll" <<'EOF'
target triple = "x86_64-unknown-linux-gnu"
define i32 @main() {
entry:
  ret i32 0
}
EOF

bash "$ROOT/tools/runtime-run-link.sh" "$TMP/out.ll" "$TMP" "$TMP/smoke"
"$TMP/smoke"
echo "→ runtime run-link check (bootstrap) OK"
