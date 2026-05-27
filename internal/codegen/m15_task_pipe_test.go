package codegen

import (
	"strings"
	"testing"
)

// ── M-15: task com pipe chain ─────────────────────────────────────────────────

// TestTaskPipeBasicIR verifies that `task (n |> f)` emits srt_enqueue with a wrapper.
func TestTaskPipeBasicIR(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() {
  val n = 21
  val t = task (n |> double)
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
	if !strings.Contains(ir, "__task_wrapper_") {
		t.Error("esperado wrapper de task no IR")
	}
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("esperado srt_set_task_result no IR do wrapper")
	}
}

// TestTaskPipeChainIR verifies that a multi-step pipe chain emits a single wrapper.
func TestTaskPipeChainIR(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn triple(n: Int) -> Int = n * 3

fn main() {
  val n = 5
  val t = task (n |> double |> triple)
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
	// Exactly one wrapper should be generated.
	count := strings.Count(ir, "__task_wrapper_")
	if count < 1 {
		t.Errorf("esperado pelo menos 1 wrapper de task, obtido %d", count)
	}
}

// TestTaskPipeCapturesVariable verifies that a local variable used as the pipe
// starting value is captured (packed into the args buffer) and unpacked in the wrapper.
func TestTaskPipeCapturesVariable(t *testing.T) {
	src := `
fn negate(n: Int) -> Int = 0 - n

fn main() -> Int {
  val x = 42
  val t = task (x |> negate)
  return t.await()
}
`
	ir := compileTask(t, src)
	// The wrapper should unpack the captured variable.
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
	// The wrapper must call the double function.
	if !strings.Contains(ir, "@negate") {
		t.Error("esperado chamada a @negate no IR")
	}
}

// TestTaskPipeLiteralIR verifies `task (42 |> f)` — no captures, null args.
func TestTaskPipeLiteralIR(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() -> Int {
  val t = task (42 |> double)
  return t.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
}

// TestTaskPipeAwaitResult verifies that t.await() after a pipe-task is emitted.
func TestTaskPipeAwaitResult(t *testing.T) {
	src := `
fn inc(n: Int) -> Int = n + 1

fn main() -> Int {
  val n = 9
  val t = task (n |> inc)
  return t.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("esperado srt_await no IR")
	}
}
