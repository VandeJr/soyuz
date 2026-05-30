package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestNamedEnumConstructorArgs(t *testing.T) {
	src := `
enum Forma {
    Circulo(raio: Float)
    Retangulo(w: Float, h: Float)
}
fn main() {
    val c = Forma.Circulo(raio: 5.0)
    val r = Forma.Retangulo(w: 10.0, h: 4.0)
}`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("erros inesperados: %v", res.Errors)
	}
}

func TestNamedEnumConstructorUnknownField(t *testing.T) {
	src := `
enum Forma { Circulo(raio: Float) }
val c = Forma.Circulo(x: 1.0)`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) == 0 {
		t.Fatal("esperado erro para campo desconhecido")
	}
}
