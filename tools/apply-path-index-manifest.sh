#!/usr/bin/env bash
# Apply a soyuz path-index manifest (===FILE=== / ===BODY=== / ===END=== format).
set -euo pipefail

MANIFEST=${1:?manifest file}

if [[ ! -f "$MANIFEST" ]]; then
  echo "manifest não encontrado: $MANIFEST" >&2
  exit 1
fi

state=""
current_path=""
body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT

flush_body() {
  if [[ -z "$current_path" ]]; then
    echo "manifest sem path antes de ===END===" >&2
    exit 1
  fi
  mkdir -p "$(dirname "$current_path")"
  cp "$body_file" "$current_path"
  : >"$body_file"
  current_path=""
  state=""
}

while IFS= read -r line || [[ -n "$line" ]]; do
  case "$state" in
    "")
      if [[ "$line" == "===FILE===" ]]; then
        state="file"
      fi
      ;;
    file)
      current_path="$line"
      state="body_wait"
      ;;
    body_wait)
      if [[ "$line" == "===BODY===" ]]; then
        : >"$body_file"
        state="body"
      else
        echo "esperado ===BODY=== após path, obteve: $line" >&2
        exit 1
      fi
      ;;
    body)
      if [[ "$line" == "===END===" ]]; then
        flush_body
      else
        if [[ -s "$body_file" ]]; then
          printf '\n' >>"$body_file"
        fi
        printf '%s' "$line" >>"$body_file"
      fi
      ;;
  esac
done <"$MANIFEST"

if [[ -n "$state" ]]; then
  echo "manifest truncado (estado=$state)" >&2
  exit 1
fi

echo "→ manifest applied from $MANIFEST"
