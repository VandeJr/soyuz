package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestCurryCodegen(t *testing.T) {
	src := `
	fn multiplicar(a: Int, b: Int) -> Int = a * b

	fn main() -> Int {
		val dobrar = multiplicar(_, 2)
		val triplicar = multiplicar(_, 3)
		return dobrar(5) + triplicar(4)
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()

	// Should generate at least one lambda function
	if !strings.Contains(irStr, "__lambda_") {
		t.Error("expected lambda function for curried call")
	}
	// Should contain the closure allocation
	if !strings.Contains(irStr, "soyuz_alloc") {
		t.Error("expected soyuz_alloc for closure")
	}
}

func TestCurryWithCapture(t *testing.T) {
	src := `
	fn somar(a: Int, b: Int) -> Int = a + b

	fn main() -> Int {
		val offset = 10
		val somarOffset = somar(offset, _)
		return somarOffset(5)
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()

	if !strings.Contains(irStr, "__lambda_") {
		t.Error("expected lambda function for curried call")
	}
}

func TestHOFInferenceCodegen(t *testing.T) {
	src := `
	fn aplicar(f: (Int) -> Int, x: Int) -> Int = f(x)

	fn main() -> Int {
		return aplicar(fn(n) => n * 2, 5)
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()

	if !strings.Contains(irStr, "__lambda_") {
		t.Error("expected lambda function for anonymous function in HOF call")
	}
}
