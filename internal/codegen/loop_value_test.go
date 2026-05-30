package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestLoopBreakValueCodegen(t *testing.T) {
	src := `
fn main() -> Int {
    var t = 0
    val r = loop {
        t = t + 1
        if t == 5 { break t * 10 }
    }
    return r
}`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	chk := checker.New()
	result := chk.Check(prog)
	if len(result.Errors) > 0 {
		t.Fatalf("checker errors: %v", result.Errors)
	}
	g := New(result)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	ir := mod.String()
	if !strings.Contains(ir, "loop_after") {
		t.Errorf("esperado bloco loop_after no IR")
	}
	if !strings.Contains(ir, "loop_body") {
		t.Errorf("esperado bloco loop_body no IR")
	}
}
