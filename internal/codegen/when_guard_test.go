package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestWhenGuardCodegen(t *testing.T) {
	src := `
	fn abs(x: Int) when x < 0 = -x
	fn abs(x: Int) = x

	fn main() -> Int {
		return abs(-10) + abs(5)
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
	
	// Check if we have variant blocks and conditional branch for the guard
	if !strings.Contains(irStr, "v0_guard_ok:") {
		t.Error("expected v0_guard_ok block in IR")
	}
	if !strings.Contains(irStr, "br i1 %") {
		t.Error("expected conditional branch for guard")
	}
	if !strings.Contains(irStr, "variant_0_body:") {
		t.Error("expected variant_0_body block")
	}
	if !strings.Contains(irStr, "variant_1_body:") {
		t.Error("expected variant_1_body block")
	}
}

func TestMatchWhenGuardCodegen(t *testing.T) {
	src := `
	fn main() -> String {
		val x = 10
		return match x {
			n when n < 0 => "negativo"
			_ => "positivo"
		}
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
	
	if !strings.Contains(irStr, "match_arm_0_pattern_ok:") {
		t.Error("expected match_arm_0_pattern_ok block")
	}
	if !strings.Contains(irStr, "br i1 %") {
		t.Error("expected conditional branch for match guard")
	}
}
