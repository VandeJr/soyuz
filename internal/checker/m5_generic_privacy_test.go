package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// TestM5PrivateMethodOnSpecializedSelf verifies private methods via self on generic classes.
func TestM5PrivateMethodOnSpecializedSelf(t *testing.T) {
	src := `
class Box[T] {
	fn secret(self) -> Int = 1
	pub fn go(self) {
		self.secret()
	}
}
val b = Box[Int] {}
b.go()
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)
	for _, e := range result.Errors {
		if containsStr(e.Message, "privado") {
			t.Fatalf("método privado deve ser acessível via self em classe genérica: %s", e.Message)
		}
	}
}
