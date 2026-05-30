package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestListPrintUsesToString(t *testing.T) {
	src := `
import @soyuz/prelude
fn main() {
	val xs = [1, 2, 3]
	print(xs)
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := checker.New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker: %v", res.Errors)
	}
	mod, err := New(res).Generate(prog)
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	ir := mod.String()
	if !strings.Contains(ir, "soyuz_list_to_string") {
		t.Fatalf("esperado soyuz_list_to_string no IR, obteve:\n%s", ir)
	}
}
