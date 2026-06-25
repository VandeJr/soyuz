#!/usr/bin/env bash
# Hello IR aligned with planHelloCodegenBuild (validated in src/driver/export_test.sy).
# Self-hosted soyuz run export will replace the template writer in S11/S12.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT=${1:?output ll path}
bash "$ROOT/tools/write-hello-module-ir.sh" 1 "$OUT"
