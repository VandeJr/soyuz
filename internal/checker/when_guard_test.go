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
