package checker

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
	"testing"
)

func TestOptionSugarShortCall(t *testing.T) {
	src := `
	fn buscar(id: Int, filtro: String?) = id
	val r = buscar(1)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("chamada com param opcional ausente não deve dar erro, obtido %v", result.Errors)
	}
}

func TestOptionSugarAutoWrap(t *testing.T) {
	src := `
	fn buscar(id: Int, filtro: String?) = id
	val r = buscar(1, "ativo")
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("valor passado para param opcional deve ser auto-wrapped, obtido %v", result.Errors)
	}
}

func TestOptionSugarExplicitNone(t *testing.T) {
	src := `
	fn buscar(id: Int, filtro: String?) = id
	val r = buscar(1, None)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("None explícito para param opcional não deve dar erro, obtido %v", result.Errors)
	}
}

func TestOptionSugarExplicitSome(t *testing.T) {
	src := `
	fn buscar(id: Int, filtro: String?) = id
	val r = buscar(1, Some("ativo"))
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("Some() explícito para param opcional não deve dar erro, obtido %v", result.Errors)
	}
}

func TestOptionSugarUnderscorePlaceholder(t *testing.T) {
	src := `
	fn exemplo(x: Int?, y: Int?) = 0
	val r = exemplo(_, 10)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("_ em posição opcional deve ser None, obtido %v", result.Errors)
	}
}

func TestOptionSugarMultipleOptional(t *testing.T) {
	src := `
	fn exemplo(x: Int?, y: Int?) = 0
	val r1 = exemplo()
	val r2 = exemplo(1)
	val r3 = exemplo(1, 2)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("múltiplos params opcionais em variações de chamada, obtido %v", result.Errors)
	}
}

func TestOptionSugarSynthCallArgs(t *testing.T) {
	src := `
	fn buscar(id: Int, filtro: String?) = id
	val r = buscar(1)
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Fatalf("esperado sem erros, obtido %v", result.Errors)
	}

	// Verify that the call site has a synthetic None injected
	var callNode *parser.CallExpr
	for _, stmt := range prog.Body {
		if vd, ok := stmt.(*parser.VarDecl); ok {
			if ce, ok := vd.Init.(*parser.CallExpr); ok {
				callNode = ce
			}
		}
	}
	if callNode == nil {
		t.Fatal("nó CallExpr não encontrado")
	}
	if synth, ok := result.SynthCallArgs[callNode]; !ok || len(synth) == 0 {
		t.Error("esperado SynthCallArgs com NoneLiteral para param opcional ausente")
	}
}

func TestSomePatternBindingHasInnerType(t *testing.T) {
	src := `
	fn add(x: Int?, y: Int) -> Int {
		return match x {
			Some(v) => v + y
			None => y
		}
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("Some(v) em match de Int? não deve gerar erros, obtido: %v", result.Errors)
	}

	// Verify that the binding `v` has type Int, not Unknown.
	var foundType Type
	for node, typ := range result.NodeTypes {
		if id, ok := node.(*parser.Identifier); ok && id.Name == "v" {
			foundType = typ
		}
	}
	if foundType == nil {
		t.Fatal("identificador 'v' não encontrado em NodeTypes")
	}
	if foundType != IntType {
		t.Errorf("esperado tipo Int para v, obtido %s", foundType)
	}
}

func TestElvisCheckerReturnsInnerType(t *testing.T) {
	src := `
	fn add(x: Int?, y: Int) -> Int {
		val v: Int = x ?: 10
		return v + y
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("elvis em Int? não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestOptionSugarRequiredParamStillRequired(t *testing.T) {
	src := `
	fn buscar(id: Int, filtro: String?) = id
	val r = buscar()
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := New()
	result := c.Check(prog)

	if len(result.Errors) == 0 {
		t.Error("param não-opcional ausente deve gerar erro")
	}
}
