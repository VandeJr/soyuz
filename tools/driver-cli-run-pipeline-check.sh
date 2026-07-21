#!/usr/bin/env bash
# S9 bootstrap gate: ordered CLI run pipeline (export → apply → link → exec hello).
# Aligns with runPlanOrderedMaterializeCommands / cliHelloMinimalRunOrderedCommands.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
HELLO="$ROOT/tools/fixtures/hello_minimal.sy"
EXPORT_SCRIPT="tools/export-hello-run-manifest.sh"
APPLY_SCRIPT="tools/apply-path-index-manifest.sh"
LINK_SCRIPT="tools/runtime-run-link.sh"

if [[ ! -f "$HELLO" ]]; then
  echo "fixture CLI pipeline não encontrado: $HELLO" >&2
  exit 1
fi
for script in "$EXPORT_SCRIPT" "$APPLY_SCRIPT" "$LINK_SCRIPT"; do
  if [[ ! -x "$ROOT/$script" ]]; then
    echo "script CLI pipeline ausente ou não executável: $script" >&2
    exit 1
  fi
done

TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
BINARY="$TMP/app"
trap 'rm -rf "$TMP"' EXIT

echo "→ pipeline step 1: export manifest"
bash "$ROOT/$EXPORT_SCRIPT" "$MANIFEST" "$PREFIX"

echo "→ pipeline step 2: apply manifest"
bash "$ROOT/$APPLY_SCRIPT" "$MANIFEST" >/dev/null

# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"
applied_count=$(manifest_verify_applied_paths "$MANIFEST")
if [[ "$applied_count" -ne 14 ]]; then
  echo "esperado 14 arquivos materializados, obteve $applied_count" >&2
  exit 1
fi

echo "→ pipeline step 3: link"
bash "$ROOT/$LINK_SCRIPT" "$PREFIX/out.ll" "$PREFIX" "$BINARY"

echo "→ pipeline step 4: exec"
OUT="$("$BINARY")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ driver cli-run-pipeline check (bootstrap) OK"
