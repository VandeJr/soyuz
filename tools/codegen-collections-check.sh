#!/usr/bin/env bash
# S5 bootstrap gate: collections + for-in codegen baseline (m3/m7 subset).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestCollections|TestList|TestM7ForIn' -count=1
echo "→ codegen collections check (bootstrap) OK"
