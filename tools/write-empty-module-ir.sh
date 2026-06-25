#!/usr/bin/env bash
# Write empty-trace module IR matching src/codegen/gen_emit.sy renderModuleIr.
set -euo pipefail

FUNC_COUNT=${1:-1}
OUT=${2:?output ll path}

cat >"$OUT" <<EOF
; soyuz module ir (funcs=${FUNC_COUNT}, blocks=0, values=0)
; ir trace:
target triple = "x86_64-unknown-linux-gnu"
define i32 @main() {
entry:
  ret i32 0
}
EOF
