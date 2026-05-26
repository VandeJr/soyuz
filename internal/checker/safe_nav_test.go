package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestSafeNavFieldAccess(t *testing.T) {
	src := `
record User {
	nome: String
}

fn getNome(user: User?) -> String? {
	return user?.nome
}
`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("safe nav field access não deve gerar erros: %v", result.Errors)
	}
}

func TestSafeNavWithElvis(t *testing.T) {
	src := `
record User {
	nome: String
}

fn label(user: User?) -> String {
	return user?.nome ?: "anon"
}
`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("safe nav + elvis não deve gerar erros: %v", result.Errors)
	}
}

func TestSafeNavRequiresOption(t *testing.T) {
	src := `
record User {
	nome: String
}

fn bad(user: User) -> String? {
	return user?.nome
}
`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("safe nav em tipo não-Option deve gerar erro")
	}
}

func TestSafeNavMethodCall(t *testing.T) {
	src := `
class User {
	val nome: String
	pub fn getNome(self) -> String = self.nome
}

fn call(user: User?) -> String? {
	return user?.getNome()
}
`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("safe nav method call não deve gerar erros: %v", result.Errors)
	}
}
