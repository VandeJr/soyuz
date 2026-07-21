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

if ! grep -q 'cliOsExecShell' "$ROOT/main.sy"; then
  echo "main.sy não delega ao bootstrap via cliOsExecShell" >&2
  exit 1
fi

USAGE="$("$OUT" 2>&1 || true)"
if ! grep -q 'Uso: soyuz' <<<"$USAGE"; then
  echo "binário main.sy não imprime usage esperado: $USAGE" >&2
  exit 1
fi

NEW_TMP="$(mktemp -d)"
trap 'rm -rf "$NEW_TMP"' EXIT
NEW="$(
  cd "$NEW_TMP" && "$OUT" new demo-proj 2>&1 || true
)"
if ! grep -q 'demo-proj' <<<"$NEW"; then
  echo "binário main.sy não executa soyuz new: $NEW" >&2
  exit 1
fi
if ! grep -q 'criado' <<<"$NEW"; then
  echo "binário main.sy soyuz new sem mensagem de sucesso: $NEW" >&2
  exit 1
fi

BUILD="$("$OUT" build tools/fixtures/hello_minimal.sy -o /tmp/soyuz-standalone-hello-out 2>&1 || true)"
if ! grep -q 'Build concluído' <<<"$BUILD"; then
  echo "binário main.sy soyuz build legacy sem mensagem de sucesso: $BUILD" >&2
  exit 1
fi

LIB="$("$OUT" build 2>&1 || true)"
if ! grep -q 'verificada com sucesso' <<<"$LIB"; then
  echo "binário main.sy soyuz build library sem mensagem de verify: $LIB" >&2
  exit 1
fi

TEST="$("$OUT" test test_runner.sy 2>&1 || true)"
if ! grep -Eq '✓[[:space:]]+5 testes passaram' <<<"$TEST"; then
  echo "binário main.sy soyuz test não executou exatamente 5 testes: $TEST" >&2
  exit 1
fi

RUN="$("$OUT" run tools/fixtures/hello_minimal.sy 2>&1 || true)"
if ! grep -q 'hello' <<<"$RUN"; then
  echo "binário main.sy soyuz run sem saída hello: $RUN" >&2
  exit 1
fi

echo "→ driver main-standalone check (bootstrap) OK"
