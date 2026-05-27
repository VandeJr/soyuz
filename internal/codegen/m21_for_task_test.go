package codegen

import (
	"strings"
	"testing"
)

// ── M-21: for task — parallel map sugar ───────────────────────────────────────

func TestForTaskEmitsSrtEnqueue(t *testing.T) {
	src := `
fn double(x: Int) -> Int { x * 2 }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { double(n) }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in IR for for-task spawn phase")
	}
}

func TestForTaskEmitsSrtAwait(t *testing.T) {
	src := `
fn double(x: Int) -> Int { x * 2 }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { double(n) }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await in IR for for-task await phase")
	}
}

func TestForTaskEmitsSpawnAndAwaitLoops(t *testing.T) {
	src := `
fn inc(x: Int) -> Int { x + 1 }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { inc(n) }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "ft_spawn_cond") {
		t.Error("expected ft_spawn_cond block in IR")
	}
	if !strings.Contains(ir, "ft_await_cond") {
		t.Error("expected ft_await_cond block in IR")
	}
}

func TestForTaskEmitsTaskWrapper(t *testing.T) {
	src := `
fn square(x: Int) -> Int { x * x }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { square(n) }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__task_wrapper") {
		t.Error("expected __task_wrapper function in IR")
	}
}

func TestForTaskEmitsSoyuzListNew(t *testing.T) {
	src := `
fn negate(x: Int) -> Int { x * -1 }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { negate(n) }
}
`
	ir := compileTask(t, src)
	// Should create at least two lists: handles list + results list
	count := strings.Count(ir, "soyuz_list_new")
	if count < 2 {
		t.Errorf("expected at least 2 calls to soyuz_list_new, got %d", count)
	}
}
