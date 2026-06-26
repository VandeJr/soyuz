#!/usr/bin/env bash
# S12: fixed-point bootstrap verification (incremental).
# Step 1: library verify message matches bootstrap soyuz vs standalone main.sy (vN).
# Step 2: test_runner success marker matches bootstrap soyuz vs standalone main.sy (vN).
# Step 3: hello_minimal.sy run output matches bootstrap soyuz vs standalone main.sy (vN).
# Step 4: hello IR linkable markers match canonical export template vs bootstrap codegen IR.
# Step 5: template and bootstrap hello IR both link with runtime and print hello.
# Step 6: vN (standalone output) rebuilds main.sy into executable vN+1 with same CLI smoke.
# Step 7: vN+1 matches bootstrap for library, test_runner, and hello run fixed-points.
# Step 8: vN+1 rebuilds main.sy into executable vN+2 with same CLI smoke.
# Step 9: vN+2 passes test_runner and hello run fixed-points.
# Step 10: vN, vN+1, vN+2 share the same standalone binary size (weak binary equivalence).
# Step 11: vN, vN+1, vN+2 share the same ELF section layout (.text/.rodata/.data/.bss).
# Step 12: vN, vN+1, vN+2 share the same defined symbol table (nm T/t fingerprint).
# Step 13: vN, vN+1, vN+2 share the same .data section content hash.
# Step 14: main.sy delegates via cliOsExecShell; vN..vN+2 export soyuz_os_exec (bootstrap contract).
# Step 15: vN..vN+2 embed bootstrap delegate command strings (soyuz build/test/run).
# Step 16: standalone library build does not shell out to bootstrap soyuz (fake PATH).
# Step 17: standalone legacy build (non-main.sy) does not shell out to bootstrap soyuz (fake PATH).
# Step 18: standalone `new` does not shell out to bootstrap soyuz (fake PATH).
# Step 19: standalone `test` does not shell out to bootstrap soyuz (fake PATH).
# Step 20: standalone `run` does not shell out to bootstrap soyuz (fake PATH).
# Step 21: only `build main.sy` still shells out to bootstrap soyuz (fake PATH).
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

VN1_TEST="$("$OUT2" test test_runner.sy 2>&1 || true)"
if ! grep -q "$TEST_MARKER" <<<"$VN1_TEST"; then
  echo "vN+1 test sem marcador de sucesso: $VN1_TEST" >&2
  exit 1
fi

VN1_RUN="$("$OUT2" run "$HELLO" 2>&1 || true)"
if ! grep -q "$HELLO_MARKER" <<<"$VN1_RUN"; then
  echo "vN+1 run sem saída hello: $VN1_RUN" >&2
  exit 1
fi

echo "→ bootstrap-verify vN+1 fixed-point (S12 step 7) OK"

OUT3="$VERIFY_IR_TMP/output3"
rm -f "$OUT3"

REBUILD2_OUT="$("$OUT2" build main.sy -o "$OUT3" 2>&1 || true)"
if ! grep -q 'Build concluído' <<<"$REBUILD2_OUT"; then
  echo "vN+1 build main.sy falhou: $REBUILD2_OUT" >&2
  exit 1
fi

if [[ ! -x "$OUT3" ]]; then
  echo "vN+2 ausente após vN+1 build main.sy: $OUT3" >&2
  exit 1
fi

VN2_USAGE="$("$OUT3" 2>&1 || true)"
if ! grep -q 'Uso: soyuz' <<<"$VN2_USAGE"; then
  echo "vN+2 sem usage esperado: $VN2_USAGE" >&2
  exit 1
fi

VN2_LIB="$("$OUT3" build 2>&1 || true)"
if ! grep -q "$MARKER" <<<"$VN2_LIB"; then
  echo "vN+2 build library sem verify: $VN2_LIB" >&2
  exit 1
fi

echo "→ bootstrap-verify vN+1 rebuilds vN+2 (S12 step 8) OK"

VN2_TEST="$("$OUT3" test test_runner.sy 2>&1 || true)"
if ! grep -q "$TEST_MARKER" <<<"$VN2_TEST"; then
  echo "vN+2 test sem marcador de sucesso: $VN2_TEST" >&2
  exit 1
fi

VN2_RUN="$("$OUT3" run "$HELLO" 2>&1 || true)"
if ! grep -q "$HELLO_MARKER" <<<"$VN2_RUN"; then
  echo "vN+2 run sem saída hello: $VN2_RUN" >&2
  exit 1
fi

echo "→ bootstrap-verify vN+2 fixed-point (S12 step 9) OK"

VN_SIZE=$(stat -c%s "$OUT")
VN1_SIZE=$(stat -c%s "$OUT2")
VN2_SIZE=$(stat -c%s "$OUT3")

if [[ "$VN_SIZE" != "$VN1_SIZE" ]] || [[ "$VN1_SIZE" != "$VN2_SIZE" ]]; then
  echo "tamanhos de binário divergem: vN=$VN_SIZE vN+1=$VN1_SIZE vN+2=$VN2_SIZE" >&2
  exit 1
fi

echo "→ bootstrap-verify generation binary size equivalence (S12 step 10) OK"

if ! command -v readelf >/dev/null 2>&1; then
  echo "readelf não encontrado no PATH" >&2
  exit 1
fi

elf_section_fingerprint() {
  local file=$1
  readelf -S "$file" 2>/dev/null | awk '/\.(text|rodata|data|bss) / {printf "%s:%s ", $2, $5}'
}

VN_ELF="$(elf_section_fingerprint "$OUT")"
VN1_ELF="$(elf_section_fingerprint "$OUT2")"
VN2_ELF="$(elf_section_fingerprint "$OUT3")"

if [[ "$VN_ELF" != "$VN1_ELF" ]] || [[ "$VN1_ELF" != "$VN2_ELF" ]]; then
  echo "layout ELF diverge: vN=[$VN_ELF] vN+1=[$VN1_ELF] vN+2=[$VN2_ELF]" >&2
  exit 1
fi

echo "→ bootstrap-verify generation ELF section equivalence (S12 step 11) OK"

if ! command -v nm >/dev/null 2>&1; then
  echo "nm não encontrado no PATH" >&2
  exit 1
fi

elf_defined_sym_fingerprint() {
  local file=$1
  nm "$file" 2>/dev/null | awk '/ [Tt] / {print $3}' | sort -u | sha256sum | awk '{print $1}'
}

for label in vN vN+1 vN+2; do
  case "$label" in
    vN) bin="$OUT" ;;
    vN+1) bin="$OUT2" ;;
    vN+2) bin="$OUT3" ;;
  esac
  if ! nm "$bin" 2>/dev/null | awk '$3=="main" && / [Tt] /' | grep -q .; then
    echo "$label sem símbolo main definido: $bin" >&2
    exit 1
  fi
done

VN_SYM="$(elf_defined_sym_fingerprint "$OUT")"
VN1_SYM="$(elf_defined_sym_fingerprint "$OUT2")"
VN2_SYM="$(elf_defined_sym_fingerprint "$OUT3")"

if [[ "$VN_SYM" != "$VN1_SYM" ]] || [[ "$VN1_SYM" != "$VN2_SYM" ]]; then
  echo "símbolos definidos divergem entre gerações" >&2
  exit 1
fi

echo "→ bootstrap-verify generation symbol equivalence (S12 step 12) OK"

if ! command -v objcopy >/dev/null 2>&1; then
  echo "objcopy não encontrado no PATH" >&2
  exit 1
fi

elf_section_content_hash() {
  local file=$1
  local section=$2
  objcopy -O binary --only-section="$section" "$file" /dev/stdout 2>/dev/null | sha256sum | awk '{print $1}'
}

VN_DATA="$(elf_section_content_hash "$OUT" ".data")"
VN1_DATA="$(elf_section_content_hash "$OUT2" ".data")"
VN2_DATA="$(elf_section_content_hash "$OUT3" ".data")"

if [[ -z "$VN_DATA" ]] || [[ -z "$VN1_DATA" ]] || [[ -z "$VN2_DATA" ]]; then
  echo "hash da seção .data ausente em alguma geração" >&2
  exit 1
fi

if [[ "$VN_DATA" != "$VN1_DATA" ]] || [[ "$VN1_DATA" != "$VN2_DATA" ]]; then
  echo "conteúdo .data diverge entre gerações" >&2
  exit 1
fi

echo "→ bootstrap-verify generation .data section equivalence (S12 step 13) OK"

MAIN_SRC="$ROOT/main.sy"
DELEGATE_MARKER="cliOsExecShell"
EXEC_SYMBOL="soyuz_os_exec"

if [[ ! -f "$MAIN_SRC" ]]; then
  echo "main.sy ausente: $MAIN_SRC" >&2
  exit 1
fi

if ! grep -q "$DELEGATE_MARKER" "$MAIN_SRC"; then
  echo "main.sy sem delegação $DELEGATE_MARKER ao bootstrap" >&2
  exit 1
fi

for label in vN vN+1 vN+2; do
  case "$label" in
    vN) bin="$OUT" ;;
    vN+1) bin="$OUT2" ;;
    vN+2) bin="$OUT3" ;;
  esac
  if ! nm "$bin" 2>/dev/null | awk -v sym="$EXEC_SYMBOL" '$3==sym && / [Tt] /' | grep -q .; then
    echo "$label sem símbolo $EXEC_SYMBOL definido: $bin" >&2
    exit 1
  fi
done

echo "→ bootstrap-verify bootstrap delegation contract (S12 step 14) OK"

BOOTSTRAP_CMD_MARKERS=(
  'soyuz build'
  'soyuz test'
  'soyuz run'
)

binary_has_embedded_string() {
  local file=$1
  local needle=$2
  if strings "$file" 2>/dev/null | grep -Fq "$needle"; then
    return 0
  fi
  grep -aFq "$needle" "$file" 2>/dev/null
}

GENERATION_BINS=("$OUT" "$OUT2" "$OUT3")
GENERATION_LABELS=(vN vN+1 vN+2)

for i in 0 1 2; do
  bin="${GENERATION_BINS[$i]}"
  label="${GENERATION_LABELS[$i]}"
  for marker in "${BOOTSTRAP_CMD_MARKERS[@]}"; do
    if ! binary_has_embedded_string "$bin" "$marker"; then
      echo "$label sem comando embutido: $marker" >&2
      exit 1
    fi
  done
done

echo "→ bootstrap-verify bootstrap command strings (S12 step 15) OK"

FAKE_BIN_DIR="$VERIFY_IR_TMP/fakebin"
mkdir -p "$FAKE_BIN_DIR"
cat >"$FAKE_BIN_DIR/soyuz" <<'EOF'
#!/usr/bin/env bash
echo "bootstrap soyuz should not run" >&2
exit 99
EOF
chmod +x "$FAKE_BIN_DIR/soyuz"

PATH_SAVE="$PATH"
export PATH="$FAKE_BIN_DIR:$PATH"

NATIVE_LIB="$("$OUT" build 2>&1 || true)"
export PATH="$PATH_SAVE"

if ! grep -q "$MARKER" <<<"$NATIVE_LIB"; then
  echo "native library build falhou com soyuz fake no PATH: $NATIVE_LIB" >&2
  exit 1
fi

if grep -q 'bootstrap soyuz should not run' <<<"$NATIVE_LIB"; then
  echo "library build ainda delega ao bootstrap soyuz" >&2
  exit 1
fi

echo "→ bootstrap-verify native library build (S12 step 16) OK"

export PATH="$FAKE_BIN_DIR:$PATH"

NATIVE_LEGACY="$("$OUT" build "$HELLO" 2>&1 || true)"
export PATH="$PATH_SAVE"

if ! grep -q 'Build concluído' <<<"$NATIVE_LEGACY"; then
  echo "native legacy build falhou com soyuz fake no PATH: $NATIVE_LEGACY" >&2
  exit 1
fi

if grep -q 'bootstrap soyuz should not run' <<<"$NATIVE_LEGACY"; then
  echo "legacy build ainda delega ao bootstrap soyuz" >&2
  exit 1
fi

echo "→ bootstrap-verify native legacy build (S12 step 17) OK"

export PATH="$FAKE_BIN_DIR:$PATH"

NATIVE_NEW="$("$OUT" new demo-native 2>&1 || true)"
export PATH="$PATH_SAVE"

if ! grep -q 'criado' <<<"$NATIVE_NEW"; then
  echo "native new falhou com soyuz fake no PATH: $NATIVE_NEW" >&2
  exit 1
fi

if grep -q 'bootstrap soyuz should not run' <<<"$NATIVE_NEW"; then
  echo "new ainda delega ao bootstrap soyuz" >&2
  exit 1
fi

echo "→ bootstrap-verify native new project (S12 step 18) OK"

export PATH="$FAKE_BIN_DIR:$PATH"

NATIVE_TEST="$("$OUT" test test_runner.sy 2>&1 || true)"
export PATH="$PATH_SAVE"

if ! grep -q "$TEST_MARKER" <<<"$NATIVE_TEST"; then
  echo "native test falhou com soyuz fake no PATH: $NATIVE_TEST" >&2
  exit 1
fi

if grep -q 'bootstrap soyuz should not run' <<<"$NATIVE_TEST"; then
  echo "test ainda delega ao bootstrap soyuz" >&2
  exit 1
fi

echo "→ bootstrap-verify native test runner (S12 step 19) OK"

export PATH="$FAKE_BIN_DIR:$PATH"

NATIVE_RUN="$("$OUT" run "$HELLO" 2>&1 || true)"
export PATH="$PATH_SAVE"

if ! grep -q "$HELLO_MARKER" <<<"$NATIVE_RUN"; then
  echo "native run falhou com soyuz fake no PATH: $NATIVE_RUN" >&2
  exit 1
fi

if grep -q 'bootstrap soyuz should not run' <<<"$NATIVE_RUN"; then
  echo "run ainda delega ao bootstrap soyuz" >&2
  exit 1
fi

echo "→ bootstrap-verify native hello run (S12 step 20) OK"

MAIN_FAKE_OUT="$VERIFY_IR_TMP/output-main-fake"
rm -f "$MAIN_FAKE_OUT"

export PATH="$FAKE_BIN_DIR:$PATH"

MAIN_BUILD_FAKE="$("$OUT" build main.sy -o "$MAIN_FAKE_OUT" 2>&1 || true)"
export PATH="$PATH_SAVE"

if ! grep -q 'bootstrap soyuz should not run' <<<"$MAIN_BUILD_FAKE"; then
  echo "build main.sy deveria delegar ao bootstrap com soyuz fake: $MAIN_BUILD_FAKE" >&2
  exit 1
fi

if [[ -x "$MAIN_FAKE_OUT" ]]; then
  echo "build main.sy produziu binário com soyuz fake no PATH: $MAIN_FAKE_OUT" >&2
  exit 1
fi

echo "→ bootstrap-verify bootstrap-only main.sy build (S12 step 21) OK"
