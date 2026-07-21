#!/usr/bin/env bash
# S7 bootstrap gate: full m_channel_test.go (M9 channel + rendezvous zero-capacity).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestChannel' -count=1
echo "→ codegen channel check (bootstrap) OK"
