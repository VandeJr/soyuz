#!/usr/bin/env bash
# S9 bootstrap gate: runtime embed package compiles in soyuz-go.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/runtime" ]]; then
  echo "soyuz-go runtime não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/runtime/ -run '^$' -count=1
echo "→ runtime embed check (bootstrap) OK"
