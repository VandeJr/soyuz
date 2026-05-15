package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func compileM7(t *testing.T, src string) string {
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

func TestM7ForInListInt(t *testing.T) {
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
	ir := compileM7(t, src)
	if !strings.Contains(ir, "soyuz_list_get") {
		t.Error("expected soyuz_list_get in IR for for-in list")
	}
	if !strings.Contains(ir, "forl_cond") {
		t.Error("expected forl_cond block in IR for for-in list")
	}
}

func TestM7ForInRange(t *testing.T) {
	src := `
fn main() -> Int {
	var sum = 0
	for i in 0..5 {
		sum = sum + i
	}
	return sum
}
`
	ir := compileM7(t, src)
	if !strings.Contains(ir, "for_cond") {
		t.Error("expected for_cond block in IR for for-in range")
	}
}

func TestM7ForInListString(t *testing.T) {
	src := `
fn main() -> Int {
	val words = ["hello", "world"]
	for w in words {
		print(w)
	}
	return 0
}
`
	ir := compileM7(t, src)
	if !strings.Contains(ir, "soyuz_list_get") {
		t.Error("expected soyuz_list_get in IR for string list for-in")
	}
}

func TestM7ForInListAccumulate(t *testing.T) {
	src := `
fn soma(lista: List[Int]) -> Int {
	var acc = 0
	for x in lista {
		acc = acc + x
	}
	return acc
}
`
	ir := compileM7(t, src)
	if !strings.Contains(ir, "soyuz_list_get") {
		t.Error("expected soyuz_list_get in IR")
	}
}
