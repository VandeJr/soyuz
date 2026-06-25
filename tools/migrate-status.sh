#!/usr/bin/env bash
# Estado do self-host soyuz (frontend + codegen). Usado por /migrate-compiler.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GO_REF="${SOYUZ_GO_ROOT:-$ROOT/soyuz-go}"

hr() { printf '\n── %s ──\n' "$1"; }

hr "Repositório"
echo "branch: $(git branch --show-current)"
echo "dirty:  $(git status --porcelain | wc -l) arquivo(s) alterado(s)"

if [[ -f .cursor/self-host-complete ]]; then
  echo "status: SELF-HOST COMPLETO (.cursor/self-host-complete)"
elif [[ -f .cursor/migration-complete ]]; then
  echo "status: FRONTEND MIGRAÇÃO COMPLETA (.cursor/migration-complete)"
fi
if [[ -f .cursor/self-host.lock ]]; then
  echo "lock:   .cursor/self-host.lock ($(cat .cursor/self-host.lock))"
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

hr "Codegen IR check (bootstrap, S1)"
if [[ -x tools/codegen-ir-check.sh ]]; then
  bash tools/codegen-ir-check.sh 2>&1 || echo "→ codegen IR check FALHOU"
else
  echo "  tools/codegen-ir-check.sh ausente"
fi

hr "Codegen expr check (bootstrap, S2)"
if [[ -x tools/codegen-expr-check.sh ]]; then
  bash tools/codegen-expr-check.sh 2>&1 || echo "→ codegen expr check FALHOU"
else
  echo "  tools/codegen-expr-check.sh ausente"
fi

hr "Codegen control check (bootstrap, S3)"
if [[ -x tools/codegen-control-check.sh ]]; then
  bash tools/codegen-control-check.sh 2>&1 || echo "→ codegen control check FALHOU"
else
  echo "  tools/codegen-control-check.sh ausente"
fi

hr "Codegen struct check (bootstrap, S4)"
if [[ -x tools/codegen-struct-check.sh ]]; then
  bash tools/codegen-struct-check.sh 2>&1 || echo "→ codegen struct check FALHOU"
else
  echo "  tools/codegen-struct-check.sh ausente"
fi

hr "Codegen when check (bootstrap, S4)"
if [[ -x tools/codegen-when-check.sh ]]; then
  bash tools/codegen-when-check.sh 2>&1 || echo "→ codegen when check FALHOU"
else
  echo "  tools/codegen-when-check.sh ausente"
fi

hr "Codegen collections check (bootstrap, S5)"
if [[ -x tools/codegen-collections-check.sh ]]; then
  bash tools/codegen-collections-check.sh 2>&1 || echo "→ codegen collections check FALHOU"
else
  echo "  tools/codegen-collections-check.sh ausente"
fi

hr "Codegen class check (bootstrap, S6)"
if [[ -x tools/codegen-class-check.sh ]]; then
  bash tools/codegen-class-check.sh 2>&1 || echo "→ codegen class check FALHOU"
else
  echo "  tools/codegen-class-check.sh ausente"
fi

hr "Codegen channel check (bootstrap, S7)"
if [[ -x tools/codegen-channel-check.sh ]]; then
  bash tools/codegen-channel-check.sh 2>&1 || echo "→ codegen channel check FALHOU"
else
  echo "  tools/codegen-channel-check.sh ausente"
fi

hr "Codegen select check (bootstrap, S7)"
if [[ -x tools/codegen-select-check.sh ]]; then
  bash tools/codegen-select-check.sh 2>&1 || echo "→ codegen select check FALHOU"
else
  echo "  tools/codegen-select-check.sh ausente"
fi

hr "Codegen sync check (bootstrap, S7)"
if [[ -x tools/codegen-sync-check.sh ]]; then
  bash tools/codegen-sync-check.sh 2>&1 || echo "→ codegen sync check FALHOU"
else
  echo "  tools/codegen-sync-check.sh ausente"
fi

hr "Codegen atomic check (bootstrap, S7)"
if [[ -x tools/codegen-atomic-check.sh ]]; then
  bash tools/codegen-atomic-check.sh 2>&1 || echo "→ codegen atomic check FALHOU"
else
  echo "  tools/codegen-atomic-check.sh ausente"
fi

hr "Codegen arc check (bootstrap, S7)"
if [[ -x tools/codegen-arc-check.sh ]]; then
  bash tools/codegen-arc-check.sh 2>&1 || echo "→ codegen arc check FALHOU"
else
  echo "  tools/codegen-arc-check.sh ausente"
fi

hr "Codegen gather check (bootstrap, S7)"
if [[ -x tools/codegen-gather-check.sh ]]; then
  bash tools/codegen-gather-check.sh 2>&1 || echo "→ codegen gather check FALHOU"
else
  echo "  tools/codegen-gather-check.sh ausente"
fi

hr "Codegen task check (bootstrap, S7)"
if [[ -x tools/codegen-task-check.sh ]]; then
  bash tools/codegen-task-check.sh 2>&1 || echo "→ codegen task check FALHOU"
else
  echo "  tools/codegen-task-check.sh ausente"
fi

hr "Codegen task-cancel check (bootstrap, S7)"
if [[ -x tools/codegen-task-cancel-check.sh ]]; then
  bash tools/codegen-task-cancel-check.sh 2>&1 || echo "→ codegen task-cancel check FALHOU"
else
  echo "  tools/codegen-task-cancel-check.sh ausente"
fi

hr "Codegen task-handle check (bootstrap, S7)"
if [[ -x tools/codegen-task-handle-check.sh ]]; then
  bash tools/codegen-task-handle-check.sh 2>&1 || echo "→ codegen task-handle check FALHOU"
else
  echo "  tools/codegen-task-handle-check.sh ausente"
fi

hr "Codegen task-pipe check (bootstrap, S7)"
if [[ -x tools/codegen-task-pipe-check.sh ]]; then
  bash tools/codegen-task-pipe-check.sh 2>&1 || echo "→ codegen task-pipe check FALHOU"
else
  echo "  tools/codegen-task-pipe-check.sh ausente"
fi

hr "Codegen task-all check (bootstrap, S7)"
if [[ -x tools/codegen-task-all-check.sh ]]; then
  bash tools/codegen-task-all-check.sh 2>&1 || echo "→ codegen task-all check FALHOU"
else
  echo "  tools/codegen-task-all-check.sh ausente"
fi

hr "Codegen task-fan check (bootstrap, S7)"
if [[ -x tools/codegen-task-fan-check.sh ]]; then
  bash tools/codegen-task-fan-check.sh 2>&1 || echo "→ codegen task-fan check FALHOU"
else
  echo "  tools/codegen-task-fan-check.sh ausente"
fi

hr "Codegen task-pipeline check (bootstrap, S7)"
if [[ -x tools/codegen-task-pipeline-check.sh ]]; then
  bash tools/codegen-task-pipeline-check.sh 2>&1 || echo "→ codegen task-pipeline check FALHOU"
else
  echo "  tools/codegen-task-pipeline-check.sh ausente"
fi

hr "Codegen task-tap check (bootstrap, S7)"
if [[ -x tools/codegen-task-tap-check.sh ]]; then
  bash tools/codegen-task-tap-check.sh 2>&1 || echo "→ codegen task-tap check FALHOU"
else
  echo "  tools/codegen-task-tap-check.sh ausente"
fi

hr "Codegen task-always check (bootstrap, S7)"
if [[ -x tools/codegen-task-always-check.sh ]]; then
  bash tools/codegen-task-always-check.sh 2>&1 || echo "→ codegen task-always check FALHOU"
else
  echo "  tools/codegen-task-always-check.sh ausente"
fi

hr "Codegen async-pipe check (bootstrap, S7)"
if [[ -x tools/codegen-async-pipe-check.sh ]]; then
  bash tools/codegen-async-pipe-check.sh 2>&1 || echo "→ codegen async-pipe check FALHOU"
else
  echo "  tools/codegen-async-pipe-check.sh ausente"
fi

hr "Codegen async-pipe-quest check (bootstrap, S7)"
if [[ -x tools/codegen-async-pipe-quest-check.sh ]]; then
  bash tools/codegen-async-pipe-quest-check.sh 2>&1 || echo "→ codegen async-pipe-quest check FALHOU"
else
  echo "  tools/codegen-async-pipe-quest-check.sh ausente"
fi

hr "Module project check (bootstrap, S8)"
if [[ -x tools/module-project-check.sh ]]; then
  bash tools/module-project-check.sh 2>&1 || echo "→ module project check FALHOU"
else
  echo "  tools/module-project-check.sh ausente"
fi

hr "Module graph check (bootstrap, S8)"
if [[ -x tools/module-graph-check.sh ]]; then
  bash tools/module-graph-check.sh 2>&1 || echo "→ module graph check FALHOU"
else
  echo "  tools/module-graph-check.sh ausente"
fi

hr "Module stdlib check (bootstrap, S8)"
if [[ -x tools/module-stdlib-check.sh ]]; then
  bash tools/module-stdlib-check.sh 2>&1 || echo "→ module stdlib check FALHOU"
else
  echo "  tools/module-stdlib-check.sh ausente"
fi

hr "Module prelude check (bootstrap, S8)"
if [[ -x tools/module-prelude-check.sh ]]; then
  bash tools/module-prelude-check.sh 2>&1 || echo "→ module prelude check FALHOU"
else
  echo "  tools/module-prelude-check.sh ausente"
fi

hr "Runtime embed check (bootstrap, S9)"
if [[ -x tools/runtime-embed-check.sh ]]; then
  bash tools/runtime-embed-check.sh 2>&1 || echo "→ runtime embed check FALHOU"
else
  echo "  tools/runtime-embed-check.sh ausente"
fi

hr "Runtime index seed check (bootstrap, S9)"
if [[ -x tools/runtime-index-seed.sh ]]; then
  bash tools/runtime-index-seed.sh 2>&1 || echo "→ runtime index seed check FALHOU"
else
  echo "  tools/runtime-index-seed.sh ausente"
fi

hr "Runtime link smoke check (bootstrap, S9)"
if [[ -x tools/runtime-link-smoke.sh ]]; then
  bash tools/runtime-link-smoke.sh 2>&1 || echo "→ runtime link smoke check FALHOU"
else
  echo "  tools/runtime-link-smoke.sh ausente"
fi

hr "Runtime run-link check (bootstrap, S9)"
if [[ -x tools/runtime-run-link-check.sh ]]; then
  bash tools/runtime-run-link-check.sh 2>&1 || echo "→ runtime run-link check FALHOU"
else
  echo "  tools/runtime-run-link-check.sh ausente"
fi

hr "Milestones com stub (TODO)"
grep -l 'TODO: port milestone' tests/checker/*.sy 2>/dev/null | sort || echo "(nenhum)"

hr "Arquivos fonte (contagem)"
if [[ -d "$GO_REF/internal" ]]; then
  printf '  soyuz src/:      %s .sy\n' "$(find src -name '*.sy' | wc -l)"
  printf '  soyuz-go int/:   %s .go (lexer+parser+checker)\n' "$(find "$GO_REF/internal/lexer" "$GO_REF/internal/parser" "$GO_REF/internal/checker" -name '*.go' 2>/dev/null | wc -l)"
else
  echo "  soyuz-go não encontrado em $GO_REF (defina SOYUZ_GO_ROOT)"
fi

hr "Self-host milestones (S0–S12)"
if [[ -f docs/SELF_HOST_PLAN.md ]]; then
  grep -E '^\| \*\*S[0-9]+\*\*' docs/SELF_HOST_PLAN.md | sed 's/^/  /' || echo "  (tabela não encontrada)"
else
  echo "  docs/SELF_HOST_PLAN.md ausente"
fi

hr "Referência"
echo "  docs/SELF_HOST_PLAN.md"
echo "  docs/INTEGRATION.md"
echo "  .cursor/plans/migração_ast_checker_e8bf0647.plan.md"
