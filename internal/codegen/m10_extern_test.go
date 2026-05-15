package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func externIR(t *testing.T, src string) string {
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

func TestExternDeclEmitsLLVMDeclaration(t *testing.T) {
	src := `
extern fn soyuz_print_int(n: Int)
fn main() {
    soyuz_print_int(99)
}
`
	ir := externIR(t, src)
	if !strings.Contains(ir, "soyuz_print_int") {
		t.Fatalf("esperado 'soyuz_print_int' no IR, obteve:\n%s", ir)
	}
	if !strings.Contains(ir, "call") {
		t.Fatalf("esperado chamada no IR, obteve:\n%s", ir)
	}
}

func TestExternDeclWithReturnTypeInCodegen(t *testing.T) {
	src := `
extern fn soyuz_str_len(s: String) -> Int
fn main() -> Int {
    soyuz_str_len("hello")
}
`
	ir := externIR(t, src)
	if !strings.Contains(ir, "soyuz_str_len") {
		t.Fatalf("esperado 'soyuz_str_len' no IR, obteve:\n%s", ir)
	}
}

func TestExternDeclMultipleFunctions(t *testing.T) {
	src := `
extern fn soyuz_print_int(n: Int)
extern fn soyuz_print_float(x: Float)
extern fn soyuz_print_bool(b: Bool)
fn main() {
    soyuz_print_int(1)
    soyuz_print_float(2.0)
    soyuz_print_bool(true)
}
`
	ir := externIR(t, src)
	for _, name := range []string{"soyuz_print_int", "soyuz_print_float", "soyuz_print_bool"} {
		if !strings.Contains(ir, name) {
			t.Errorf("esperado '%s' no IR", name)
		}
	}
}
