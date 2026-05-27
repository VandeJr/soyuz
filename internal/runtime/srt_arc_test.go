package runtime

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestM14ArcEBR compiles and runs srt_arc_test.c, which exercises the
// M-14 Arc[T] epoch-based reclamation: basic lifecycle, multi-clone,
// cross-task sharing, EBR deferred reclamation, and a throughput benchmark.
func TestM14ArcEBR(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	srcDir := filepath.Join(filepath.Dir(filename), "src")

	tmp := t.TempDir()
	out := filepath.Join(tmp, "srt_arc_test")

	args := []string{
		"-std=c11", "-O2", "-pthread",
		filepath.Join(srcDir, "std_arc.c"),
		filepath.Join(srcDir, "soyuz_rt.c"),
		filepath.Join(srcDir, "srt_arc_test.c"),
		"-o", out,
	}
	cmd := exec.Command("cc", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, output)
	}

	run := exec.Command(out)
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("srt_arc_test failed: %v\n%s", err, output)
	}
}
