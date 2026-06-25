#!/usr/bin/env bash
# Run clang link using the same argv shape as src/runtime/link.sy buildClangArgs.
set -euo pipefail

usage() {
  echo "uso: $0 <ll-file> <tmp-dir> <output-binary> [--optimize]" >&2
  exit 1
}

[[ $# -ge 3 ]] || usage

LL_FILE=$1
TMP_DIR=$2
OUTPUT=$3
OPT_FLAG=${4:-}

if ! command -v clang >/dev/null 2>&1; then
  echo "clang não encontrado no PATH" >&2
  exit 1
fi
if [[ ! -f "$LL_FILE" ]]; then
  echo "ll não encontrado: $LL_FILE" >&2
  exit 1
fi
if [[ ! -d "$TMP_DIR" ]]; then
  echo "tmp dir não encontrado: $TMP_DIR" >&2
  exit 1
fi

clang_args=("$LL_FILE")
for src in "$TMP_DIR"/*.c; do
  [[ -e "$src" ]] || continue
  clang_args+=("$src")
done
clang_args+=(-I "$TMP_DIR" -pthread)
if [[ "$OPT_FLAG" == "--optimize" ]]; then
  clang_args+=(-O2)
fi
clang_args+=(-o "$OUTPUT")

clang "${clang_args[@]}"
