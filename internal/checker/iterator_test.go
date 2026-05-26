package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestForInMap(t *testing.T) {
	src := `
fn sumKeys(m: Map[Int, Int]) -> Int {
	var total: Int = 0
	for k in m {
		total = total + k
	}
	return total
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)
	if len(result.Errors) > 0 {
		t.Fatalf("for-in em Map não deve gerar erros: %v", result.Errors)
	}
}

func TestListIterMethod(t *testing.T) {
	src := `
fn count(items: List[Int]) -> Int {
	var n: Int = 0
	for x in items.iter() {
		n = n + x
	}
	return n
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)
	if len(result.Errors) > 0 {
		t.Fatalf("List.iter() não deve gerar erros: %v", result.Errors)
	}
}

func TestIteratorNextType(t *testing.T) {
	src := `
fn first(items: List[Int]) -> Int? {
	val it = items.iter()
	return it.next()
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)
	if len(result.Errors) > 0 {
		t.Fatalf("Iterator.next() não deve gerar erros: %v", result.Errors)
	}
}
