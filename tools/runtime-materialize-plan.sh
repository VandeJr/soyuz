#!/usr/bin/env bash
# S9 helper: copy soyuz-go runtime/src assets into a tmp dir for manual clang link.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"
SRC_DIR="$GO_REF/internal/runtime/src"
DEST="${1:-/tmp/soyuz-runtime}"

if [[ ! -d "$SRC_DIR" ]]; then
  echo "runtime src não encontrado em $SRC_DIR" >&2
  exit 1
fi

mkdir -p "$DEST"
assets=(
  soyuz.h rc.c std_io.c std_string.c std_fs.c std_os.c std_collections.c
  soyuz_rt.h soyuz_rt.c std_sync.c std_channel.c std_arc.c std_test.c
)
for name in "${assets[@]}"; do
  cp "$SRC_DIR/$name" "$DEST/$name"
done
echo "→ runtime materialized to $DEST (${#assets[@]} arquivos)"
