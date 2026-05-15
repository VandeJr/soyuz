package checker

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
	"testing"
)

// ─── HOF type inference (M4a) ───────────────────────────────────────────────

func TestHOFInferenceConcreteParam(t *testing.T) {
	src := `
	fn aplicar(f: (Int) -> Int, x: Int) -> Int = f(x)
	val r = aplicar(fn(n) => n * 2, 5)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("tipo de parâmetro deve ser inferido de fn(Int)->Int, obtido %v", result.Errors)
	}
}

func TestHOFInferenceAnnotatedParamStillWorks(t *testing.T) {
	src := `
	fn aplicar(f: (Int) -> Int, x: Int) -> Int = f(x)
	val r = aplicar(fn(n: Int) => n * 2, 5)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("lambda com anotação explícita não deve dar erro, obtido %v", result.Errors)
	}
}

func TestHOFInferenceWrongBodyType(t *testing.T) {
	src := `
	fn aplicar(f: (Int) -> Int, x: Int) -> Int = f(x)
	val r = aplicar(fn(n) => "string", 5)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Error("retorno String quando esperado Int deve gerar erro")
	}
}

// ─── Currying com placeholder _ (M4b) ────────────────────────────────────────

func TestCurryBasic(t *testing.T) {
	src := `
	fn multiplicar(a: Int, b: Int) -> Int = a * b
	val dobrar = multiplicar(_, 2)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("curry básico não deve dar erro, obtido %v", result.Errors)
	}

	// Verifica que o tipo inferido de dobrar é fn(Int) -> Int
	var varDecl *parser.VarDecl
	for _, n := range prog.Body {
		if vd, ok := n.(*parser.VarDecl); ok && vd.Name == "dobrar" {
			varDecl = vd
		}
	}
	if varDecl == nil {
		t.Fatal("declaração de dobrar não encontrada")
	}
	ft, ok := result.NodeTypes[varDecl.Init].(*FuncType)
	if !ok {
		t.Fatalf("tipo de dobrar deve ser FuncType, obtido %T", result.NodeTypes[varDecl.Init])
	}
	if len(ft.Params) != 1 {
		t.Errorf("dobrar deve ter 1 parâmetro, obtido %d", len(ft.Params))
	}
	if ft.Params[0] != IntType {
		t.Errorf("parâmetro deve ser Int, obtido %s", ft.Params[0])
	}
	if ft.Return != IntType {
		t.Errorf("retorno deve ser Int, obtido %s", ft.Return)
	}
}

func TestCurryMultiplePlaceholders(t *testing.T) {
	src := `
	fn combinar(a: Int, b: Int, c: Int) -> Int = a + b + c
	val parcial = combinar(_, 10, _)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("curry com múltiplos placeholders não deve dar erro, obtido %v", result.Errors)
	}

	var varDecl *parser.VarDecl
	for _, n := range prog.Body {
		if vd, ok := n.(*parser.VarDecl); ok && vd.Name == "parcial" {
			varDecl = vd
		}
	}
	if varDecl == nil {
		t.Fatal("declaração de parcial não encontrada")
	}
	ft, ok := result.NodeTypes[varDecl.Init].(*FuncType)
	if !ok {
		t.Fatalf("tipo de parcial deve ser FuncType, obtido %T", result.NodeTypes[varDecl.Init])
	}
	if len(ft.Params) != 2 {
		t.Errorf("parcial deve ter 2 parâmetros (os dois _), obtido %d", len(ft.Params))
	}
}

func TestCurryFirstArg(t *testing.T) {
	src := `
	fn somar(a: Int, b: Int) -> Int = a + b
	val soma5 = somar(_, 5)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("curry do primeiro argumento não deve dar erro, obtido %v", result.Errors)
	}
}

func TestCurrySecondArg(t *testing.T) {
	src := `
	fn somar(a: Int, b: Int) -> Int = a + b
	val somarA3 = somar(3, _)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("curry do segundo argumento não deve dar erro, obtido %v", result.Errors)
	}
}

func TestCurriedCallIsStoredInResult(t *testing.T) {
	src := `
	fn multiplicar(a: Int, b: Int) -> Int = a * b
	val dobrar = multiplicar(_, 2)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("esperado sem erros, obtido %v", result.Errors)
	}

	if len(result.CurriedCalls) == 0 {
		t.Error("esperado pelo menos um CurriedCall registrado no result")
	}
}

func TestNormalCallWithUnderscoreInOptionalPositionNotCurried(t *testing.T) {
	// _ em posição opcional deve continuar sendo tratado como None (M3), não curry
	src := `
	fn buscar(id: Int, filtro: String?) -> Int = id
	val r = buscar(1, _)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("_ em posição opcional deve ser None, não curry, obtido %v", result.Errors)
	}
	if len(result.CurriedCalls) > 0 {
		t.Error("_ em posição opcional não deve gerar CurriedCall")
	}
}
