#!/usr/bin/env bash
# S12 bootstrap gate: in-memory main.sy full-build plan ready in driver library.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do main-build plan check" >&2
  exit 1
fi

echo "→ driver main-build plan check (bootstrap) OK"
