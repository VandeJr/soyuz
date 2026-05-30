package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// TestWhenVariantBeforeCatchall verifies when-guarded variants run before catch-all (P-13).
func TestWhenVariantBeforeCatchall(t *testing.T) {
	src := `
fn pick(x: Int) = "default"
fn pick(x: Int) when x > 0 = "positive"
fn main() -> String = pick(5)
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := checker.New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
	mod, err := New(res).Generate(prog)
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	ir := mod.String()
	if !strings.Contains(ir, "positive") {
		t.Fatalf("esperado corpo da variante when no IR, obteve:\n%s", ir)
	}
}
