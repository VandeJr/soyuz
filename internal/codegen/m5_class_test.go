package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestM5CodegenFieldDefaults(t *testing.T) {
	src := `
class Contador {
	var count: Int = 0
	var label: String = "cnt"
	pub fn incrementar(self) { }
}
fn main() -> Int {
	val c1 = Contador {}
	val c2 = Contador { label: "x" }
	val c3 = Contador { count: 10 }
	return 0
}
`
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

	irStr := mod.String()
	if !strings.Contains(irStr, "Contador_incrementar") {
		t.Error("expected Contador_incrementar in IR")
	}
}

func TestM5CodegenMethodOverloading(t *testing.T) {
	src := `
class Contador {
	var count: Int = 0
	pub fn incrementar(self) { }
	pub fn incrementar(self, n: Int) { }
}
fn main() -> Int {
	val c = Contador {}
	c.incrementar()
	c.incrementar(5)
	return 0
}
`
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

	irStr := mod.String()
	// Overloaded methods get mangled: Contador_incrementar_0 and Contador_incrementar_1
	if !strings.Contains(irStr, "Contador_incrementar_0") {
		t.Error("expected Contador_incrementar_0 (0-arg variant) in IR")
	}
	if !strings.Contains(irStr, "Contador_incrementar_1") {
		t.Error("expected Contador_incrementar_1 (1-arg variant) in IR")
	}
}

func TestM5CodegenPubMethodWithInterface(t *testing.T) {
	src := `
interface Contavel {
	fn incrementar(self)
}
class Contador : Contavel {
	var count: Int = 0
	pub fn incrementar(self) { }
	fn reset(self) { }
}
fn main() -> Int {
	val c: Contavel = Contador {}
	c.incrementar()
	return 0
}
`
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

	irStr := mod.String()
	if !strings.Contains(irStr, "__vtable_Contador_Contavel") {
		t.Error("expected vtable for Contador_Contavel in IR")
	}
}
