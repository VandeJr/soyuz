package checker

import "testing"


// M-26: Task.gather(list: List[T], fn: T -> U) -> List[U]

func TestTaskGatherReturnsList(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int = n * 2
fn run(nums: List[Int]) {
    val results = Task.gather(nums, dobrar)
}
`
	res := checkSrc(src)
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
}

func TestTaskGatherTypeInference(t *testing.T) {
	src := `
fn toString(n: Int) -> String = "x"
fn run(nums: List[Int]) {
    val results: List[String] = Task.gather(nums, toString)
}
`
	res := checkSrc(src)
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
}

func TestTaskGatherRejectsNonList(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int = n * 2
fn run(n: Int) {
    val _ = Task.gather(n, dobrar)
}
`
	res := checkSrc(src)
	if len(res.Errors) == 0 {
		t.Fatal("expected error: Task.gather requires List[T] as first argument")
	}
}

func TestTaskGatherRejectsNonFunction(t *testing.T) {
	src := `
fn run(nums: List[Int], x: Int) {
    val _ = Task.gather(nums, x)
}
`
	res := checkSrc(src)
	if len(res.Errors) == 0 {
		t.Fatal("expected error: Task.gather second argument must be a function")
	}
}

func TestTaskGatherRejectsWrongArgCount(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int = n * 2
fn run(nums: List[Int]) {
    val _ = Task.gather(nums)
}
`
	res := checkSrc(src)
	if len(res.Errors) == 0 {
		t.Fatal("expected error: Task.gather requires exactly 2 arguments")
	}
}
