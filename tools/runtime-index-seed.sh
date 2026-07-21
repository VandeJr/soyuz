#!/usr/bin/env bash
# S9 bootstrap gate: soyuz-go runtime/src contains all embed.go asset files.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"
SRC_DIR="$GO_REF/internal/runtime/src"

if [[ ! -d "$SRC_DIR" ]]; then
  echo "runtime src não encontrado em $SRC_DIR (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

# Keep in sync with src/runtime/embed.sy runtimeAssetNames().
assets=(
  soyuz.h
  rc.c
  std_io.c
  std_string.c
  std_fs.c
  std_os.c
  std_collections.c
  soyuz_rt.h
  soyuz_rt.c
  std_sync.c
  std_channel.c
  std_arc.c
  std_test.c
)

missing=0
for name in "${assets[@]}"; do
  path="$SRC_DIR/$name"
  if [[ ! -f "$path" ]]; then
    echo "ausente: $path" >&2
    missing=$((missing + 1))
    continue
  fi
  if [[ ! -s "$path" ]]; then
    echo "vazio: $path" >&2
    missing=$((missing + 1))
  fi
done

if [[ "$missing" -ne 0 ]]; then
  echo "→ runtime index seed check FALHOU ($missing arquivo(s))" >&2
  exit 1
fi

echo "→ runtime index seed check (bootstrap) OK (${#assets[@]} arquivos)"
