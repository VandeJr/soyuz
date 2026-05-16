package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func checkChar(src string) []TypeError {
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog).Errors
}

func TestCharLiteralType(t *testing.T) {
	src := `fn main() { val c: Char = 'a' }`
	if errs := checkChar(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestCharTypeInference(t *testing.T) {
	src := `fn main() { val c = 'z' }`
	if errs := checkChar(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestCharEscapeSequence(t *testing.T) {
	src := `fn main() { val nl = '\n' }`
	if errs := checkChar(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestCharAsFuncParam(t *testing.T) {
	src := `
fn identity(c: Char) -> Char = c
fn main() { val r: Char = identity('x') }
`
	if errs := checkChar(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}
