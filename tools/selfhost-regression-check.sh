#!/usr/bin/env bash
# Known blockers that must be green before parser/checker suites enter test_runner.sy.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPILER="${SOYUZ_COMPILER:-soyuz}"
cd "$ROOT"

check() {
  local label=$1 entry=$2 log
  log=$(mktemp "/tmp/soyuz-regression.XXXXXX")
  if "$COMPILER" test "$entry" >"$log" 2>&1; then
    echo "✓ $label"
    rm -f "$log"
    return 0
  fi
  echo "✗ $label: $entry continua bloqueado" >&2
  tail -n 25 "$log" >&2
  rm -f "$log"
  return 1
}

failed=0
check "Invariantes do IR tipado" tools/fixtures/typed_ir_invariants_test.sy || failed=1
check "Expressões e funções tipadas" tools/fixtures/typed_expr_invariants_test.sy || failed=1
check "Resultados estruturados do driver" tools/fixtures/native_results_test.sy || failed=1
check "Lowering AST de expressões" tools/fixtures/typed_ast_expr_test.sy || failed=1
check "Módulo AST tipado" tools/fixtures/typed_ast_module_test.sy || failed=1
check "Parser codegen" tests/parser/parser_test.sy || failed=1
check "Checker baseline e List.slice" tests/checker/checker_test.sy || failed=1
check "Checker extern e store LLVM" tests/checker/m10_extern_test.sy || failed=1
check "Codegen de expressões e List.removeAt" src/codegen/gen_exprs_test.sy || failed=1
[[ $failed -eq 0 ]] || exit 1
echo "→ regressões de self-host passaram"
