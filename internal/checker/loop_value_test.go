package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestLoopBreakValueType(t *testing.T) {
	src := `
fn f() -> Int {
    var t = 0
    val r = loop {
        t = t + 1
        if t == 5 { break t * 10 }
    }
    return r
}`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("erros inesperados: %v", res.Errors)
	}
	var loopNode *parser.LoopStmt
	var fn *parser.FuncDecl
	for _, n := range prog.Body {
		if fd, ok := n.(*parser.FuncDecl); ok && fd.Name == "f" {
			fn = fd
			break
		}
	}
	if fn == nil {
		t.Fatal("função f não encontrada")
	}
	block := fn.Body.(*parser.BlockStmt)
	for _, s := range block.Statements {
		if vd, ok := s.(*parser.VarDecl); ok && vd.Name == "r" {
			loopNode = vd.Init.(*parser.LoopStmt)
			break
		}
	}
	if loopNode == nil {
		t.Fatal("loop não encontrado")
	}
	if res.NodeTypes[loopNode].String() != "Int" {
		t.Fatalf("esperado tipo Int no loop, obtido %s", res.NodeTypes[loopNode])
	}
}

func TestBreakOutsideLoop(t *testing.T) {
	src := `fn f() { break 1 }`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := New().Check(prog)
	if len(res.Errors) == 0 {
		t.Fatal("esperado erro de break fora de loop")
	}
}
