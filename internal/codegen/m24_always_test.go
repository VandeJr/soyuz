package codegen

import (
	"strings"
	"testing"
)

// ── M-24: Task[T].always(fn: Unit -> Unit) -> Task[T] ─────────────────────────

func TestTaskAlwaysEmitsAlwaysWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => print("done"))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__always_wrapper_") {
		t.Error("expected __always_wrapper_ function in IR")
	}
}

func TestTaskAlwaysEmitsSrtEnqueue(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => print("cleanup"))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in IR for .always")
	}
}

func TestTaskAlwaysEmitsSrtAwaitInWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => print("cleanup"))
  t2.await()
}
`
	ir := compileTask(t, src)
	// At least 2: one inside the always wrapper, one for t2.await()
	if strings.Count(ir, "srt_await") < 2 {
		t.Error("expected at least 2 srt_await calls (wrapper + outer)")
	}
}

func TestTaskAlwaysEmitsSrtSetTaskResult(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => print("done"))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("expected srt_set_task_result — always must re-store original result")
	}
}

func TestTaskAlwaysChainingWithTapEmitsBothWrappers(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x + 1 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r)).always(fn() => print("fim"))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__tap_wrapper_") {
		t.Error("expected __tap_wrapper_ in IR for .tap")
	}
	if !strings.Contains(ir, "__always_wrapper_") {
		t.Error("expected __always_wrapper_ in IR for .always")
	}
}
