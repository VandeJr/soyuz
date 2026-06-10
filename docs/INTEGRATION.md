# Integração soyuz-go ↔ frontend Soyuz (self-host)

Este documento descreve como conectar o frontend em Soyuz Lang (`/home/vand/Projects/soyuz`) ao compilador de produção (`soyuz-go`) quando o bootstrap estiver estável.

## Estado atual

| Camada | Soyuz (.sy) | Bootstrap (`soyuz` no PATH) |
|--------|-------------|------------------------------|
| Lexer | Completo + testes | Compila e roda |
| AST / Parser / Checker | ~6500 LOC portados | **Não compila ainda** — type-check trava em `@ast/ast` (enum `Node` recursivo) |

O binário `soyuz` compila `main.sy` (lexer-only) em ~1s. Importar `@ast/ast` dispara type-check que não termina em ~20s (limitação conhecida do bootstrap). O pacote `@parser/*` foi separado de `@ast/*` para evitar recompilar parser/checker ao testar só tipos AST.

**Notas técnicas (jun/2026):**
- `pub var` em módulo + atribuição no codegen falha (`undefined variable in assignment`); IDs de nó ficam no `Parser.nextNodeId`.
- Testes lexer: `testRanges` deve rodar **antes** de `testSemicolonInsertion` (workaround de bug no runtime/bootstrap ao encadear muitos `tokenize` após ASI).

## Modelo alvo

```
.so sy source
    → [frontend .sy] Lexer → Parser → Checker → JSON/IPC
    → [soyuz-go]     Codegen (LLVM) → binário
```

## Opção A: flag `--frontend=sy` (recomendada)

Em `soyuz-go/cmd/main.go`, antes do codegen:

1. Se `--frontend=sy` estiver ativo, executar o binário self-hosted:
   ```bash
   soyuz run /path/to/soyuz/tools/check.sy -- --json source.sy
   ```
2. O frontend emite JSON com:
   - `errors[]`, `warnings[]`
   - `nodeTypes` (mapa `nodeId → typeString`)
   - opcional: dump AST serializado
3. `soyuz-go` consome o JSON e:
   - aborta se houver erros de parse/check
   - ou traduz AST JSON → `parser.Program` Go (fase 2) para codegen existente

### Esboço CLI

```bash
# Fase 1: só diagnósticos
soyuz build --frontend=sy app.sy

# Fase 2: codegen via tradutor
soyuz build --frontend=sy --ast-bridge=go app.sy
```

## Opção B: frontends independentes

Manter parser/checker Go e Soyuz em paralelo até paridade M26. Validar com testes Go portados (`tests/checker/*.sy`) executados pelo bootstrap quando a compilação do frontend estabilizar.

## Desbloqueio do bootstrap

Prioridades para o compilador Go:

1. Type-check/codegen eficiente em enums recursivos grandes (`Node`, `Pattern`, `TypeExpr`)
2. Ou refactor do AST self-hosted: IDs + tabelas laterais em vez de payloads recursivos profundos no type checker

## Testes

```bash
# Lexer (funciona hoje)
soyuz test test_runner.sy

# Checker (quando bootstrap compilar o frontend)
soyuz test tests/checker/checker_test.sy
```

## Referências

- Pipeline Go: `soyuz-go/cmd/main.go`
- AST Go: `soyuz-go/internal/parser/ast.go`
- Checker Go: `soyuz-go/internal/checker/checker.go`
