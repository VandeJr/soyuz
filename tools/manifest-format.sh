#!/usr/bin/env bash
# Shared helpers for soyuz path-index manifest format.
set -euo pipefail

manifest_append_file() {
  local manifest=$1 path=$2 src=$3
  {
    printf '%s\n' '===FILE==='
    printf '%s\n' "$path"
    printf '%s\n' '===BODY==='
    cat "$src"
    printf '%s\n' '===END==='
  } >>"$manifest"
}

manifest_append_body() {
  local manifest=$1 path=$2
  shift 2
  local body_file
  body_file="$(mktemp)"
  printf '%s' "$*" >"$body_file"
  manifest_append_file "$manifest" "$path" "$body_file"
  rm -f "$body_file"
}

# Verify every ===FILE=== path in manifest exists on disk with non-zero size.
manifest_verify_applied_paths() {
  local manifest=$1
  local state="" path="" count=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$state" in
      "")
        if [[ "$line" == "===FILE===" ]]; then
          state="file"
        fi
        ;;
      file)
        path="$line"
        state="body_wait"
        ;;
      body_wait)
        if [[ "$line" == "===BODY===" ]]; then
          state="body"
        else
          echo "esperado ===BODY=== após path, obteve: $line" >&2
          return 1
        fi
        ;;
      body)
        if [[ "$line" == "===END===" ]]; then
          if [[ ! -s "$path" ]]; then
            echo "arquivo ausente ou vazio após apply: $path" >&2
            return 1
          fi
          count=$((count + 1))
          path=""
          state=""
        fi
        ;;
    esac
  done <"$manifest"
  printf '%s' "$count"
}
