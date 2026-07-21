#!/usr/bin/env bash
# Execute every language feature fixture and compare deterministic program output.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
COMPILER="${SOYUZ_COMPILER:-soyuz}"

if [[ ${1:-} == "--compiler" ]]; then
  COMPILER=${2:?missing compiler path}
  shift 2
fi
[[ $# -eq 0 ]] || { echo "uso: $0 [--compiler PATH]" >&2; exit 2; }
command -v "$COMPILER" >/dev/null 2>&1 || { echo "compilador não encontrado: $COMPILER" >&2; exit 1; }

cd "$ROOT"
TMP="$(mktemp -d /tmp/soyuz-feature-corpus.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
failed=0
for fixture in feature-tests/*.sy; do
  name=$(basename "$fixture" .sy)
  stdout="$TMP/$name.stdout"
  stderr="$TMP/$name.stderr"
  actual="$TMP/$name.actual"
  expected="feature-tests/expected/$name.out"
  if [[ ! -f "$expected" ]]; then
    echo "✗ $fixture: expectativa ausente: $expected" >&2
    failed=1
    continue
  fi
  if ! "$COMPILER" run "$fixture" >"$stdout" 2>"$stderr"; then
    phase="frontend/codegen/link"
    grep -q '^Build concluído:' "$stdout" && phase="execução"
    if grep -qxF "$name" tools/selfhost-known-failures.txt; then
      echo "✗ $fixture: falha conhecida na fase de $phase" >&2
    else
      echo "✗ $fixture: regressão nova na fase de $phase" >&2
    fi
    cat "$stdout" >&2
    cat "$stderr" >&2
    failed=1
    continue
  fi
  sed -E '/^Build concluído:/d; s/(^|[[:space:]:=(])0x[0-9a-fA-F]+/\1<ptr>/g' "$stdout" >"$actual"
  if ! diff -u "$expected" "$actual" >"$TMP/$name.diff"; then
    echo "✗ $fixture: saída divergiu" >&2
    cat "$TMP/$name.diff" >&2
    failed=1
    continue
  fi
  if grep -qvE '^(warning: overriding the module target triple|[0-9]+ warning generated\.|$)' "$stderr"; then
    echo "✗ $fixture: stderr inesperado" >&2
    cat "$stderr" >&2
    failed=1
    continue
  fi
  echo "✓ $fixture"
done
[[ $failed -eq 0 ]] || exit 1
echo "→ corpus de recursos passou"
