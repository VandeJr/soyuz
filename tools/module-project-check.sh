#!/usr/bin/env bash
# S8 bootstrap gate: project config / TOML parse (project_test.go subset).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

if [[ ! -d "$GO_REF/internal/module" ]]; then
  echo "soyuz-go module não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)" >&2
  exit 1
fi

cd "$GO_REF"
go test ./internal/module/ -run 'TestFindProjectRoot|TestLoadFromTOML|TestResolveAliasPath' -count=1
echo "→ module project check (bootstrap) OK"
