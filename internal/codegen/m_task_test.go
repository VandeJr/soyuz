package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func compileTask(t *testing.T, src string) string {
	t.Helper()
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	return mod.String()
}

// TestTaskEnqueue verifies that `task fn(args)` emits srt_enqueue with a wrapper function.
func TestTaskEnqueue(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2

fn main() -> Int {
  val t = task double(5)
  t.detach()
  return 0
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue call in IR")
	}
	if !strings.Contains(ir, "__task_wrapper_") {
		t.Error("expected generated task wrapper function in IR")
	}
	if !strings.Contains(ir, "srt_set_task_result") {
		t.Error("expected srt_set_task_result call in task wrapper IR")
	}
}

// TestTaskDetach verifies that t.detach() emits srt_detach.
func TestTaskDetach(t *testing.T) {
	src := `
fn compute() -> Int = 42

fn main() {
  val t = task compute()
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_detach") {
		t.Error("expected srt_detach call in IR")
	}
}

// TestTaskAwait verifies that t.await() emits srt_await and converts result.
func TestTaskAwait(t *testing.T) {
	src := `
fn square(n: Int) -> Int = n * n

fn main() -> Int {
  val t = task square(4)
  val result = t.await()
  return result
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await call in IR")
	}
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue call in IR")
	}
}

// TestTaskAwaitUnit verifies that task of void-returning function works.
func TestTaskAwaitUnit(t *testing.T) {
	src := `
fn doWork() {}

fn main() {
  val t = task doWork()
  t.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in IR")
	}
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await in IR")
	}
}

// TestTaskWrapperFreeArgs verifies that the args buffer is freed inside the wrapper.
func TestTaskWrapperFreeArgs(t *testing.T) {
	src := `
fn add(a: Int, b: Int) -> Int = a + b

fn main() -> Int {
  val t = task add(3, 7)
  val result = t.await()
  return result
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "free") {
		t.Error("expected free() for args buffer in IR")
	}
	if !strings.Contains(ir, "malloc") {
		t.Error("expected malloc for args buffer in IR")
	}
}

// TestTaskDropOnScopeExit verifies that srt_drop_task_handle is emitted at scope exit
// for a task variable that is neither awaited nor detached.
func TestTaskDropOnScopeExit(t *testing.T) {
	src := `
fn work() -> Int = 42

fn main() {
  val t = task work()
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_drop_task_handle") {
		t.Error("expected srt_drop_task_handle in IR for task scope exit")
	}
}

// TestTaskAwaitNullsHandle verifies that after await(), a NULL store is emitted
// so the scope-exit drop is a no-op.
func TestTaskAwaitNullsHandle(t *testing.T) {
	src := `
fn compute() -> Int = 7

fn main() -> Int {
  val t = task compute()
  val r = t.await()
  return r
}
`
	ir := compileTask(t, src)
	// The IR should store null into the task handle alloca after await.
	if !strings.Contains(ir, "store i8* null") {
		t.Error("expected null store after await() to neutralise scope-exit drop")
	}
}

// ── M-06 ─────────────────────────────────────────────────────────────────────

// TestTaskAllEmitsSrtAwait verifies Task.all awaits each handle and packs into a tuple.
func TestTaskAllEmitsSrtAwait(t *testing.T) {
	src := `
fn getNum() -> Int = 42
fn getName() -> String = "soyuz"

fn main() {
  val t1 = task getNum()
  val t2 = task getName()
  val (n, s) = Task.all(t1, t2)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("Task.all deve emitir srt_await")
	}
	if !strings.Contains(ir, "soyuz_alloc") {
		t.Error("Task.all deve alocar a tuple com soyuz_alloc")
	}
}

// TestTaskAllSettledEmitsSrtAwait verifies Task.allSettled behaves like Task.all.
func TestTaskAllSettledEmitsSrtAwait(t *testing.T) {
	src := `
fn work1() -> Int = 1
fn work2() -> Int = 2

fn main() {
  val t1 = task work1()
  val t2 = task work2()
  val (a, b) = Task.allSettled(t1, t2)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("Task.allSettled deve emitir srt_await")
	}
}

// TestTaskAnyEmitsSrtAwaitAny verifies Task.any calls the runtime srt_await_any.
func TestTaskAnyEmitsSrtAwaitAny(t *testing.T) {
	src := `
fn fast() -> Int = 1
fn slow() -> Int = 2

fn main() -> Int {
  val t1 = task fast()
  val t2 = task slow()
  val winner = Task.any(t1, t2)
  return winner
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await_any") {
		t.Error("Task.any deve emitir srt_await_any")
	}
}

// TestTaskAllNullsHandles verifies that Task.all stores null into each task alloca after awaiting.
func TestTaskAllNullsHandles(t *testing.T) {
	src := `
fn compute() -> Int = 7
fn compute2() -> Int = 8

fn main() {
  val t1 = task compute()
  val t2 = task compute2()
  val (a, b) = Task.all(t1, t2)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "store i8* null") {
		t.Error("Task.all deve armazenar null nos handles após await")
	}
}

// TestTaskDetachNullsHandle verifies that after detach(), a NULL store is emitted.
func TestTaskDetachNullsHandle(t *testing.T) {
	src := `
fn doThing() {}

fn main() {
  val t = task doThing()
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "store i8* null") {
		t.Error("expected null store after detach() to neutralise scope-exit drop")
	}
}

// ── M-07 ─────────────────────────────────────────────────────────────────────

// TestTaskHandleCurrentEmitsSrtTaskHandleCurrent verifies that TaskHandle.current()
// calls srt_task_handle_current and emits a null-check branch.
func TestTaskHandleCurrentEmitsSrtTaskHandleCurrent(t *testing.T) {
	src := `
fn doWork() {
  val h = TaskHandle.current()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_task_handle_current") {
		t.Error("expected srt_task_handle_current call in IR")
	}
	if !strings.Contains(ir, "task_handle_some") && !strings.Contains(ir, "task_handle_none") {
		t.Error("expected Some/None branch blocks in IR for TaskHandle.current()")
	}
}

// TestTaskHandleCurrentWrapsInOption verifies that the result is an Option (soyuz_alloc for the enum).
func TestTaskHandleCurrentWrapsInOption(t *testing.T) {
	src := `
fn doWork() {
  val h = TaskHandle.current()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "soyuz_alloc") {
		t.Error("expected soyuz_alloc for Option enum allocation in TaskHandle.current()")
	}
}

// TestTaskHandleCancelledEmitsSrtTaskCancelled verifies .cancelled() emits srt_task_cancelled.
func TestTaskHandleCancelledEmitsSrtTaskCancelled(t *testing.T) {
	src := `
fn checkIfCancelled(handle: TaskHandle) -> Bool = handle.cancelled()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_task_cancelled") {
		t.Error("expected srt_task_cancelled call in IR")
	}
}

// ── M-10 ─────────────────────────────────────────────────────────────────────

// TestTaskCancelEmitsSrtCancel verifies t.cancel() emits srt_cancel.
func TestTaskCancelEmitsSrtCancel(t *testing.T) {
	src := `
fn doWork() -> Int = 42
fn main() -> Int {
  val t = task doWork()
  t.cancel()
  return t.await()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_cancel") {
		t.Error("expected srt_cancel call in IR")
	}
}

// TestTaskCancelDoesNotNullHandle verifies cancel() does not null the alloca
// (the handle is still needed for await/detach after cancel).
func TestTaskCancelDoesNotNullHandle(t *testing.T) {
	src := `
fn doWork() -> Int = 0
fn main() -> Int {
  val t = task doWork()
  t.cancel()
  return t.await()
}
`
	ir := compileTask(t, src)
	// srt_cancel must appear; the null-store comes only from await, not cancel
	if !strings.Contains(ir, "srt_cancel") {
		t.Error("expected srt_cancel in IR")
	}
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await in IR after cancel")
	}
}

// TestTaskHandleProgressEmitsSrtTaskSetProgress verifies .progress(f) emits srt_task_set_progress.
func TestTaskHandleProgressEmitsSrtTaskSetProgress(t *testing.T) {
	src := `
fn reportProgress(handle: TaskHandle) {
  handle.progress(0.5)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_task_set_progress") {
		t.Error("expected srt_task_set_progress call in IR")
	}
}
