package codegen

import (
	"strings"
	"testing"
)

// TestTaskGatherEmitsSpawnAndAwaitLoops verifies that Task.gather emits
// two loops (spawn + await) and uses srt_enqueue + srt_await.
func TestTaskGatherEmitsSpawnAndAwaitLoops(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int { n * 2 }

fn main() {
    val nums: List[Int] = [1, 2, 3]
    val results = Task.gather(nums, dobrar)
    print(results.size())
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "gather_spawn_cond") {
		t.Error("expected gather_spawn_cond block for spawn loop")
	}
	if !strings.Contains(ir, "gather_await_cond") {
		t.Error("expected gather_await_cond block for await loop")
	}
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in Task.gather IR")
	}
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await in Task.gather IR")
	}
}
