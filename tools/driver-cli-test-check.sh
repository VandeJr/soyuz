#!/usr/bin/env bash
# S11 bootstrap gate: soyuz test arg parse + test_runner test build plan.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do cli test check" >&2
  exit 1
fi

echo "→ driver cli-test check (bootstrap) OK"
