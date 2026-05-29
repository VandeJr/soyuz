package codegen

import (
	"strings"
	"testing"
)

// ── M-25: Task[T].then(fn) + Task[Result[T]].catch(fn) ────────────────────────

func TestTaskThenEmitsThenWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.then(fn(r) => r + 1)
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__then_wrapper_") {
		t.Error("expected __then_wrapper_ function in IR")
	}
}

func TestTaskThenEmitsSrtEnqueue(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.then(fn(r) => r + 1)
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in IR for .then")
	}
}

func TestTaskThenEmitsSrtSetTaskResult(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.then(fn(r) => r + 1)
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("expected srt_set_task_result in IR — then must store transformed result")
	}
}

func TestTaskThenChainedEmitsTwoWrappers(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x + 1 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.then(fn(r) => r * 2).then(fn(r) => r + 10)
  t2.await()
}
`
	ir := compileTask(t, src)
	count := strings.Count(ir, "__then_wrapper_")
	if count < 2 {
		t.Errorf("esperado ao menos 2 __then_wrapper_ para .then encadeado, obtido %d", count)
	}
}

func TestTaskCatchEmitsCatchWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Result[Int] { Ok(x) }

fn run(n: Int) -> Result[Int] {
  val t = task work(n)
  val t2 = t.catch(fn(e) => Ok(0))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__catch_wrapper_") {
		t.Error("expected __catch_wrapper_ function in IR")
	}
}

func TestTaskCatchEmitsTagInspection(t *testing.T) {
	src := `
fn work(x: Int) -> Result[Int] { Ok(x) }

fn run(n: Int) -> Result[Int] {
  val t = task work(n)
  val t2 = t.catch(fn(e) => Ok(0))
  t2.await()
}
`
	ir := compileTask(t, src)
	// The catch wrapper branches on the Result tag.
	if !strings.Contains(ir, "catch_err") || !strings.Contains(ir, "catch_ok") {
		t.Error("expected catch_err and catch_ok blocks in IR — catch must branch on Result tag")
	}
}
