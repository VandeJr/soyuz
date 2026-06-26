#!/usr/bin/env bash
# S11 bootstrap gate: root main.sy builds as thin standalone binary (usage path).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! soyuz build >/dev/null 2>&1; then
  echo "soyuz build (library) falhou antes do main standalone check" >&2
  exit 1
fi

OUT="$ROOT/output"
rm -f "$OUT"

if ! soyuz build main.sy 2>&1; then
  echo "soyuz build main.sy falhou" >&2
  exit 1
fi

if [[ ! -x "$OUT" ]]; then
  echo "binário output ausente após soyuz build main.sy" >&2
  exit 1
fi

if ! grep -q 'runStandaloneCli' "$ROOT/main.sy"; then
  echo "main.sy não delega ao standalone CLI" >&2
  exit 1
fi

USAGE="$("$OUT" 2>&1 || true)"
if ! grep -q 'Uso: soyuz' <<<"$USAGE"; then
  echo "binário main.sy não imprime usage esperado: $USAGE" >&2
  exit 1
fi

NEW="$("$OUT" new demo-proj 2>&1 || true)"
if ! grep -q 'demo-proj' <<<"$NEW"; then
  echo "binário main.sy não roteia soyuz new: $NEW" >&2
  exit 1
fi

BUILD="$("$OUT" build tools/fixtures/hello_minimal.sy 2>&1 || true)"
if ! grep -q 'Build concluído' <<<"$BUILD"; then
  echo "binário main.sy não roteia soyuz build legacy: $BUILD" >&2
  exit 1
fi

LIB="$("$OUT" build 2>&1 || true)"
if ! grep -q 'verificada com sucesso' <<<"$LIB"; then
  echo "binário main.sy não roteia soyuz build project-aware: $LIB" >&2
  exit 1
fi

TEST="$("$OUT" test 2>&1 || true)"
if ! grep -q '/tmp/soyuz-cli-test/test-bin' <<<"$TEST"; then
  echo "binário main.sy não roteia soyuz test legacy: $TEST" >&2
  exit 1
fi

RUN="$("$OUT" run tools/fixtures/hello_minimal.sy 2>&1 || true)"
if ! grep -q '/tmp/soyuz-cli-run/app' <<<"$RUN"; then
  echo "binário main.sy não roteia soyuz run legacy: $RUN" >&2
  exit 1
fi

echo "→ driver main-standalone check (bootstrap) OK"
