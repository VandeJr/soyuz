package checker

import "testing"

// ── M-25: Task[T].then(fn: T -> U) + Task[Result[T]].catch(fn) ───────────────

func TestTaskThenReturnsNewTaskType(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }
fn transform(x: Int) -> Float { 1.0 }

fn run(n: Int) -> Float {
  val t = task work(n)
  val t2 = t.then(fn(r) => transform(r))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".then não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskThenInfersReturnType(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x + 1 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.then(fn(r) => r * 2)
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".then com lambda Int->Int deve retornar Task[Int], obtido: %v", result.Errors)
	}
}

func TestTaskThenChaining(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.then(fn(r) => r + 1).then(fn(r) => r * 2)
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".then encadeado não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskThenRequiresOneArg(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task work(n)
  val _ = t.then()
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal(".then sem argumento deve gerar erro")
	}
}

func TestTaskThenInjectsFnHints(t *testing.T) {
	src := `
fn work(x: Float) -> Float { x * 2.0 }

fn run(n: Float) -> Float {
  val t = task work(n)
  val t2 = t.then(fn(r) => r + 1.0)
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".then deve injetar hint Float para lambda, obtido: %v", result.Errors)
	}
}

func TestTaskCatchOnResultTask(t *testing.T) {
	src := `
fn work(x: Int) -> Result[Int] { Ok(x) }

fn run(n: Int) -> Result[Int] {
  val t = task work(n)
  val t2 = t.catch(fn(e) => Ok(0))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".catch em Task[Result[T]] não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskCatchRequiresResultTask(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task work(n)
  val _ = t.catch(fn(e) => 0)
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal(".catch em Task[Int] (não Result) deve gerar erro")
	}
}
