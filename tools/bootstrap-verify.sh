#!/usr/bin/env bash
# S12: fixed-point bootstrap verification (incremental).
# Step 1: library verify message matches bootstrap soyuz vs standalone main.sy (vN).
# Step 2: test_runner success marker matches bootstrap soyuz vs standalone main.sy (vN).
# Step 3: hello_minimal.sy run output matches bootstrap soyuz vs standalone main.sy (vN).
# Step 4: hello IR linkable markers match canonical export template vs bootstrap codegen IR.
# Step 5: template and bootstrap hello IR both link with runtime and print hello.
# Step 6: vN (standalone output) rebuilds main.sy into executable vN+1 with same CLI smoke.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v soyuz >/dev/null 2>&1; then
  echo "soyuz bootstrap não encontrado no PATH" >&2
  exit 1
fi

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do bootstrap-verify" >&2
  exit 1
fi

OUT="$ROOT/output"
rm -f "$OUT"

if ! soyuz build main.sy 2>&1; then
  echo "soyuz build main.sy falhou" >&2
  exit 1
fi

if [[ ! -x "$OUT" ]]; then
  echo "binário output ausente após soyuz build main.sy" >&2
  exit 1
fi

MARKER="verificada com sucesso"

BOOTSTRAP_OUT="$(soyuz build 2>&1 || true)"
STANDALONE_OUT="$("$OUT" build 2>&1 || true)"

if ! grep -q "$MARKER" <<<"$BOOTSTRAP_OUT"; then
  echo "bootstrap soyuz build sem mensagem de verify: $BOOTSTRAP_OUT" >&2
  exit 1
fi

if ! grep -q "$MARKER" <<<"$STANDALONE_OUT"; then
  echo "standalone output build sem mensagem de verify: $STANDALONE_OUT" >&2
  exit 1
fi

echo "→ bootstrap-verify library fixed-point (S12 step 1) OK"

TEST_MARKER="testes passaram"

BOOTSTRAP_TEST="$(soyuz test test_runner.sy 2>&1 || true)"
STANDALONE_TEST="$("$OUT" test test_runner.sy 2>&1 || true)"

if ! grep -q "$TEST_MARKER" <<<"$BOOTSTRAP_TEST"; then
  echo "bootstrap soyuz test sem marcador de sucesso: $BOOTSTRAP_TEST" >&2
  exit 1
fi

if ! grep -q "$TEST_MARKER" <<<"$STANDALONE_TEST"; then
  echo "standalone output test sem marcador de sucesso: $STANDALONE_TEST" >&2
  exit 1
fi

echo "→ bootstrap-verify test_runner fixed-point (S12 step 2) OK"

HELLO="$ROOT/tools/fixtures/hello_minimal.sy"
HELLO_MARKER="hello"

if [[ ! -f "$HELLO" ]]; then
  echo "fixture hello ausente: $HELLO" >&2
  exit 1
fi

BOOTSTRAP_RUN="$(soyuz run "$HELLO" 2>&1 || true)"
STANDALONE_RUN="$("$OUT" run "$HELLO" 2>&1 || true)"

if ! grep -q "$HELLO_MARKER" <<<"$BOOTSTRAP_RUN"; then
  echo "bootstrap soyuz run sem saída hello: $BOOTSTRAP_RUN" >&2
  exit 1
fi

if ! grep -q "$HELLO_MARKER" <<<"$STANDALONE_RUN"; then
  echo "standalone output run sem saída hello: $STANDALONE_RUN" >&2
  exit 1
fi

echo "→ bootstrap-verify hello run fixed-point (S12 step 3) OK"

ir_has_markers() {
  local file=$1
  local label=$2
  local markers=(
    'c"hello'
    'call i32 (i8*, ...) @printf'
    'define i32 @main'
    'ret i32 0'
  )
  for marker in "${markers[@]}"; do
    if ! grep -qF "$marker" "$file"; then
      echo "$label IR sem marcador linkável: $marker" >&2
      return 1
    fi
  done
}

VERIFY_IR_TMP="$(mktemp -d)"
trap 'rm -rf "$VERIFY_IR_TMP"' EXIT

TEMPLATE_LL="$VERIFY_IR_TMP/template.ll"
BOOTSTRAP_LL="/tmp/soyuz_debug.ll"

bash "$ROOT/tools/export-driver-hello-ir.sh" "$TEMPLATE_LL"

rm -f "$BOOTSTRAP_LL"
if ! SOYUZ_KEEP_IR=1 soyuz build "$HELLO" -o "$VERIFY_IR_TMP/bootstrap-out" >/dev/null 2>&1; then
  echo "bootstrap soyuz build hello com SOYUZ_KEEP_IR falhou" >&2
  exit 1
fi

if [[ ! -f "$BOOTSTRAP_LL" ]]; then
  echo "bootstrap IR debug ausente após SOYUZ_KEEP_IR=1" >&2
  exit 1
fi

ir_has_markers "$TEMPLATE_LL" "template export" || exit 1
ir_has_markers "$BOOTSTRAP_LL" "bootstrap codegen" || exit 1

if ! soyuz build main.sy >/dev/null 2>&1; then
  echo "restaurar output (main.sy) após hello IR capture falhou" >&2
  exit 1
fi

if [[ ! -x "$OUT" ]]; then
  echo "output ausente após restaurar main.sy" >&2
  exit 1
fi

echo "→ bootstrap-verify hello IR marker equivalence (S12 step 4) OK"

# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"

link_hello_ir_and_run() {
  local ll_src=$1
  local label=$2
  local work="$VERIFY_IR_TMP/link-$label"
  local prefix="$work/rt"
  local manifest="$work/pipeline.manifest"
  local linked="$work/hello"
  mkdir -p "$work"
  : >"$manifest"
  manifest_append_file "$manifest" "$prefix/out.ll" "$ll_src"
  bash "$ROOT/tools/runtime-export-runtime-manifest.sh" "$manifest" "$prefix" --append >/dev/null
  bash "$ROOT/tools/apply-path-index-manifest.sh" "$manifest" >/dev/null
  bash "$ROOT/tools/runtime-run-link.sh" "$prefix/out.ll" "$prefix" "$linked"
  "$linked"
}

TEMPLATE_LINK_OUT="$(link_hello_ir_and_run "$TEMPLATE_LL" "template")"
BOOTSTRAP_LINK_OUT="$(link_hello_ir_and_run "$BOOTSTRAP_LL" "bootstrap")"

if [[ "$TEMPLATE_LINK_OUT" != "$HELLO_MARKER" ]]; then
  echo "template IR link esperava '$HELLO_MARKER', obteve '$TEMPLATE_LINK_OUT'" >&2
  exit 1
fi

if [[ "$BOOTSTRAP_LINK_OUT" != "$HELLO_MARKER" ]]; then
  echo "bootstrap IR link esperava '$HELLO_MARKER', obteve '$BOOTSTRAP_LINK_OUT'" >&2
  exit 1
fi

echo "→ bootstrap-verify hello IR link output (S12 step 5) OK"

OUT2="$VERIFY_IR_TMP/output2"
rm -f "$OUT2"

REBUILD_OUT="$("$OUT" build main.sy -o "$OUT2" 2>&1 || true)"
if ! grep -q 'Build concluído' <<<"$REBUILD_OUT"; then
  echo "vN build main.sy falhou: $REBUILD_OUT" >&2
  exit 1
fi

if [[ ! -x "$OUT2" ]]; then
  echo "vN+1 ausente após vN build main.sy: $OUT2" >&2
  exit 1
fi

VN1_USAGE="$("$OUT2" 2>&1 || true)"
if ! grep -q 'Uso: soyuz' <<<"$VN1_USAGE"; then
  echo "vN+1 sem usage esperado: $VN1_USAGE" >&2
  exit 1
fi

VN1_LIB="$("$OUT2" build 2>&1 || true)"
if ! grep -q "$MARKER" <<<"$VN1_LIB"; then
  echo "vN+1 build library sem verify: $VN1_LIB" >&2
  exit 1
fi

echo "→ bootstrap-verify vN rebuilds vN+1 (S12 step 6) OK"
