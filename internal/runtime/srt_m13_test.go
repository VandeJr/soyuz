package runtime

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestM13WorkStealing compiles and runs srt_bench.c, which exercises the
// M-13 Chase-Lev work-stealing deque: 256-task correctness, recursive fan-out
// (tasks spawning tasks), and a throughput benchmark.
func TestM13WorkStealing(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	srcDir := filepath.Join(filepath.Dir(filename), "src")

	tmp := t.TempDir()
	out := filepath.Join(tmp, "srt_bench")

	args := []string{
		"-std=c11", "-O2", "-pthread",
		filepath.Join(srcDir, "soyuz_rt.c"),
		filepath.Join(srcDir, "srt_bench.c"),
		"-o", out,
	}
	cmd := exec.Command("cc", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, output)
	}

	run := exec.Command(out)
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("srt_bench failed: %v\n%s", err, output)
	}
}
