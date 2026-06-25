#!/usr/bin/env bash
# S7 bootstrap gate: task pipe codegen (m15_task_pipe_test.go subset).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestTaskPipeBasicIR|TestTaskPipeChainIR|TestTaskPipeCapturesVariable|TestTaskPipeLiteralIR|TestTaskPipeAwaitResult' -count=1
echo "→ codegen task-pipe check (bootstrap) OK"
