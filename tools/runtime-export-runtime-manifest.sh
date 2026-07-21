#!/usr/bin/env bash
# Export soyuz-go runtime assets as path-index manifest entries.
# Usage: runtime-export-runtime-manifest.sh <manifest-out> <prefix-dir> [--append]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"

MANIFEST=${1:?manifest output path}
PREFIX=${2:?absolute prefix for manifest file paths}
APPEND=${3:-}
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"
SRC_DIR="$GO_REF/internal/runtime/src"

if [[ ! -d "$SRC_DIR" ]]; then
  echo "runtime src não encontrado em $SRC_DIR" >&2
  exit 1
fi

if [[ "$APPEND" != "--append" ]]; then
  : >"$MANIFEST"
fi

# Keep in sync with src/runtime/embed.sy runtimeAssetNames().
assets=(
  soyuz.h rc.c std_io.c std_string.c std_fs.c std_os.c std_collections.c
  soyuz_rt.h soyuz_rt.c std_sync.c std_channel.c std_arc.c std_test.c
)
for name in "${assets[@]}"; do
  manifest_append_file "$MANIFEST" "$PREFIX/$name" "$SRC_DIR/$name"
done

count=$(grep -c '^===FILE===$' "$MANIFEST" || true)
echo "→ runtime manifest entries in $MANIFEST ($count arquivos total)"
