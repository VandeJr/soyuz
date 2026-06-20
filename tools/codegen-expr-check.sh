#!/usr/bin/env bash
# S2 bootstrap gate: M0–M2 expr/codegen baseline subset (generator_test + char).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestGeneratorBasic|TestGeneratorMath|TestCharLiteralEmitsI32' -count=1
echo "→ codegen expr check (bootstrap) OK"
