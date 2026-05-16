package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// stringExtPreamble prefixes src with the extern declarations and
// StringExtensions class so String method dispatch works in tests
// without importing std/lib/string.sy.
func stringExtPreamble(src string) string {
	return `
extern fn soyuz_str_len(s: String) -> Int
extern fn soyuz_str_substring(s: String, start: Int, end: Int) -> String
extern fn soyuz_str_contains(s: String, sub: String) -> Bool
extern fn soyuz_str_to_upper(s: String) -> String
extern fn soyuz_str_index_of(s: String, sub: String) -> Int

pub class StringExtensions {
  pub fn len(self) -> Int = soyuz_str_len(self)
  pub fn substring(self, start: Int, end: Int) -> String = soyuz_str_substring(self, start, end)
  pub fn contains(self, sub: String) -> Bool = soyuz_str_contains(self, sub)
  pub fn toUpper(self) -> String = soyuz_str_to_upper(self)
  pub fn indexOf(self, sub: String) -> Int = soyuz_str_index_of(self, sub)
}
` + src
}

func checkString(src string) []TypeError {
	tokens := lexer.Tokenize(stringExtPreamble(src))
	prog := parser.New(tokens).Parse()
	return New().Check(prog).Errors
}

func TestStringLenMethod(t *testing.T) {
	src := `fn main() { val n: Int = "hello".len() }`
	if errs := checkString(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestStringSubstringMethod(t *testing.T) {
	src := `fn main() { val s: String = "hello".substring(0, 3) }`
	if errs := checkString(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestStringContainsMethod(t *testing.T) {
	src := `fn main() { val b: Bool = "hello world".contains("world") }`
	if errs := checkString(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestStringToUpperMethod(t *testing.T) {
	src := `fn main() { val s: String = "hello".toUpper() }`
	if errs := checkString(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestStringIndexOfMethod(t *testing.T) {
	src := `fn main() { val i: Int = "hello".indexOf("ll") }`
	if errs := checkString(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestStringUnknownMethodError(t *testing.T) {
	src := `fn main() { "hello".nonexistent() }`
	errs := checkString(src)
	if len(errs) == 0 {
		t.Fatal("esperava erro para método inexistente em String")
	}
}

func TestStringMethodChain(t *testing.T) {
	src := `fn main() { val n: Int = "hello".substring(0, 3).len() }`
	if errs := checkString(src); len(errs) > 0 {
		t.Fatalf("erros inesperados em chain: %v", errs)
	}
}
