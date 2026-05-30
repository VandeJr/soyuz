package checker

import "testing"

func TestAsyncPipePartialApplication(t *testing.T) {
	src := `
fn somar(a: Int, b: Int) -> Int = a + b
fn main() {
    val t = task (5 ~> somar(10, _))
    print("$(t.await())")
}`
	res := checkSrc(src)
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
}
