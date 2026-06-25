#!/usr/bin/env bash
# S8 bootstrap gate: module graph collect (module_test.go subset).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/module" ]]; then
  echo "soyuz-go module não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/module/ -run 'TestSingleFile$|TestSingleFileImport|TestCycleDetection' -count=1
echo "→ module graph check (bootstrap) OK"
