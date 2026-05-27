package codegen

import (
	"strings"
	"testing"
)

// ── M-16: ~> async pipe ───────────────────────────────────────────────────────

// TestAsyncPipeEmitsEnqueueAndAwait verifies `a ~> f ~> g` emits:
// srt_enqueue for each step and srt_await for intermediate steps.
func TestAsyncPipeEmitsEnqueueAndAwait(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn triple(n: Int) -> Int = n * 3

fn main() {
  val n = 5
  val t = n ~> double ~> triple
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
	// Intermediate steps must be awaited.
	if !strings.Contains(ir, "srt_await") {
		t.Error("esperado srt_await no IR para step intermediário")
	}
	// Task wrappers must be generated.
	if !strings.Contains(ir, "__task_wrapper_") {
		t.Error("esperado __task_wrapper_ no IR")
	}
}

// TestAsyncPipeSingleStepNoAwait verifies `a ~> f` (single step) emits only srt_enqueue,
// no srt_await — the result is a live Task[T] handle.
func TestAsyncPipeSingleStepNoAwait(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() -> Int {
  val n = 21
  val t = n ~> double
  return t.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
}

// TestAsyncPipeMultiWrapper verifies that a 3-step chain produces 3 task wrappers
// (one per step, including the last).
func TestAsyncPipeMultiWrapper(t *testing.T) {
	src := `
fn a(n: Int) -> Int = n + 1
fn b(n: Int) -> Int = n + 2
fn c(n: Int) -> Int = n + 3

fn main() {
  val n = 0
  val t = n ~> a ~> b ~> c
  t.detach()
}
`
	ir := compileTask(t, src)
	count := strings.Count(ir, "__task_wrapper_")
	// 3 wrappers for 3 steps.
	if count < 3 {
		t.Errorf("esperado 3 wrappers para 3 steps, obtido %d\nIR:\n%s", count, ir)
	}
}

// TestAsyncPipeSetsTaskResult verifies the wrapper calls srt_set_task_result.
func TestAsyncPipeSetsTaskResult(t *testing.T) {
	src := `
fn inc(n: Int) -> Int = n + 1

fn main() -> Int {
  val n = 41
  val t = n ~> inc
  return t.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("esperado srt_set_task_result no IR do wrapper")
	}
}
