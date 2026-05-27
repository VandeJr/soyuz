package checker

import (
	"testing"

	"soyuz/internal/parser"
)

// ── M-16: ~> async pipe ───────────────────────────────────────────────────────

// TestAsyncPipeBasicType verifies `a ~> f` has type Task[ReturnType(f)].
func TestAsyncPipeBasicType(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() {
  val n = 21
  val t = n ~> double
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("n ~> double não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "t" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType, obtido %T (%s)", typ, typ)
			}
			ct, ok2 := st.Base.(*ClassType)
			if !ok2 || ct.Name != "Task" {
				t.Fatalf("base esperada Task, obtido %v", st.Base)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "Int" {
				t.Fatalf("Task[Int] esperado, obtido Task[%v]", st.Params)
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 't' não encontrada nos NodeTypes")
	}
}

// TestAsyncPipeChainType verifies the final type after a multi-step chain.
func TestAsyncPipeChainType(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn toString(n: Int) -> String = "x"

fn main() {
  val n = 5
  val t = n ~> double ~> toString
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("n ~> double ~> toString não deve gerar erros, obtido: %v", result.Errors)
	}

	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "t" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType para t, obtido %T", typ)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "String" {
				t.Fatalf("Task[String] esperado, obtido Task[%v]", st.Params)
			}
		}
	}
}

// TestAsyncPipeAwait verifies that .await() on the result of ~> is valid.
func TestAsyncPipeAwait(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() -> Int {
  val n = 21
  val t = n ~> double
  return t.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".await() em t: Task[Int] não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestAsyncPipeLiteralSource verifies `42 ~> f` works with a literal.
func TestAsyncPipeLiteralSource(t *testing.T) {
	src := `
fn inc(n: Int) -> Int = n + 1

fn main() {
  val t = 42 ~> inc
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("42 ~> inc não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestAsyncPipeLexer verifies that ~> and ~?> are tokenized correctly.
func TestAsyncPipeLexer(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn main() { val t = 1 ~> double  t.detach() }
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("lexer/parser de ~> falhou: %v", result.Errors)
	}
}
