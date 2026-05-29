package codegen

import (
	"strings"
	"testing"
)

// M-26: Task.gather(list, fn) — parallel map

func TestTaskGatherEmitsSrtEnqueue(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int { n * 2 }
fn run(nums: List[Int]) -> List[Int] {
    Task.gather(nums, dobrar)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in Task.gather IR")
	}
}

func TestTaskGatherEmitsSrtAwait(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int { n * 2 }
fn run(nums: List[Int]) -> List[Int] {
    Task.gather(nums, dobrar)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await in Task.gather IR")
	}
}

func TestTaskGatherEmitsGatherBlocks(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int { n * 2 }
fn run(nums: List[Int]) -> List[Int] {
    Task.gather(nums, dobrar)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "gather_spawn_cond") {
		t.Error("expected gather_spawn_cond block")
	}
	if !strings.Contains(ir, "gather_await_cond") {
		t.Error("expected gather_await_cond block")
	}
}

func TestTaskGatherEmitsWrapperFunc(t *testing.T) {
	src := `
fn quadrado(n: Int) -> Int { n * n }
fn run(nums: List[Int]) -> List[Int] {
    Task.gather(nums, quadrado)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__task_wrapper_") {
		t.Error("expected __task_wrapper_ function for Task.gather")
	}
}

func TestTaskGatherReturnsListIR(t *testing.T) {
	src := `
fn incrementar(n: Int) -> Int { n + 1 }
fn run(nums: List[Int]) -> List[Int] {
    Task.gather(nums, incrementar)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "soyuz_list_new") {
		t.Error("expected soyuz_list_new for results list in Task.gather")
	}
	if !strings.Contains(ir, "soyuz_list_append") {
		t.Error("expected soyuz_list_append to collect results")
	}
}
