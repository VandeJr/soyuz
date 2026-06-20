#!/usr/bin/env bash
# Estado da migração soyuz-go → soyuz (frontend). Usado por /migrate-compiler.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

hr() { printf '\n── %s ──\n' "$1"; }

hr "Repositório"
echo "branch: $(git branch --show-current)"
echo "dirty:  $(git status --porcelain | wc -l) arquivo(s) alterado(s)"

if [[ -f .cursor/migration-complete ]]; then
  echo "status: MIGRAÇÃO MARCADA COMO COMPLETA (.cursor/migration-complete)"
fi
if [[ -f .cursor/migration.lock ]]; then
  echo "lock:   .cursor/migration.lock ($(cat .cursor/migration.lock))"
fi

hr "Type-check (parser + checker, sem codegen)"
TOML_BAK="$(mktemp)"
cp soyuz.toml "$TOML_BAK"
trap 'cp "$TOML_BAK" soyuz.toml; rm -f "$TOML_BAK"' EXIT
sed -i 's/^type[[:space:]]*=.*/type    = "library"/' soyuz.toml
sed -i 's/^entry[[:space:]]*=.*/entry   = "validate.sy"/' soyuz.toml
if soyuz build 2>&1; then
  echo "→ type-check OK"
else
  echo "→ type-check FALHOU"
fi

hr "Lexer tests"
soyuz test test_runner.sy 2>&1 || true

hr "Milestones com stub (TODO)"
grep -l 'TODO: port milestone' tests/checker/*.sy 2>/dev/null | sort || echo "(nenhum)"

hr "Arquivos fonte (contagem)"
if [[ -d "$GO_REF/internal" ]]; then
  printf '  soyuz src/:      %s .sy\n' "$(find src -name '*.sy' | wc -l)"
  printf '  soyuz-go int/:   %s .go (lexer+parser+checker)\n' "$(find "$GO_REF/internal/lexer" "$GO_REF/internal/parser" "$GO_REF/internal/checker" -name '*.go' 2>/dev/null | wc -l)"
else
  echo "  soyuz-go não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)"
fi

hr "Referência"
echo "  docs/INTEGRATION.md"
echo "  .cursor/plans/migração_ast_checker_e8bf0647.plan.md"
