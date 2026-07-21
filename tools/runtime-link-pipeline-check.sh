#!/usr/bin/env bash
# S9 bootstrap gate: end-to-end runtime link pipeline.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
bash "$ROOT/tools/runtime-link-pipeline.sh"
echo "→ runtime link pipeline check (bootstrap) OK"
