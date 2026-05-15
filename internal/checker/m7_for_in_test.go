package checker

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
	"testing"
)

func TestForInListIntBinding(t *testing.T) {
	src := `
fn main() -> Int {
	val nums = [1, 2, 3]
	var sum = 0
	for n in nums {
		sum = sum + n
	}
	return sum
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestForInRange(t *testing.T) {
	src := `
fn main() -> Int {
	var acc = 0
	for i in 0..10 {
		acc = acc + i
	}
	return acc
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestForInListStringBinding(t *testing.T) {
	src := `
fn main() -> Int {
	val words = ["hello", "world"]
	for w in words {
		print(w)
	}
	return 0
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestForInNonIterableError(t *testing.T) {
	src := `
fn main() -> Int {
	val x = 42
	for n in x {
		print(n)
	}
	return 0
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) == 0 {
		t.Fatal("esperava erro ao iterar sobre Int")
	}
}
