#!/usr/bin/env bash
# S11 bootstrap gate: root main.sy delegates to driver CLI (library type-check).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do main entry check" >&2
  exit 1
fi

if ! grep -q 'runCliFromArgv' "$ROOT/main.sy"; then
  echo "main.sy não delega ao driver CLI" >&2
  exit 1
fi

echo "→ driver main-entry check (bootstrap) OK"
