#!/usr/bin/env bash
# S9 bootstrap gate: clang links minimal LLVM IR with soyuz runtime C sources.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if ! command -v clang >/dev/null 2>&1; then
  echo "clang não encontrado no PATH" >&2
  exit 1
fi

bash "$ROOT/tools/runtime-materialize-plan.sh" "$TMP" >/dev/null

cat >"$TMP/out.ll" <<'EOF'
; soyuz runtime link smoke test
target triple = "x86_64-unknown-linux-gnu"

define i32 @main() {
entry:
  ret i32 0
}
EOF

clang_args=("$TMP/out.ll")
for src in "$TMP"/*.c; do
  clang_args+=("$src")
done
clang_args+=(-I "$TMP" -pthread -o "$TMP/smoke")

clang "${clang_args[@]}"
"$TMP/smoke"
echo "→ runtime link smoke check (bootstrap) OK"
