package lsp

import (
	"strings"
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func parseAndFormat(src string) string {
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	return formatProgram(prog, src)
}

// ─── Formatter: CharLiteral ───────────────────────────────────────────────────

func TestFormatCharLiteral(t *testing.T) {
	src := "val c = 'a'"
	got := parseAndFormat(src)
	if !strings.Contains(got, "'a'") {
		t.Fatalf("esperado char literal 'a' formatado, obteve:\n%s", got)
	}
}

func TestFormatCharLiteralEscapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{"val c = '\\n'", `'\n'`},
		{"val c = '\\t'", `'\t'`},
		{"val c = '\\''", `'\''`},
	}
	for _, tc := range cases {
		got := parseAndFormat(tc.src)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("src=%q: esperado %q no output, obteve:\n%s", tc.src, tc.want, got)
		}
	}
}

// ─── Formatter: WhenGuard ─────────────────────────────────────────────────────

func TestFormatFuncDeclWhenGuard(t *testing.T) {
	src := "fn factorial(n: Int) when n > 0 -> Int = n * factorial(n - 1)"
	got := parseAndFormat(src)
	if !strings.Contains(got, "when n > 0") {
		t.Fatalf("esperado 'when n > 0' no output, obteve:\n%s", got)
	}
}

func TestFormatFuncDeclNoWhenGuard(t *testing.T) {
	src := "fn id(x: Int) -> Int = x"
	got := parseAndFormat(src)
	if strings.Contains(got, "when") {
		t.Fatalf("não esperado 'when' no output, obteve:\n%s", got)
	}
}

// ─── Formatter: class implements syntax ──────────────────────────────────────

func TestFormatClassImplements(t *testing.T) {
	src := `pub class IOError : Error {
  pub val msg: String
  pub fn message(self) -> String = self.msg
  pub fn code(self) -> Int = 0
}`
	got := parseAndFormat(src)
	// Must use ':' not 'impl'
	if strings.Contains(got, " impl ") {
		t.Fatalf("formatter não deve escrever 'impl', obteve:\n%s", got)
	}
	if !strings.Contains(got, ": Error") {
		t.Fatalf("esperado ': Error' no output, obteve:\n%s", got)
	}
}

// ─── Formatter: idempotency ───────────────────────────────────────────────────

func TestFormatIdempotentFuncExprBody(t *testing.T) {
	// Format output should re-parse to the same structure.
	src := `fn add(a: Int, b: Int) -> Int = a + b`
	pass1 := parseAndFormat(src)
	pass2 := parseAndFormat(pass1)
	if pass1 != pass2 {
		t.Fatalf("formato não é idempotente:\npass1:\n%s\npass2:\n%s", pass1, pass2)
	}
}

func TestFormatIdempotentClass(t *testing.T) {
	src := `pub class Foo : Error {
    pub val msg: String
    pub fn message(self) -> String = self.msg
    pub fn code(self) -> Int = 0
}`
	pass1 := parseAndFormat(src)
	pass2 := parseAndFormat(pass1)
	if pass1 != pass2 {
		t.Fatalf("formato não é idempotente:\npass1:\n%s\npass2:\n%s", pass1, pass2)
	}
}

// ─── walkAST: WhenGuard reachability ─────────────────────────────────────────

func TestWalkASTVisitsWhenGuard(t *testing.T) {
	src := "fn f(n: Int) when n > 0 -> Int = n"
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()

	var idents []string
	walkAST(prog, func(n parser.Node) {
		if id, ok := n.(*parser.Identifier); ok {
			idents = append(idents, id.Name)
		}
	})

	found := false
	for _, id := range idents {
		if id == "n" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("walkAST não visitou identifiers dentro de WhenGuard; idents encontrados: %v", idents)
	}
}

// ─── Formatter: import grouping ──────────────────────────────────────────────

func TestFormatImportsNoBlankLineBetween(t *testing.T) {
	src := `import (
    { readFile } from "@soyuz/fs"
    { Lexer } from "lib/lexer/lexer"
)

fn main() {}`
	got := parseAndFormat(src)
	if !strings.Contains(got, "import (") {
		t.Fatalf("esperado bloco import, obteve:\n%s", got)
	}
	if strings.Contains(got, "readFile}\n\nimport") {
		t.Fatalf("formatter inseriu blank line entre specs do import:\n%s", got)
	}
	if !strings.Contains(got, ")\n\nfn") && !strings.Contains(got, ")\n\n\nfn") {
		t.Fatalf("esperado blank line entre import e fn, obteve:\n%s", got)
	}
}

// ─── walkAST: param defaults reachability ────────────────────────────────────

func TestWalkASTVisitsParamDefaults(t *testing.T) {
	src := "fn greet(name: String = \"mundo\") -> String = name"
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()

	var strs []string
	walkAST(prog, func(n parser.Node) {
		if s, ok := n.(*parser.StringLiteral); ok {
			strs = append(strs, s.Value)
		}
	})

	found := false
	for _, s := range strs {
		if s == "mundo" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("walkAST não visitou default param StringLiteral; strings encontradas: %v", strs)
	}
}
