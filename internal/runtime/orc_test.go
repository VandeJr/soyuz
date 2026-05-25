package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestORCCycleCollection(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	srcDir := filepath.Join(filepath.Dir(filename), "src")
	testC := filepath.Join(srcDir, "orc_test.c")
	if _, err := os.Stat(testC); err != nil {
		t.Fatalf("orc_test.c not found: %v", err)
	}

	tmp := t.TempDir()
	out := filepath.Join(tmp, "orc_test")

	args := []string{
		"-std=c11", "-O0",
		filepath.Join(srcDir, "rc.c"),
		testC,
		"-o", out,
	}
	cmd := exec.Command("cc", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile failed: %v\n%s", err, out)
	}

	run := exec.Command(out)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("orc test failed: %v\n%s", err, out)
	}
}
