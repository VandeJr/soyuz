#!/usr/bin/env bash
# S1 bootstrap gate: subset of soyuz-go/internal/codegen/ir_check_test.go
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/codegen" ]]; then
  echo "soyuz-go codegen não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/codegen/ -run 'TestMainWithBodyIR|TestLexerIR$' -count=1
echo "→ codegen IR check (bootstrap) OK"
