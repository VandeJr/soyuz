package parser

import "testing"

func TestNamedCallArgs(t *testing.T) {
	prog := parseSource(t, `
enum Forma {
    Circulo(raio: Float)
    Retangulo(w: Float, h: Float)
}
fn main() {
    val c = Forma.Circulo(raio: 5.0)
    val r = Forma.Retangulo(h: 4.0, w: 10.0)
}`)
	fn := prog.Body[1].(*FuncDecl)
	block := fn.Body.(*BlockStmt)
	decl0 := block.Statements[0].(*VarDecl)
	call0 := decl0.Init.(*CallExpr)
	if len(call0.Args) != 1 {
		t.Fatalf("esperado 1 arg, obtido %d", len(call0.Args))
	}
	na, ok := call0.Args[0].(*NamedArg)
	if !ok {
		t.Fatalf("esperado *NamedArg, obtido %T", call0.Args[0])
	}
	if na.Name != "raio" {
		t.Errorf("esperado raio, obtido %s", na.Name)
	}
	decl1 := block.Statements[1].(*VarDecl)
	call1 := decl1.Init.(*CallExpr)
	if len(call1.Args) != 2 {
		t.Fatalf("esperado 2 args nomeados")
	}
	if _, ok := call1.Args[0].(*NamedArg); !ok {
		t.Error("esperado NamedArg em Retangulo")
	}
}
