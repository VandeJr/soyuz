#!/usr/bin/env bash
# Export bootstrap hello compile+link pipeline as path-index manifest.
# Usage: runtime-export-pipeline-manifest.sh <manifest-out> <prefix-dir> [hello.sy]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"

MANIFEST=${1:?manifest output path}
PREFIX=${2:?absolute prefix for manifest file paths}
HELLO=${3:-"$ROOT/tools/fixtures/hello_minimal.sy"}
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"
SRC_DIR="$GO_REF/internal/runtime/src"

if [[ ! -f "$HELLO" ]]; then
  echo "hello source não encontrado: $HELLO" >&2
  exit 1
fi
if [[ ! -d "$SRC_DIR" ]]; then
  echo "runtime src não encontrado em $SRC_DIR" >&2
  exit 1
fi
if ! command -v soyuz >/dev/null 2>&1; then
  echo "soyuz bootstrap não encontrado no PATH" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

SOYUZ_KEEP_IR=1 soyuz build "$HELLO" -o "$TMP/bootstrap-out" >/dev/null
if [[ ! -f /tmp/soyuz_debug.ll ]]; then
  echo "IR debug não gerado (SOYUZ_KEEP_IR=1)" >&2
  exit 1
fi

: >"$MANIFEST"
manifest_append_file "$MANIFEST" "$PREFIX/out.ll" /tmp/soyuz_debug.ll

assets=(
  soyuz.h rc.c std_io.c std_string.c std_fs.c std_os.c std_collections.c
  soyuz_rt.h soyuz_rt.c std_sync.c std_channel.c std_arc.c std_test.c
)
for name in "${assets[@]}"; do
  manifest_append_file "$MANIFEST" "$PREFIX/$name" "$SRC_DIR/$name"
done

count=$(grep -c '^===FILE===$' "$MANIFEST" || true)
echo "→ pipeline manifest exported to $MANIFEST ($count arquivos)"
