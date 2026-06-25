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
