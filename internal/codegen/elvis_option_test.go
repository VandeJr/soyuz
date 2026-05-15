package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func compileElvis(t *testing.T, src string) string {
	t.Helper()
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
	return mod.String()
}

func TestElvisCodegen(t *testing.T) {
	src := `
fn add(x: Int?, y: Int) -> Int {
	val v: Int = x ?: 10
	return v + y
}
`
	ir := compileElvis(t, src)

	if !strings.Contains(ir, "elvis_some:") {
		t.Error("esperado bloco elvis_some no IR")
	}
	if !strings.Contains(ir, "elvis_none:") {
		t.Error("esperado bloco elvis_none no IR")
	}
	if !strings.Contains(ir, "elvis_merge:") {
		t.Error("esperado bloco elvis_merge no IR")
	}
	if !strings.Contains(ir, "phi i64") {
		t.Error("esperado phi node i64 no IR para elvis")
	}
}

func TestMatchSomeBindingCodegen(t *testing.T) {
	src := `
fn add(x: Int?, y: Int) -> Int {
	return match x {
		Some(v) => v + y
		None => y
	}
}
`
	ir := compileElvis(t, src)

	// Must have conditional branch on the Some tag (0).
	if !strings.Contains(ir, "icmp eq i64") {
		t.Error("esperado comparação de tag i64 no IR para match Some")
	}
	if !strings.Contains(ir, "match_Some_ok:") {
		t.Error("esperado bloco match_Some_ok no IR")
	}
}
