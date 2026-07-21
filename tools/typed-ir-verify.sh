#!/usr/bin/env bash
# Validate representative typed LLVM modules independently of the linker.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d /tmp/soyuz-typed-ir.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT

if ! command -v clang >/dev/null 2>&1; then
  echo "clang ausente: não é possível validar LLVM IR" >&2
  exit 1
fi

COMPILER="${SOYUZ_COMPILER:-soyuz}"
RUNTIME_SRC="$ROOT/runtime/src"

run_suite() {
  local file=$1 expected=$2 output normalized
  output=$("$COMPILER" test "$file" 2>&1)
  printf '%s\n' "$output"
  normalized=$(printf '%s\n' "$output" | sed $'s/\033\[[0-9;]*m//g')
  if ! grep -Fq "✓ $expected testes passaram" <<<"$normalized"; then
    echo "contagem inesperada em $file: esperado $expected" >&2
    return 1
  fi
}

run_suite "$ROOT/tools/fixtures/typed_ir_invariants_test.sy" 4
run_suite "$ROOT/tools/fixtures/typed_expr_invariants_test.sy" 5

validate_ir() {
  local file=$1
  local syntax_log="$TMP/clang-syntax.log"
  if clang -x ir -fsyntax-only "$file" 2>"$syntax_log"; then
    return 0
  fi
  # Some Clang releases reject AST-only actions for IR. Compiling to the null
  # device performs the same parser/verifier pass without keeping an object.
  if grep -q "cannot apply AST actions to LLVM IR" "$syntax_log"; then
    clang -Wno-override-module -x ir -c "$file" -o /dev/null
    return 0
  fi
  cat "$syntax_log" >&2
  return 1
}

run_renderer_with_repo_runtime() {
  local source=$1 name=$2 generated_ir binary
  generated_ir="$TMP/$name.bootstrap.ll"
  binary="$TMP/$name.bootstrap"
  SOYUZ_KEEP_IR=1 "$COMPILER" build -o "$binary" "$source" >/dev/null
  if [[ ! -s /tmp/soyuz_debug.ll ]]; then
    echo "bootstrap não produziu IR para $source" >&2
    return 1
  fi
  cp /tmp/soyuz_debug.ll "$generated_ir"
  clang -Wno-override-module "$generated_ir" "$RUNTIME_SRC"/*.c -I "$RUNTIME_SRC" -pthread -o "$binary"
  "$binary"
}

bash "$ROOT/tools/export-driver-hello-ir.sh" "$TMP/hello.ll"
validate_ir "$TMP/hello.ll"

cat >"$TMP/typed-values.ll" <<'EOF'
target triple = "x86_64-unknown-linux-gnu"

%Pair = type { i64, i64 }
%List = type { ptr, i64, i64 }

define i64 @scalar_store_load(i64 %input) {
entry:
  %slot = alloca i64, align 8
  store i64 %input, ptr %slot, align 8
  %loaded = load i64, ptr %slot, align 8
  ret i64 %loaded
}

define i64 @struct_gep(ptr %pair) {
entry:
  %field = getelementptr inbounds %Pair, ptr %pair, i32 0, i32 1
  %value = load i64, ptr %field, align 8
  ret i64 %value
}

define i64 @list_size(ptr %list) {
entry:
  %size_ptr = getelementptr inbounds %List, ptr %list, i32 0, i32 1
  %size = load i64, ptr %size_ptr, align 8
  ret i64 %size
}

define i64 @select_value(i1 %condition, i64 %left, i64 %right) {
entry:
  br i1 %condition, label %then, label %otherwise
then:
  br label %done
otherwise:
  br label %done
done:
  %result = phi i64 [ %left, %then ], [ %right, %otherwise ]
  ret i64 %result
}
EOF
validate_ir "$TMP/typed-values.ll"

run_renderer_with_repo_runtime "$ROOT/tools/fixtures/render_typed_expr_ir.sy" typed-expr-renderer \
  | sed -n '/^target triple =/,$p' >"$TMP/generated-expressions.ll"
validate_ir "$TMP/generated-expressions.ll"
clang -Wno-override-module -x ir "$TMP/generated-expressions.ll" -o "$TMP/generated-expressions"
generated_output=$("$TMP/generated-expressions")
if [[ "$generated_output" != "answer=42 hello" ]]; then
  echo "execução do IR gerado divergiu: esperado 'answer=42 hello', recebido '$generated_output'" >&2
  exit 1
fi

run_renderer_with_repo_runtime "$ROOT/tools/fixtures/render_typed_ast_module_ir.sy" typed-ast-renderer \
  | sed -n '/^target triple =/,$p' >"$TMP/generated-ast-module.ll"
validate_ir "$TMP/generated-ast-module.ll"
clang -Wno-override-module -x ir "$TMP/generated-ast-module.ll" -o "$TMP/generated-ast-module"
ast_module_output=$("$TMP/generated-ast-module")
if [[ "$ast_module_output" != "native hello" ]]; then
  echo "módulo AST gerado produziu saída inesperada: $ast_module_output" >&2
  exit 1
fi

echo "→ LLVM IR tipado validado pelo clang (stores, tipos, funções, if, while, loop, range, break e continue)"
