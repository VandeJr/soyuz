#!/usr/bin/env bash
# S9 bootstrap gate: RunPlan materialize commands → exec hello binary.
# Sequence mirrors runPlanExportManifestCommand / runPlanApplyManifestCommand /
# runPlanLinkCommand / runPlanExecCommand (validated in src/driver/run_test.sy).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EXPORT_SCRIPT="tools/export-hello-run-manifest.sh"
APPLY_SCRIPT="tools/apply-path-index-manifest.sh"
LINK_SCRIPT="tools/runtime-run-link.sh"

for script in "$EXPORT_SCRIPT" "$APPLY_SCRIPT" "$LINK_SCRIPT"; do
  if [[ ! -x "$ROOT/$script" ]]; then
    echo "script RunPlan ausente ou não executável: $script" >&2
    exit 1
  fi
done

TMP="$(mktemp -d)"
PREFIX="$TMP/rt"
MANIFEST="$TMP/pipeline.manifest"
BINARY="$TMP/app"
trap 'rm -rf "$TMP"' EXIT

bash "$ROOT/$EXPORT_SCRIPT" "$MANIFEST" "$PREFIX"
bash "$ROOT/$APPLY_SCRIPT" "$MANIFEST" >/dev/null

# shellcheck source=manifest-format.sh
source "$ROOT/tools/manifest-format.sh"
applied_count=$(manifest_verify_applied_paths "$MANIFEST")
if [[ "$applied_count" -ne 14 ]]; then
  echo "esperado 14 arquivos materializados, obteve $applied_count" >&2
  exit 1
fi

bash "$ROOT/$LINK_SCRIPT" "$PREFIX/out.ll" "$PREFIX" "$BINARY"
OUT="$("$BINARY")"
if [[ "$OUT" != "hello" ]]; then
  echo "esperado 'hello', obteve '$OUT'" >&2
  exit 1
fi
echo "→ driver run-plan-materialize check (bootstrap) OK"
