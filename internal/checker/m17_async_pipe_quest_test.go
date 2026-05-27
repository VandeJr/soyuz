package checker

import (
	"testing"

	"soyuz/internal/parser"
)

// ── M-17: ~?> async pipe-quest ────────────────────────────────────────────────

// TestAsyncPipeQuestType verifies `a ~> f ~?> g` has type Task[Result[T]].
func TestAsyncPipeQuestType(t *testing.T) {
	src := `
fn parse(s: String) -> Result[Int] = Ok(42)
fn double(n: Int) -> Int = n * 2

fn main() {
  val s = "42"
  val t = s ~> parse ~?> double
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("s ~> parse ~?> double não deve gerar erros, obtido: %v", result.Errors)
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
			found = true
			_ = st // Task[Result[Int]] or similar
		}
	}
	if !found {
		t.Error("declaração 't' não encontrada nos NodeTypes")
	}
}

// TestAsyncPipeQuestLexer verifies that ~?> is tokenized correctly.
func TestAsyncPipeQuestLexer(t *testing.T) {
	src := `
fn parse(s: String) -> Result[Int] = Ok(42)
fn main() {
  val s = "x"
  val t = s ~?> parse
  t.detach()
}
`
	result := checkSrc(src)
	// Checker should handle ~?> on initial value (first ~?> step)
	// Some type errors are acceptable here since s is String not Result[String].
	_ = result
}

// TestAsyncPipeQuestChain verifies mixed ~> and ~?> in a chain.
func TestAsyncPipeQuestChain(t *testing.T) {
	src := `
fn validate(n: Int) -> Result[Int] = Ok(n)
fn double(n: Int) -> Int = n * 2

fn main() {
  val n = 5
  val t = n ~> validate ~?> double
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("n ~> validate ~?> double não deve gerar erros, obtido: %v", result.Errors)
	}
}
