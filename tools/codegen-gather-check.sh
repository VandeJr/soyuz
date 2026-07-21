#!/usr/bin/env bash
# S7 bootstrap gate: Task.gather codegen baseline (m26_gather_test.go subset).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestTaskGatherEmitsSrtEnqueue|TestTaskGatherEmitsSrtAwait|TestTaskGatherEmitsGatherBlocks|TestTaskGatherEmitsWrapperFunc|TestTaskGatherReturnsListIR' -count=1
echo "→ codegen gather check (bootstrap) OK"
