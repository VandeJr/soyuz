package checker

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
	"testing"
)

func TestWhenGuardExhaustiveness(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{
			name: "missing catchall with when",
			src: `
			fn classificar(x: Int) when x < 0 = "negativo"
			fn classificar(x: Int) when x == 0 = "zero"
			`,
			wantErr: true,
		},
		{
			name: "valid catchall with when",
			src: `
			fn classificar(x: Int) when x < 0 = "negativo"
			fn classificar(x: Int) when x == 0 = "zero"
			fn classificar(x: Int) = "positivo"
			`,
			wantErr: false,
		},
		{
			name: "missing catchall with pattern matching",
			src: `
			fn f(0) = 0
			fn f(1) = 1
			`,
			wantErr: true,
		},
		{
			name: "valid catchall with pattern matching",
			src: `
			fn f(0) = 0
			fn f(x: Int) = x
			`,
			wantErr: false,
		},
		{
			name: "valid catchall with when and pattern matching",
			src: `
			fn f(0) when true = 0
			fn f(x: Int) = x
			`,
			wantErr: false,
		},
		{
			name: "missing catchall with when and pattern matching",
			src: `
			fn f(0) when true = 0
			fn f(1) = 1
			`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := lexer.Tokenize(tt.src)
			p := parser.New(tokens)
			prog := p.Parse()

			c := New()
			result := c.Check(prog)

			if tt.wantErr && len(result.Errors) == 0 {
				t.Errorf("esperado erro de exaustividade, mas nenhum foi encontrado")
			}
			if !tt.wantErr && len(result.Errors) > 0 {
				t.Errorf("não esperado erros, obtido %v", result.Errors)
			}
		})
	}
}

func TestMatchWhenGuard(t *testing.T) {
	src := `
	val x = 10
	val r = match x {
		n when n < 0 => "negativo"
		_ => "positivo"
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros em match com when, obtido %v", result.Errors)
	}
}

// TestErrInferredFromOkVariant verifica que Err(e) em uma variante catchall recebe
// o tipo Result[T] inferido da variante Ok da mesma função (não Result[Unknown]).
func TestErrInferredFromOkVariant(t *testing.T) {
	src := `
	fn validar(n: Int) when n > 0 = Ok(n)
	fn validar(n: Int) = Err("não positivo")
	`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("não esperado erros, obtido %v", result.Errors)
	}

	// O tipo da função deve ser Result[Int], não Result[Unknown].
	for node, ty := range result.NodeTypes {
		fd, ok := node.(*parser.FuncDecl)
		if !ok || fd.Name != "validar" {
			continue
		}
		ft, ok := ty.(*FuncType)
		if !ok {
			t.Fatalf("tipo de validar não é FuncType: %T", ty)
		}
		got := ft.Return.String()
		if got != "Result[Int]" {
			t.Errorf("esperado retorno Result[Int], obtido %s", got)
		}
	}
}

// TestNoneInferredFromSomeVariant verifica que None em variante catchall recebe
// o tipo Option[T] inferido da variante Some da mesma função.
func TestNoneInferredFromSomeVariant(t *testing.T) {
	src := `
	fn buscar(n: Int) when n > 0 = Some(n)
	fn buscar(n: Int) = None
	`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("não esperado erros, obtido %v", result.Errors)
	}

	for node, ty := range result.NodeTypes {
		fd, ok := node.(*parser.FuncDecl)
		if !ok || fd.Name != "buscar" {
			continue
		}
		ft, ok := ty.(*FuncType)
		if !ok {
			t.Fatalf("tipo de buscar não é FuncType: %T", ty)
		}
		got := ft.Return.String()
		if got != "Option[Int]" {
			t.Errorf("esperado retorno Option[Int], obtido %s", got)
		}
	}
}

// TestErrWithExplicitAnnotation verifica que Err(e) em função com anotação explícita
// Result[Int] recebe o tipo correto.
func TestErrWithExplicitAnnotation(t *testing.T) {
	src := `
	fn f(n: Int) -> Result[Int] = Err("erro")
	`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("não esperado erros, obtido %v", result.Errors)
	}

	for node, ty := range result.NodeTypes {
		fd, ok := node.(*parser.FuncDecl)
		if !ok || fd.Name != "f" {
			continue
		}
		ft, ok := ty.(*FuncType)
		if !ok {
			t.Fatalf("tipo de f não é FuncType: %T", ty)
		}
		got := ft.Return.String()
		if got != "Result[Int]" {
			t.Errorf("esperado retorno Result[Int], obtido %s", got)
		}
	}
}

func TestWhenGuardType(t *testing.T) {
	src := `
	fn f(x: Int) when "não é um bool" = x
	fn f(x: Int) = x
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro de tipo no when guard (String vs Bool)")
	}
}
