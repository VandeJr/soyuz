package checker

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
	"testing"
)

func TestCheckVarDecl(t *testing.T) {
	src := `val x: Int = "não sou um int"`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro de tipo, mas nenhum foi encontrado")
	}

	expected := "incompatible type: expected Int, got String"
	if result.Errors[0].Message != expected {
		t.Errorf("esperado %q, obtido %q", expected, result.Errors[0].Message)
	}
}

func TestCheckValidVarDecl(t *testing.T) {
	src := `val x: Int = 42`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros, obtido %v", result.Errors)
	}
}

func TestCheckBinaryExpr(t *testing.T) {
	src := `val x = 1 + "erro"`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro de tipo em expressão binária")
	}
}

func TestCheckMutability(t *testing.T) {
	src := `
	val x = 10
	x = 20
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro ao tentar alterar variável val")
	}
	expected := "cannot assign to immutable variable (val): x"
	if result.Errors[0].Message != expected {
		t.Errorf("esperado %q, obtido %q", expected, result.Errors[0].Message)
	}
}

func TestCheckFuncDecl(t *testing.T) {
	src := `
	fn soma(a: Int, b: Int) -> Int {
		return a + b
	}
	val r = soma(1, 2)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros, obtido %v", result.Errors)
	}
}

func TestCheckFuncError(t *testing.T) {
	src := `
	fn soma(a: Int, b: Int) -> Int {
		return "não sou um int"
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro de retorno na função")
	}
}

func TestCheckRecordLiteral(t *testing.T) {
	src := `
	record Ponto { x: Int, y: Int }
	val p = Ponto { x: 1, y: 2 }
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros em record literal válido, obtido %v", result.Errors)
	}
}

func TestCheckRecordLiteralError(t *testing.T) {
	src := `
	record Ponto { x: Int, y: Int }
	val p = Ponto { x: 1 }
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro por campo faltante no record")
	}
}

func TestCheckMatchExpr(t *testing.T) {
	src := `
	enum Res { Sim, Nao }
	val msg = match Sim() {
		Sim => "yes"
		Nao => "no"
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros em match válido, obtido %v", result.Errors)
	}
}

func TestCheckMatchPatternError(t *testing.T) {
	src := `
	val x = 10
	val msg = match x {
		"texto" => "erro"
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Fatal("esperado erro por padrão literal incompatível (Int vs String)")
	}
}

func TestCheckInterfaceAndClass(t *testing.T) {
	src := `
	interface Saudavel {
		fn saudar(self) -> String
	}
	class Pessoa : Saudavel {
		val nome: String
		fn saudar(self) -> String = "olá"
	}
	val s: Saudavel = Pessoa { nome: "Vand" }
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros em interface/class válidos, obtido %v", result.Errors)
	}
}

func TestCheckGenerics(t *testing.T) {
	src := `
	record Par[A, B] { primeiro: A, segundo: B }
	fn identidade[T](x: T) -> T = x
	val p: Par[Int, String] = Par { primeiro: 1, segundo: "dois" }
	val r = identidade[Int](10)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros em genéricos válidos, obtido %v", result.Errors)
	}
}

func TestCheckOptionBuiltin(t *testing.T) {
	src := `
	val x: Int? = Some(42)
	val y: String? = None
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("não esperado erros em Option válido, obtido %v", result.Errors)
	}
}

func TestCheckResultBuiltin(t *testing.T) {
	src := `
	class MyError : Error {
		fn message(self) -> String = "failed"
		fn code(self) -> Int = 500
	}
	val r: Result[Int] = Ok(10)
	val e: Result[Int] = Err(MyError {})
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors in valid Result, got %v", result.Errors)
	}
}

func TestCheckGenericInference(t *testing.T) {
	src := `
	fn identity[T](x: T) -> T = x
	val r1 = identity(10)      // Inferred Int
	val r2 = identity("soyuz") // Inferred String
	val r3: Int = identity(20)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("expected no errors in valid generic inference, got %v", result.Errors)
	}
}

func TestNodeTypesPopulated(t *testing.T) {
	src := "val x: Int = 42"
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("não esperado erros, obtido %v", result.Errors)
	}

	if len(result.NodeTypes) == 0 {
		t.Error("NodeTypes map should not be empty")
	}

	// Check if the IntLiteral 42 has type Int
	found := false
	for node, typ := range result.NodeTypes {
		if lit, ok := node.(*parser.IntLiteral); ok && lit.Value == "42" {
			if typ.String() != "Int" {
				t.Errorf("expected type Int for literal 42, got %s", typ)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("IntLiteral 42 not found in NodeTypes")
	}
}
