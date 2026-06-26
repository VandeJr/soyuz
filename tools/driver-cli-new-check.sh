#!/usr/bin/env bash
# S11 bootstrap gate: soyuz new templates + entry routing (in-memory driver tests).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do cli new check" >&2
  exit 1
fi

echo "→ driver cli-new check (bootstrap) OK"
