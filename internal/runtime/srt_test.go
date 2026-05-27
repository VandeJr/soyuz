package runtime

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestM12CooperativeScheduler compiles and runs srt_test.c, which exercises
// the M-12 ucontext cooperative scheduler: basic await, chain await (task
// yields to scheduler while waiting for inner task), parallel tasks, and stress.
func TestM12CooperativeScheduler(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	srcDir := filepath.Join(filepath.Dir(filename), "src")

	tmp := t.TempDir()
	out := filepath.Join(tmp, "srt_test")

	args := []string{
		"-std=c11", "-O0", "-pthread",
		filepath.Join(srcDir, "soyuz_rt.c"),
		filepath.Join(srcDir, "srt_test.c"),
		"-o", out,
	}
	cmd := exec.Command("cc", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, output)
	}

	run := exec.Command(out)
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("srt_test failed: %v\n%s", err, output)
	}
}
