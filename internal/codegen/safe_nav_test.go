package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func compileSafeNav(t *testing.T, src string) string {
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

func TestSafeNavCodegen(t *testing.T) {
	src := `
record User {
	nome: String
}

fn label(user: User?) -> String {
	return user?.nome ?: "anon"
}
`
	ir := compileSafeNav(t, src)

	if !strings.Contains(ir, "safenav_some:") {
		t.Error("esperado bloco safenav_some no IR")
	}
	if !strings.Contains(ir, "safenav_none:") {
		t.Error("esperado bloco safenav_none no IR")
	}
	if !strings.Contains(ir, "safenav_merge:") {
		t.Error("esperado bloco safenav_merge no IR")
	}
}
