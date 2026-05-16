package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func charIR(t *testing.T, src string) string {
	t.Helper()
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := checker.New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
	mod, err := New(res).Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	return mod.String()
}

func TestCharLiteralEmitsI32(t *testing.T) {
	src := `fn main() -> Char { 'A' }`
	ir := charIR(t, src)
	// 'A' = 65 decimal, i32 65
	if !strings.Contains(ir, "i32 65") {
		t.Fatalf("esperado 'i32 65' no IR para 'A', obteve:\n%s", ir)
	}
}

func TestCharEscapeNewlineEmitsI32(t *testing.T) {
	src := `fn main() -> Char { '\n' }`
	ir := charIR(t, src)
	// '\n' = 10 decimal
	if !strings.Contains(ir, "i32 10") {
		t.Fatalf("esperado 'i32 10' no IR para '\\n', obteve:\n%s", ir)
	}
}

func TestCharParamAndReturn(t *testing.T) {
	src := `
fn identity(c: Char) -> Char = c
fn main() -> Char { identity('x') }
`
	ir := charIR(t, src)
	if !strings.Contains(ir, "i32") {
		t.Fatalf("esperado tipo i32 no IR, obteve:\n%s", ir)
	}
}
