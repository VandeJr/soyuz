package codegen

import (
	"strings"
	"testing"
)

// ── M-22: Task[T].tap(fn: T -> Unit) -> Task[T] ───────────────────────────────

func TestTaskTapEmitsTapWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__tap_wrapper_") {
		t.Error("expected __tap_wrapper_ function in IR")
	}
}

func TestTaskTapEmitsSrtEnqueue(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in IR for .tap")
	}
}

func TestTaskTapEmitsSrtAwaitInWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r))
  t2.await()
}
`
	ir := compileTask(t, src)
	if strings.Count(ir, "srt_await") < 2 {
		t.Error("expected at least 2 srt_await calls (one in wrapper, one for t2.await)")
	}
}

func TestTaskTapEmitsSrtSetTaskResult(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r))
  t2.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("expected srt_set_task_result in IR — tap must re-store result")
	}
}

func TestTaskTapChainedEmitsTwoWrappers(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x + 1 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r)).tap(fn(r) => print(r))
  t2.await()
}
`
	ir := compileTask(t, src)
	count := strings.Count(ir, "__tap_wrapper_")
	if count < 2 {
		t.Errorf("esperado ao menos 2 __tap_wrapper_ para .tap encadeado, obtido %d", count)
	}
}
