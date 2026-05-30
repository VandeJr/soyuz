package parser

import "testing"

func TestExprGenericRecordLiteral(t *testing.T) {
	prog := parseSource(t, `
record Par[T, U] { primeiro: T, segundo: U }
fn main() {
    val coords = Par[Int, Int] { primeiro: 10, segundo: 20 }
}`)
	fn := prog.Body[1].(*FuncDecl)
	block := fn.Body.(*BlockStmt)
	decl := block.Statements[0].(*VarDecl)
	rl, ok := decl.Init.(*RecordLiteral)
	if !ok {
		t.Fatalf("esperado *RecordLiteral, obtido %T", decl.Init)
	}
	if len(rl.TypeArgs) != 2 {
		t.Fatalf("esperado 2 TypeArgs, obtido %d", len(rl.TypeArgs))
	}
}

func TestExprGenericFuncCall(t *testing.T) {
	prog := parseSource(t, `
fn identidade[T](x: T) -> T = x
fn main() {
    val z = identidade[Bool](true)
}`)
	fn := prog.Body[1].(*FuncDecl)
	block := fn.Body.(*BlockStmt)
	decl := block.Statements[0].(*VarDecl)
	call, ok := decl.Init.(*CallExpr)
	if !ok {
		t.Fatalf("esperado *CallExpr, obtido %T", decl.Init)
	}
	se, ok := call.Callee.(*SpecializedExpr)
	if !ok {
		t.Fatalf("esperado *SpecializedExpr callee, obtido %T", call.Callee)
	}
	if len(se.TypeArgs) != 1 {
		t.Errorf("esperado 1 type arg")
	}
}
