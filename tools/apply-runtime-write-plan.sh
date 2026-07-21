#!/usr/bin/env bash
# Apply default runtime write plan: copy soyuz-go runtime/src into DEST.
set -euo pipefail

DEST=${1:?dest dir}
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
exec bash "$ROOT/tools/runtime-materialize-plan.sh" "$DEST"
