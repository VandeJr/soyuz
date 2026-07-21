# Estabilização do self-hosting

Este arquivo acompanha o plano M0–M17. Um milestone só muda para `done` quando
seu gate objetivo passa; implementação parcial permanece `in_progress`.

| Milestone | Estado | Evidência atual |
|---|---|---|
| M0 | done | `bootstrap-verify.sh` passa 29/29; gates usam `/tmp` |
| M1 | done | runner exige 5 testes; corpus compara saída e exit code |
| M2 | done | `typed_ir_invariants_test.sy` 4/4 + validação Clang |
| M3 | in_progress | backend isolado 5/5; AST 4/4; módulo em duas passagens 2/2 e executável via Clang |
| M4 | in_progress | `if`, `while`, `loop`, range exclusivo, `break` e `continue` geram IR executável |
| M5–M17 | pending | dependem da sequência crítica do plano |

## Estado ativo do M3

As arestas recursivas diretas de `BinaryExprData`, `UnaryExprData` e
`AssignExprData` agora usam storage indireto por lista, removendo os casts
inválidos de `%Node*` para `i64` nesse subconjunto. A regressão em
`tools/fixtures/typed_ast_expr_test.sy` passa, inclusive rejeitando calls sem
assinatura conhecida. Declarações, parâmetros, corpos de expressão e calls
entre funções já chegam ao backend tipado. Ainda falta suportar o restante do
subconjunto exercitado por `funcoes.sy` e ligar o planner nativo ao dispatch.

O módulo tipado coleta assinaturas antes dos corpos, resolve calls forward,
emite `print`/`Unit`, aceita corpos em bloco com declarações, expression
statements e `return`, e cria um wrapper ABI `i32 @main` para `fn main()`. O
gate compila e executa o LLVM gerado, obtendo `native hello`. A integração do
planner foi criada em `planTypedCodegenBuild` e `planTypedFullBuild`; o dispatch
de CLI ainda usa o caminho legado.

O `funcoes.sy` do bootstrap foi diagnosticado com AddressSanitizer: a função
`log` chama `soyuz_release` sobre uma string literal global e sofre
global-buffer-overflow. O checkout Go permanece intocado; o caminho nativo não
emite esse release e será o caminho da correção funcional.

## Estado ativo do M4

Escopos do lowering tipado agora formam uma cadeia pai-filho. Declarações de
blocos não escapam, enquanto leituras e atribuições resolvem no pai. Isso
removeu o snapshot compartilhado de `Map` que causava use-after-free ao sair de
um `if`.

O backend tipado já emite blocos e terminadores reais para `if`, `while`,
`loop`, `break`, `continue` e `for` sobre ranges inteiros. O renderer executável
soma `0..4`, exercita os alvos de `continue` e `break`, valida o resultado em um
branch e imprime `native hello`. O LLVM é validado e executado pelo gate, e o
renderer também passa sob AddressSanitizer.

O runtime C local agora reconhece o prefixo estático legado do bootstrap e
compara/hacheia strings pelo comprimento explícito, sem leitura além do buffer.
A montagem de módulos usa `soyuz_str_concat` em vez da interpolação com buffer
fixo de 1 KiB. Ainda faltam ranges inclusivos no corpus, loops com valor/`phi`,
loops aninhados e `for` sobre collections para concluir M4.

## Gates

```bash
soyuz build
bash tools/typed-ir-verify.sh
bash tools/driver-test-runner-check.sh
bash tools/selfhost-regression-check.sh
bash tools/feature-corpus-verify.sh
SOYUZ_GO_ROOT=/caminho/soyuz-go bash tools/bootstrap-verify.sh
bash tools/selfhost-verify.sh
```

Falhas conhecidas não contam como sucesso: os dois últimos gates relevantes
continuam retornando código não zero até a independência funcional existir.
