# Integração soyuz-go ↔ frontend Soyuz (self-host)

Este documento descreve como conectar o frontend em Soyuz Lang (`/home/vand/Projects/soyuz`) ao compilador de produção (`soyuz-go`) quando o bootstrap estiver estável.

## Estado atual (jun/2026)

| Camada | Soyuz (.sy) | Bootstrap (`soyuz` no PATH) |
|--------|-------------|------------------------------|
| Lexer | Completo + 5 testes | Compila e roda |
| AST | Enums `Node`, `TypeExpr`, `Pattern` | Type-check OK |
| Parser | ~M0+ portado (`extend Parser`) | Type-check OK; **codegen** falha em extend |
| Checker | ~6500 LOC, 5 passes, **M0–M26** testes portados | Type-check OK; **codegen** falha (enum match, Type constants) |

### Comandos

```bash
# Lexer runtime tests (passam)
soyuz test test_runner.sy

# Type-check lexer + parser + checker (sem codegen)
# Temporariamente: type=library, entry=validate.sy em soyuz.toml, depois soyuz build
soyuz build   # com entry=validate.sy e type=library → "verificada com sucesso"
```

Arquivos de teste portados (executam quando codegen estabilizar):

- `tests/parser/parser_test.sy` — baseline M0 parser
- `tests/checker/checker_test.sy` — baseline M0 checker
- `tests/checker/m1_* … m26_*` — stubs por milestone

## Modelo alvo

```
.sy source
    → [frontend .sy] Lexer → Parser → Checker → JSON/IPC
    → [soyuz-go]     Codegen (LLVM) → binário
```

## Opção A: flag `--frontend=sy` (recomendada)

Em `soyuz-go/cmd/main.go`, antes do codegen:

1. Se `--frontend=sy` estiver ativo, executar o binário self-hosted (ou type-check + JSON dump).
2. O frontend emite JSON com `errors[]`, `warnings[]`, `nodeTypes`.
3. `soyuz-go` consome o JSON para codegen ou diagnósticos.

## Opção B: frontends independentes

Manter parser/checker Go e Soyuz em paralelo até paridade M26.

## Correções soyuz-go em progresso

Patches aplicados em `soyuz-go/internal/codegen/`:

- `emitRecordAlloc`: bitcast em stores incompatíveis
- `generateRecordLiteral`: `Map[K,V]{}` / `List[T]{}` vazios
- `emitTypeBasicConstant`: `Unknown`, `IntType`, …
- Enum payload structs nomeados + `enumVariantForPattern` por tipo do subject
- `generateIndexExpr`: indexação de `List[T]`

Pendente: codegen completo de `extend Parser` / match em enums grandes.

## Referências

- Pipeline Go: `soyuz-go/cmd/main.go`
- AST Go: `soyuz-go/internal/parser/ast.go`
- Checker Go: `soyuz-go/internal/checker/checker.go`
