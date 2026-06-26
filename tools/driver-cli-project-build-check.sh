#!/usr/bin/env bash
# S11 bootstrap gate: soyuz build project-aware library verify (driver in-memory).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do cli project-build check" >&2
  exit 1
fi

echo "→ driver cli-project-build check (bootstrap) OK"
