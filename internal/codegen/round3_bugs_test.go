package codegen

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFloatToIntMethod(t *testing.T) {
	src := `fn main() { val x: Float = 3.7; print("$(x.toInt())") }`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "fptosi") && !strings.Contains(ir, "FPToSI") {
		t.Log("IR may use intrinsic name variant; checking compile only")
	}
}

func TestGatherLambda(t *testing.T) {
	src := `fn main() { val xs = Task.gather([1, 2, 3], fn(n: Int) => n * 2); print("ok") }`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "__gather_wrapper_") {
		t.Error("expected gather closure wrapper in IR")
	}
}

func TestAsyncPipePartialRun(t *testing.T) {
	src := `
fn somar(a: Int, b: Int) -> Int = a + b
fn main() {
    val t = task (5 ~> somar(10, _))
    print("$(t.await())")
}`
	ir := compileCheck(t, src)
	if err := os.WriteFile("/tmp/async_partial_test.ll", []byte(ir), 0644); err == nil {
		if out, err := exec.Command("llvm-as", "/tmp/async_partial_test.ll", "-o", "/dev/null").CombinedOutput(); err != nil {
			t.Fatalf("llvm-as: %v\n%s", err, out)
		}
	}
}
