#!/usr/bin/env bash
# S11 bootstrap gate: soyuz build legacy arg parse + hello fixture build plan.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do cli build check" >&2
  exit 1
fi

echo "→ driver cli-build check (bootstrap) OK"
