package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

const stringExtPreamble = `
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
`

func stringIR(t *testing.T, src string) string {
	t.Helper()
	full := stringExtPreamble + src
	tokens := lexer.Tokenize(full)
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

func TestStringLenMethodIR(t *testing.T) {
	src := `fn main() -> Int { "hello".len() }`
	ir := stringIR(t, src)
	if !strings.Contains(ir, "call i64 @soyuz_str_len") {
		t.Fatalf("esperado call a soyuz_str_len no IR, obteve:\n%s", ir)
	}
}

func TestStringSubstringMethodIR(t *testing.T) {
	src := `fn main() -> String { "hello".substring(0, 3) }`
	ir := stringIR(t, src)
	if !strings.Contains(ir, "@soyuz_str_substring") {
		t.Fatalf("esperado call a soyuz_str_substring no IR, obteve:\n%s", ir)
	}
}

func TestStringContainsMethodIR(t *testing.T) {
	src := `fn main() -> Bool { "hello world".contains("world") }`
	ir := stringIR(t, src)
	if !strings.Contains(ir, "@soyuz_str_contains") {
		t.Fatalf("esperado call a soyuz_str_contains no IR, obteve:\n%s", ir)
	}
}

func TestStringExtensionsMethodBodyCallsExtern(t *testing.T) {
	src := `fn main() -> String { "hello".toUpper() }`
	ir := stringIR(t, src)
	// StringExtensions_toUpper should be defined and call soyuz_str_to_upper
	if !strings.Contains(ir, "StringExtensions_toUpper") {
		t.Fatalf("esperado StringExtensions_toUpper no IR, obteve:\n%s", ir)
	}
	if !strings.Contains(ir, "@soyuz_str_to_upper") {
		t.Fatalf("esperado call a soyuz_str_to_upper no IR, obteve:\n%s", ir)
	}
}
