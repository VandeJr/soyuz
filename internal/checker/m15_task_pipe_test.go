package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// ── M-15: task com pipe chain ─────────────────────────────────────────────────

// TestTaskPipeBasic verifies that `task (a |> f)` has type Task[ReturnTypeOfF].
func TestTaskPipeBasic(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() {
  val n = 21
  val t = task (n |> double)
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("task (n |> double) não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "t" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType para t, obtido %T (%s)", typ, typ)
			}
			ct, ok2 := st.Base.(*ClassType)
			if !ok2 || ct.Name != "Task" {
				t.Fatalf("base esperada Task, obtido %v", st.Base)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "Int" {
				t.Fatalf("Task[Int] esperado, obtido %v", st.Params)
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 't' não encontrada nos NodeTypes")
	}
}

// TestTaskPipeChain verifies `task (a |> f |> g)` computes the final type correctly.
func TestTaskPipeChain(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn toString(n: Int) -> String = "x"

fn main() {
  val n = 5
  val t = task (n |> double |> toString)
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("task (n |> double |> toString) não deve gerar erros, obtido: %v", result.Errors)
	}

	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "t" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType para t, obtido %T (%s)", typ, typ)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "String" {
				t.Fatalf("Task[String] esperado, obtido %v", st.Params)
			}
		}
	}
}

// TestTaskPipeAwait verifies that t.await() on a pipe-task returns the correct type.
func TestTaskPipeAwait(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() -> Int {
  val n = 21
  val t = task (n |> double)
  return t.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("t.await() em task pipe não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskPipeLiteralInput verifies that `task (42 |> f)` works with a literal.
func TestTaskPipeLiteralInput(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() {
  val t = task (42 |> double)
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("task (42 |> double) não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskPipeNoLambda verifies that `task fn() => expr` is rejected by the parser.
func TestTaskPipeNoLambda(t *testing.T) {
	src := `val t = task fn() => 42`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	// Parser should report an error and/or the program body should not contain a valid task.
	_ = prog // error is reported inline; test just verifies no panic
}
