package codegen

import (
	"strings"
	"testing"
)

// ── M-18: Task.fan — fan-out paralelo ────────────────────────────────────────

// TestTaskFanEmitsMultipleEnqueueAndWrappers verifies Task.fan spawns one task per function.
func TestTaskFanEmitsMultipleEnqueueAndWrappers(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn triple(n: Int) -> Int = n * 3

fn main() {
  val n = 5
  val (t1, t2) = Task.fan(n, double, triple)
  t1.detach()
  t2.detach()
}
`
	ir := compileTask(t, src)
	// Two srt_enqueue calls — one per function.
	count := strings.Count(ir, "srt_enqueue")
	if count < 2 {
		t.Errorf("esperado ao menos 2 srt_enqueue, obtido %d\nIR:\n%s", count, ir)
	}
	// Two wrapper functions generated.
	wrapCount := strings.Count(ir, "__task_wrapper_")
	if wrapCount < 2 {
		t.Errorf("esperado ao menos 2 __task_wrapper_, obtido %d\nIR:\n%s", wrapCount, ir)
	}
}

// TestTaskFanWithPipeCodegen verifies `n |> Task.fan(f, g)` emits correct IR via pipe.
func TestTaskFanWithPipeCodegen(t *testing.T) {
	src := `
fn inc(n: Int) -> Int = n + 1
fn dec(n: Int) -> Int = n - 1

fn main() {
  val n = 10
  val (t1, t2) = n |> Task.fan(inc, dec)
  t1.detach()
  t2.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR de Task.fan via pipe")
	}
	if !strings.Contains(ir, "__task_wrapper_") {
		t.Error("esperado __task_wrapper_ no IR de Task.fan via pipe")
	}
}

// TestTaskFanHandlesPackedInTuple verifies that the handles are stored in a struct
// (soyuz_alloc call indicates heap-allocated tuple packing).
func TestTaskFanHandlesPackedInTuple(t *testing.T) {
	src := `
fn square(n: Int) -> Int = n * n
fn cube(n: Int) -> Int = n * n * n

fn main() {
  val n = 3
  val (t1, t2) = n |> Task.fan(square, cube)
  t1.detach()
  t2.detach()
}
`
	ir := compileTask(t, src)
	// soyuz_alloc is used to pack the handles into a heap-allocated tuple struct.
	if !strings.Contains(ir, "soyuz_alloc") {
		t.Error("esperado soyuz_alloc para empacotar handles da Task.fan")
	}
}

// TestTaskFanSetsTaskResultInWrappers verifies each wrapper stores the fn result.
func TestTaskFanSetsTaskResultInWrappers(t *testing.T) {
	src := `
fn negate(n: Int) -> Int = n * -1
fn abs(n: Int) -> Int = n

fn main() {
  val n = 7
  val (t1, t2) = n |> Task.fan(negate, abs)
  val r1 = t1.await()
  val r2 = t2.await()
  val _ = r1 + r2
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("esperado srt_set_task_result nos wrappers de Task.fan")
	}
	if !strings.Contains(ir, "srt_await") {
		t.Error("esperado srt_await para t1.await() e t2.await()")
	}
}
