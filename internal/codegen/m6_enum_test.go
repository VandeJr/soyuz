package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func compileM6(t *testing.T, src string) string {
	t.Helper()
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	return mod.String()
}

func TestM6EnumSimple(t *testing.T) {
	src := `
enum Cor {
	Vermelho
	Verde
	Azul
}
fn main() -> Int {
	val c = Vermelho
	return 0
}
`
	ir := compileM6(t, src)
	if !strings.Contains(ir, "Cor") {
		t.Error("expected enum type Cor in IR")
	}
}

func TestM6EnumMatch(t *testing.T) {
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
		_ => 3
	}
}
fn main() -> Int {
	val c = Vermelho
	return classifica(c)
}
`
	ir := compileM6(t, src)
	if !strings.Contains(ir, "classifica") {
		t.Error("expected classifica function in IR")
	}
}

func TestM6EnumWithFields(t *testing.T) {
	src := `
enum Forma {
	Circulo { raio: Float }
	Retangulo { largura: Float }
}
fn main() -> Int {
	val f = Circulo(5.0)
	return 0
}
`
	ir := compileM6(t, src)
	if !strings.Contains(ir, "Forma") {
		t.Error("expected enum type Forma in IR")
	}
}

func TestM6EnumDotSyntax(t *testing.T) {
	src := `
enum Cor {
	Vermelho
	Verde
}
fn main() -> Int {
	val c = Cor.Vermelho
	return 0
}
`
	ir := compileM6(t, src)
	if !strings.Contains(ir, "Cor") {
		t.Error("expected enum type Cor in IR")
	}
}

func TestM6EnumDotSyntaxWithArgs(t *testing.T) {
	src := `
enum Forma {
	Circulo { raio: Float }
}
fn main() -> Int {
	val f = Forma.Circulo(3.14)
	return 0
}
`
	ir := compileM6(t, src)
	if !strings.Contains(ir, "Forma") {
		t.Error("expected enum type Forma in IR")
	}
}

func TestM6TwoEnumsSameVariantName(t *testing.T) {
	src := `
enum Status {
	Ok
	Erro
}
enum Resultado {
	Ok
	Falha
}
fn main() -> Int {
	val s = Status.Ok
	val r = Resultado.Ok
	return 0
}
`
	ir := compileM6(t, src)
	if !strings.Contains(ir, "Status") {
		t.Error("expected Status enum in IR")
	}
	if !strings.Contains(ir, "Resultado") {
		t.Error("expected Resultado enum in IR")
	}
}
