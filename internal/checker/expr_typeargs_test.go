package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestExprGenericRecordLiteralCheck(t *testing.T) {
	src := `
record Par[T, U] { primeiro: T, segundo: U }
fn main() {
    val coords = Par[Int, Int] { primeiro: 10, segundo: 20 }
}`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("erros: %v", res.Errors)
	}
}

func TestExprGenericFuncCallCheck(t *testing.T) {
	src := `
fn identidade[T](x: T) -> T = x
fn main() {
    val z = identidade[Bool](true)
}`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("erros: %v", res.Errors)
	}
}
