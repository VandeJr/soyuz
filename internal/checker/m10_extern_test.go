package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func checkExtern(src string) []TypeError {
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog).Errors
}

func TestExternDeclRegistersInScope(t *testing.T) {
	src := `
extern fn soyuz_print_int(n: Int)
fn main() {
    soyuz_print_int(42)
}
`
	if errs := checkExtern(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestExternDeclWithReturnType(t *testing.T) {
	src := `
extern fn soyuz_str_len(s: String) -> Int
fn main() {
    val n: Int = soyuz_str_len("hello")
}
`
	if errs := checkExtern(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestExternDeclPubExposed(t *testing.T) {
	src := `
pub extern fn soyuz_exit(code: Int)
fn main() {
    soyuz_exit(0)
}
`
	if errs := checkExtern(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestExternDeclMultipleParams(t *testing.T) {
	src := `
extern fn soyuz_print_float(x: Float)
extern fn soyuz_print_bool(b: Bool)
fn main() {
    soyuz_print_float(3.14)
    soyuz_print_bool(true)
}
`
	if errs := checkExtern(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}
