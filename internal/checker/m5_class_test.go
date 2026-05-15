package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// TestM5FieldDefaults verifica que campos com Init são opcionais na inicialização.
func TestM5FieldDefaults(t *testing.T) {
	src := `
class Contador {
	var count: Int = 0
	var label: String = "cnt"
	pub fn incrementar(self) { }
}
val c1 = Contador {}
val c2 = Contador { label: "x" }
val c3 = Contador { count: 10 }
val c4 = Contador { count: 5, label: "y" }
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("campos com default não devem gerar erros, obtido: %v", result.Errors)
	}
}

// TestM5FieldDefaultsRequired verifica que campos sem default ainda são obrigatórios.
func TestM5FieldDefaultsRequired(t *testing.T) {
	src := `
class Ponto {
	val x: Int
	val y: Int = 0
}
val p = Ponto {}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)

	if len(result.Errors) == 0 {
		t.Error("campo 'x' sem default deve gerar erro quando ausente")
	}
}

// TestM5MethodOverloading verifica sobrecarga de métodos por aridade.
func TestM5MethodOverloading(t *testing.T) {
	src := `
class Contador {
	var count: Int = 0
	pub fn incrementar(self) { }
	pub fn incrementar(self, n: Int) { }
	pub fn reset(self) { }
}
val c = Contador {}
c.incrementar()
c.incrementar(5)
c.reset()
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("sobrecarga por aridade não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestM5MethodOverloadingWrongArity verifica erro com aridade inválida em sobrecarga.
func TestM5MethodOverloadingWrongArity(t *testing.T) {
	src := `
class Calc {
	pub fn somar(self, a: Int) -> Int = a
	pub fn somar(self, a: Int, b: Int) -> Int = a
}
val c = Calc {}
c.somar(1, 2, 3)
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)

	if len(result.Errors) == 0 {
		t.Error("3 argumentos onde existem variantes de 1 e 2 deve gerar erro")
	}
}

// TestM5PubVisibility verifica que métodos privados não são acessíveis externamente.
func TestM5PubVisibility(t *testing.T) {
	src := `
class Caixa {
	var valor: Int = 0
	pub fn obter(self) -> Int = 0
	fn resetar(self) { }
}
val c = Caixa {}
c.obter()
c.resetar()
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)

	hasPrivateError := false
	for _, e := range result.Errors {
		if containsStr(e.Message, "privado") {
			hasPrivateError = true
			break
		}
	}
	if !hasPrivateError {
		t.Errorf("acesso a método privado deve gerar erro de visibilidade, obtido: %v", result.Errors)
	}
}

// TestM5PubVisibilityInside verifica que métodos privados são acessíveis dentro da classe.
func TestM5PubVisibilityInside(t *testing.T) {
	src := `
class Motor {
	var ligado: Bool = false
	fn ligar(self) { }
	pub fn iniciar(self) { self.ligar() }
}
val m = Motor {}
m.iniciar()
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	result := New().Check(prog)

	for _, e := range result.Errors {
		if containsStr(e.Message, "privado") {
			t.Errorf("método privado deve ser acessível dentro da própria classe: %s", e.Message)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
