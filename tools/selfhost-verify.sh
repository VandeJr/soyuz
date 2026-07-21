#!/usr/bin/env bash
# Verify that a generated Soyuz compiler can rebuild itself without soyuz-go.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BOOTSTRAP="${SOYUZ_BOOTSTRAP:-soyuz}"
COMPILER=""
NO_BOOTSTRAP=false
KEEP=false

usage() {
  cat <<'EOF'
Usage: tools/selfhost-verify.sh [--bootstrap-bin PATH] [--compiler PATH] [--no-bootstrap] [--keep-artifacts]

Without --no-bootstrap, the bootstrap compiler creates vN. With --no-bootstrap,
--compiler is vN and the script verifies only the native vN -> vN+1 -> vN+2 chain.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bootstrap-bin) BOOTSTRAP=${2:?missing bootstrap path}; shift 2 ;;
    --compiler) COMPILER=${2:?missing compiler path}; shift 2 ;;
    --no-bootstrap) NO_BOOTSTRAP=true; shift ;;
    --keep-artifacts) KEEP=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "argumento desconhecido: $1" >&2; usage >&2; exit 2 ;;
  esac
done

ARTIFACTS="${SELFHOST_ARTIFACTS_DIR:-$(mktemp -d /tmp/soyuz-selfhost.XXXXXX)}"
mkdir -p "$ARTIFACTS"
case "$ARTIFACTS/" in
  "$ROOT/"*) echo "SELFHOST_ARTIFACTS_DIR deve ficar fora do repositório" >&2; exit 2 ;;
esac
SOURCE_ROOT="$ARTIFACTS/source"
mkdir -p "$SOURCE_ROOT"
cp -a "$ROOT/." "$SOURCE_ROOT/"

cleanup() {
  local status=$?
  if [[ $status -ne 0 || $KEEP == true ]]; then
    echo "artefatos preservados em $ARTIFACTS" >&2
  else
    rm -rf "$ARTIFACTS"
  fi
}
trap cleanup EXIT

safe_path() {
  local tool dir path=""
  for tool in bash clang sha256sum readelf nm env mktemp awk grep sed dirname find sort tr head tail cp mkdir rm diff; do
    dir="$(dirname "$(command -v "$tool")")"
    case ":$path:" in *":$dir:"*) ;; *) path="${path:+$path:}$dir" ;; esac
  done
  printf '%s' "$path"
}

SAFE_PATH="$(safe_path)"
if PATH="$SAFE_PATH" command -v soyuz >/dev/null 2>&1; then
  echo "PATH isolado ainda encontra soyuz; recusei executar o gate" >&2
  exit 1
fi

fingerprint() {
  local binary=$1 output=$2
  {
    sha256sum "$binary"
    readelf -SW "$binary" | awk '/\.(text|rodata|data|bss)[[:space:]]/ {print $2, $5, $6}'
    nm -n --defined-only "$binary" | awk '{print $2, $3}'
  } >"$output"
}

run_native_build() {
  local compiler=$1 output=$2 label=$3
  local log="$ARTIFACTS/$label.log"
  if ! (cd "$SOURCE_ROOT" && env -i \
      PATH="$SAFE_PATH" \
      HOME="$ARTIFACTS/home" \
      SOYUZ_GO_ROOT="/nonexistent/soyuz-go" \
      "$compiler" build main.sy -o "$output") >"$log" 2>&1; then
    echo "$label falhou sem bootstrap. Consulte $log" >&2
    cat "$log" >&2
    return 1
  fi
  if [[ ! -x "$output" ]]; then
    if grep -Eq '(command not found|erro: exec soyuz|soyuz-go)' "$log"; then
      echo "$label tentou usar o bootstrap; fallback é proibido em --no-bootstrap" >&2
      cat "$log" >&2
    else
      echo "$label não produziu binário executável" >&2
      cat "$log" >&2
    fi
    return 1
  fi
}

VN="$ARTIFACTS/vN"
if [[ $NO_BOOTSTRAP == true ]]; then
  [[ -n "$COMPILER" ]] || { echo "--no-bootstrap exige --compiler" >&2; exit 2; }
  [[ -x "$COMPILER" ]] || { echo "compilador não executável: $COMPILER" >&2; exit 2; }
  cp "$COMPILER" "$VN"
else
  command -v "$BOOTSTRAP" >/dev/null 2>&1 || { echo "bootstrap não encontrado: $BOOTSTRAP" >&2; exit 1; }
  # The current bootstrap CLI always writes output in cwd for main.sy and ignores
  # -o on this path. Copy it immediately so the generated artifact is stable.
  rm -f "$SOURCE_ROOT/output"
  (cd "$SOURCE_ROOT" && "$BOOTSTRAP" build main.sy) >"$ARTIFACTS/bootstrap.log" 2>&1
  [[ -x "$SOURCE_ROOT/output" ]] || { echo "bootstrap não produziu source/output" >&2; exit 1; }
  cp "$SOURCE_ROOT/output" "$VN"
fi

VN1="$ARTIFACTS/vN+1"
VN2="$ARTIFACTS/vN+2"
run_native_build "$VN" "$VN1" "vN-para-vN1"
run_native_build "$VN1" "$VN2" "vN1-para-vN2"

for binary in "$VN" "$VN1" "$VN2"; do
  fingerprint "$binary" "$binary.fingerprint"
  (cd "$SOURCE_ROOT" && env -i PATH="$SAFE_PATH" HOME="$ARTIFACTS/home" SOYUZ_GO_ROOT="/nonexistent/soyuz-go" \
    "$binary" test test_runner.sy) >"$binary.test.log" 2>&1
  grep -Eq '✓[[:space:]]+5 testes passaram' "$binary.test.log"
done

if ! diff -u "$VN.fingerprint" "$VN1.fingerprint" >"$ARTIFACTS/vN-vN1.diff"; then
  echo "fingerprint de vN e vN+1 diverge: $ARTIFACTS/vN-vN1.diff" >&2
  exit 1
fi
if ! diff -u "$VN1.fingerprint" "$VN2.fingerprint" >"$ARTIFACTS/vN1-vN2.diff"; then
  echo "fingerprint de vN+1 e vN+2 diverge: $ARTIFACTS/vN1-vN2.diff" >&2
  exit 1
fi

echo "→ self-host independente passou: vN, vN+1 e vN+2 não acessaram o bootstrap"
