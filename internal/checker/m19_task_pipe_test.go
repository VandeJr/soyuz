package checker

import (
	"testing"

	"soyuz/internal/parser"
)

// ── M-19: Task.pipe — pipeline paralelo com channels ────────────────────────

// TestTaskPipeBasicValueInput verifies Task.pipe(n, f, g) returns Channel[R].
func TestTaskPipeBasicValueInput(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn negate(n: Int) -> Int = n * -1

fn main() {
  val n = 5
  val ch = Task.pipe(n, double, negate)
  ch.close()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.pipe não deve gerar erros, obtido: %v", result.Errors)
	}

	// Verify ch is Channel[Int]
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "ch" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("ch deve ser SpecializedType, obtido %T (%s)", typ, typ)
			}
			ct, ok := st.Base.(*ClassType)
			if !ok || ct.Name != "Channel" {
				t.Fatalf("base esperada Channel, obtido %v", st.Base)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "Int" {
				t.Fatalf("Channel[Int] esperado, obtido Channel[%v]", st.Params)
			}
			return
		}
	}
	t.Error("declaração 'ch' não encontrada nos NodeTypes")
}

// TestTaskPipeChannelInput verifies Task.pipe(inCh, f, g) accepts Channel[T] as input.
func TestTaskPipeChannelInput(t *testing.T) {
	src := `
fn process(n: Int) -> Int = n + 1
fn stringify(n: Int) -> String = "x"

fn main() {
  val inCh = Channel.new(4)
  val outCh = Task.pipe(inCh, process, stringify)
  outCh.close()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.pipe com Channel como input não deve gerar erros, obtido: %v", result.Errors)
	}

	// Verify outCh is Channel[String]
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "outCh" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("outCh deve ser SpecializedType, obtido %T (%s)", typ, typ)
			}
			ct, ok := st.Base.(*ClassType)
			if !ok || ct.Name != "Channel" {
				t.Fatalf("base esperada Channel, obtido %v", st.Base)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "String" {
				t.Fatalf("Channel[String] esperado, obtido Channel[%v]", st.Params)
			}
			return
		}
	}
	t.Error("declaração 'outCh' não encontrada nos NodeTypes")
}

// TestTaskPipeSingleStage verifies Task.pipe(n, f) with one stage returns Channel[R].
func TestTaskPipeSingleStage(t *testing.T) {
	src := `
fn square(n: Int) -> Int = n * n

fn main() {
  val n = 4
  val ch = Task.pipe(n, square)
  ch.close()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.pipe com 1 stage não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskPipeTooFewArgs verifies an error when fewer than 2 args are provided.
func TestTaskPipeTooFewArgs(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() {
  val ch = Task.pipe(double)
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("Task.pipe com menos de 2 args deve gerar erro")
	}
}

// TestTaskPipeThreeStages verifies a three-stage pipeline chains types correctly.
func TestTaskPipeThreeStages(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn stringify(n: Int) -> String = "x"
fn exclaim(s: String) -> String = s

fn main() {
  val n = 3
  val ch = Task.pipe(n, double, stringify, exclaim)
  ch.close()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.pipe com 3 stages não deve gerar erros, obtido: %v", result.Errors)
	}

	// Verify ch is Channel[String]
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "ch" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("ch deve ser SpecializedType, obtido %T (%s)", typ, typ)
			}
			ct, ok := st.Base.(*ClassType)
			if !ok || ct.Name != "Channel" {
				t.Fatalf("base esperada Channel, obtido %v", st.Base)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "String" {
				t.Fatalf("Channel[String] esperado, obtido Channel[%v]", st.Params)
			}
			return
		}
	}
	t.Error("declaração 'ch' não encontrada nos NodeTypes")
}
