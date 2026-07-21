#!/usr/bin/env bash
# Exercise source -> lexer -> parser -> checker -> typed LLVM -> native execution.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d /tmp/soyuz-source-pipeline.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT

COMPILER="${SOYUZ_COMPILER:-soyuz}"
RENDERER="$TMP/render-source-ir"
GENERATED_IR="$TMP/generated-source.ll"
GENERATED_BIN="$TMP/generated-source"

SOYUZ_KEEP_IR=1 "$COMPILER" build -o "$RENDERER" \
  "$ROOT/tools/fixtures/render_typed_source_ir.sy" >/dev/null

set +e
"$RENDERER" >"$GENERATED_IR"
renderer_status=$?
set -e
if [[ $renderer_status -ne 0 ]]; then
  echo "source-pipeline: frontend autoportado falhou ao executar (exit $renderer_status)" >&2
  echo "bloqueio conhecido: o bootstrap ainda emite retorno nulo para funções que retornam Node heap (parseExpression)" >&2
  exit 1
fi

if ! sed -n '/^target triple =/,$p' "$GENERATED_IR" >"$TMP/normalized.ll" || \
   [[ ! -s "$TMP/normalized.ll" ]]; then
  echo "source-pipeline: renderer não produziu LLVM" >&2
  exit 1
fi

clang -Wno-override-module -x ir "$TMP/normalized.ll" -o "$GENERATED_BIN"
output=$("$GENERATED_BIN")
if [[ "$output" != "source native" ]]; then
  echo "source-pipeline: saída divergente; esperado 'source native', recebido '$output'" >&2
  exit 1
fi

echo "source-pipeline: fonte -> frontend -> LLVM -> execução validada"
