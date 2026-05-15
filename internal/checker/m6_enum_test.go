package checker

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
	"testing"
)

func TestEnumSimpleDecl(t *testing.T) {
	src := `
enum Cor {
	Vermelho
	Verde
	Azul
}
val c = Vermelho
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestEnumWithFields(t *testing.T) {
	src := `
enum Forma {
	Circulo { raio: Float }
	Retangulo { largura: Float, altura: Float }
}
val f = Circulo(5.0)
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestEnumMatchExhaustive(t *testing.T) {
	src := `
enum Cor {
	Vermelho
	Verde
	Azul
}
fn classifica(c: Cor) -> Int {
	return match c {
		Vermelho => 1
		Verde => 2
		Azul => 3
	}
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestEnumMatchNonExhaustiveError(t *testing.T) {
	src := `
enum Cor {
	Vermelho
	Verde
	Azul
}
fn classifica(c: Cor) -> Int {
	return match c {
		Vermelho => 1
		Verde => 2
	}
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) == 0 {
		t.Fatal("esperado erro de match não exaustivo, mas nenhum foi encontrado")
	}
}

func TestEnumDotSyntax(t *testing.T) {
	src := `
enum Cor {
	Vermelho
	Verde
}
val c = Cor.Vermelho
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors com Enum.Variante: %v", res.Errors)
	}
}

func TestEnumDotSyntaxWithArgs(t *testing.T) {
	src := `
enum Forma {
	Circulo { raio: Float }
	Retangulo { largura: Float }
}
val f = Forma.Circulo(5.0)
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors com Forma.Circulo(5.0): %v", res.Errors)
	}
}

func TestTwoEnumsSameVariantName(t *testing.T) {
	src := `
enum Status {
	Ok
	Erro
}
enum Resultado {
	Ok
	Falha
}
val s: Status = Status.Ok
val r: Resultado = Resultado.Ok
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors com dois enums com variante Ok: %v", res.Errors)
	}
}
