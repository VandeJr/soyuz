#!/usr/bin/env bash
# S3 bootstrap gate: control flow + loop break value subset.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestGeneratorControlFlow|TestLoopBreakValueCodegen' -count=1
echo "→ codegen control check (bootstrap) OK"
