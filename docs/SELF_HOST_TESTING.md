# Testes do compilador autoportado

O self-hosting é aceito somente quando uma geração produzida por Soyuz recompila
`main.sy` sem encontrar `soyuz` ou `soyuz-go` no ambiente. O bootstrap Go pode
criar apenas a primeira geração (`vN`).

## Gates locais

```bash
# Paridade com o bootstrap e fixed-point atual
SOYUZ_GO_ROOT=/caminho/para/soyuz-go bash tools/bootstrap-verify.sh

# vN -> vN+1 -> vN+2 sem bootstrap; preserva logs e fingerprints na falha
bash tools/selfhost-verify.sh --keep-artifacts

# Executa os programas de feature-tests e exige saída do programa
bash tools/feature-corpus-verify.sh

# Parser, checker e codegen: bloqueios antes de habilitar imports no runner
bash tools/selfhost-regression-check.sh

# Invariantes do IR tipado e validação sintática pelo Clang
bash tools/typed-ir-verify.sh
```

O gate de IR executa duas suítes Soyuz (IR básico e expressões/funções), gera um
módulo LLVM usando o próprio backend, valida-o com o Clang, linka e executa o
artefato. A saída funcional esperada atualmente é `answer=42 hello`.

`selfhost-verify.sh --no-bootstrap --compiler /caminho/vN` repete o gate a
partir de um binário já produzido. O script remove `soyuz` do `PATH`, define
`SOYUZ_GO_ROOT` para um caminho inexistente e compara hash, seções ELF e símbolos
de `vN`, `vN+1` e `vN+2`. Em falhas ele preserva logs em `/tmp/soyuz-selfhost.*`.

## Situação atual

O gate independente falha até `main.sy` deixar de chamar `cliOsExecShell` para
`soyuz build`. A camada de IR agora rejeita valores sem tipo, stores incompatíveis
e terminadores duplicados; `tools/typed-ir-verify.sh` executa a suíte isolada e
valida módulos representativos com o Clang. Parser e checker ainda não podem entrar
no runner enquanto o codegen não suportar o enum recursivo `Node`, `Parser` e
operações de lista. O backend independente já executa expressões e funções sem AST;
`typed_ast_expr_test.sy` mantém a conexão AST como regressão obrigatória.
O corpus também exige que `soyuz run` execute o binário gerado, não só compile.
As saídas esperadas ficam em `feature-tests/expected/`; endereços hexadecimais
são normalizados para `<ptr>`. `extensions`, `funcoes` e `generics` permanecem
registrados em `tools/selfhost-known-failures.txt`, mas continuam fazendo o gate
retornar falha até serem corrigidos.

## Integração contínua

O CI precisa receber um artefato bootstrap Go confiável para `bootstrap-parity`.
Este repositório não contém uma referência limpa e compilável desse bootstrap:
a branch Go local disponível falha em `go test ./...`. Quando esse artefato ou
repositório for definido, publique `vN` para `selfhost-independent`, que deve
rodar `tools/selfhost-verify.sh --no-bootstrap --compiler <vN>` e o corpus.
