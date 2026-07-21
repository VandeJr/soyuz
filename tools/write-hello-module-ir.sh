#!/usr/bin/env bash
# Write linkable hello-world module IR matching src/codegen/gen_emit.sy + print("hello").
set -euo pipefail

FUNC_COUNT=${1:-1}
OUT=${2:?output ll path}

cat >"$OUT" <<EOF
; soyuz module ir (funcs=${FUNC_COUNT}, blocks=1, values=2)
; ir trace:
;   block entry
target triple = "x86_64-unknown-linux-gnu"
@fmt_print_str = private unnamed_addr constant [4 x i8] c"%s\\0A\\00"
declare i32 @printf(i8*, ...)
@str.1 = private unnamed_addr constant [6 x i8] c"hello\\00"
define i32 @main() {
entry:
  %fmt = getelementptr inbounds [4 x i8], [4 x i8]* @fmt_print_str, i64 0, i64 0
  %v1 = getelementptr inbounds [6 x i8], [6 x i8]* @str.1, i64 0, i64 0
  call i32 (i8*, ...) @printf(i8* %fmt, i8* %v1)
  ret i32 0
}
EOF
