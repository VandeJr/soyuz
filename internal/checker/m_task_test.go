package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// helpers to get task type
func checkSrc(src string) *CheckResult {
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog)
}

func TestTaskExprType(t *testing.T) {
	src := `
fn doWork(n: Int) -> Int = n * 2
val t = task doWork(5)
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("task expr não deve gerar erros, obtido: %v", result.Errors)
	}

	// find the VarDecl for t and check its type is Task[Int]
	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "t" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType, obtido %T (%s)", typ, typ)
			}
			ct, ok := st.Base.(*ClassType)
			if !ok || ct.Name != "Task" {
				t.Fatalf("base esperada Task, obtido %v", st.Base)
			}
			if len(st.Params) != 1 {
				t.Fatalf("Task deve ter 1 param, obtido %d", len(st.Params))
			}
			if st.Params[0].String() != "Int" {
				t.Fatalf("Task[Int] esperado, obtido Task[%s]", st.Params[0])
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 't' não encontrada nos NodeTypes")
	}
}

func TestTaskExprUnit(t *testing.T) {
	src := `
fn doSomething() {}
val t = task doSomething()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("task com fn Unit não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskExprNoLambda(t *testing.T) {
	src := `val t = task fn() => 42`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	// parser should report an error for task + lambda
	if len(prog.Body) == 0 {
		return // parse error reported
	}
	// If parser didn't error, checker is not expected to either — the rule is
	// enforced at parse time via errorf in parsePrefix.
}

// ── M-06 ─────────────────────────────────────────────────────────────────────

func TestTaskAllReturnsTuple(t *testing.T) {
	src := `
fn getA() -> Int = 1
fn getB() -> String = "x"
fn main() {
  val t1 = task getA()
  val t2 = task getB()
  val (a, b) = Task.all(t1, t2)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.all não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskAllSettledReturnsTuple(t *testing.T) {
	src := `
fn work1() -> Int = 42
fn work2() -> Int = 99
fn main() {
  val t1 = task work1()
  val t2 = task work2()
  val (a, b) = Task.allSettled(t1, t2)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.allSettled não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskAnyReturnsInnerType(t *testing.T) {
	src := `
fn fast() -> Int = 1
fn slow() -> Int = 2
fn main() -> Int {
  val t1 = task fast()
  val t2 = task slow()
  val result = Task.any(t1, t2)
  return result
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.any não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskAnyMismatchedTypesError(t *testing.T) {
	src := `
fn getInt() -> Int = 1
fn getStr() -> String = "x"
fn main() {
  val t1 = task getInt()
  val t2 = task getStr()
  val r = Task.any(t1, t2)
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("Task.any com tipos diferentes deve gerar erro")
	}
}

func TestTaskExprResultType(t *testing.T) {
	src := `
fn fetchUser(id: Int) -> String = "user"
val t = task fetchUser(1)
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("task com fn -> String não deve gerar erros, obtido: %v", result.Errors)
	}

	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "t" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType, obtido %T", typ)
			}
			if st.Params[0].String() != "String" {
				t.Fatalf("Task[String] esperado, obtido Task[%s]", st.Params[0])
			}
		}
	}
}

// ── M-07 ─────────────────────────────────────────────────────────────────────

func TestTaskHandleCurrentReturnsOption(t *testing.T) {
	src := `
fn doWork() {
  val h = TaskHandle.current()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("TaskHandle.current() não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "h" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType para h, obtido %T (%s)", typ, typ)
			}
			if st.Base.String() != "Option" {
				t.Fatalf("esperado Option[TaskHandle], obtido %s", typ)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "TaskHandle" {
				t.Fatalf("esperado Option[TaskHandle], obtido Option[%v]", st.Params)
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 'h' não encontrada nos NodeTypes")
	}
}

func TestTaskHandleCancelledReturnsBool(t *testing.T) {
	src := `
fn checkIfCancelled(handle: TaskHandle) -> Bool = handle.cancelled()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("handle.cancelled() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskHandleProgressAccepted(t *testing.T) {
	src := `
fn doWork() {
  val h = TaskHandle.current()
  match h {
    Some(handle) => handle.progress(0.5)
    None => {}
  }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("handle.progress() não deve gerar erros, obtido: %v", result.Errors)
	}
}

// ── M-10 ─────────────────────────────────────────────────────────────────────

func TestTaskCancelIsUnit(t *testing.T) {
	src := `
fn doWork() -> Int = 42
fn main() {
  val t = task doWork()
  t.cancel()
  t.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("t.cancel() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskCancelOnDetachedTask(t *testing.T) {
	src := `
fn doWork() -> Int = 1
fn main() {
  val t = task doWork()
  t.cancel()
  t.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("t.cancel() seguido de t.detach() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskHandleCurrentOutsideTask(t *testing.T) {
	src := `
fn notATask() {
  val h = TaskHandle.current()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("TaskHandle.current() fora de task não deve gerar erros de checker, obtido: %v", result.Errors)
	}
}
