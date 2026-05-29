package checker

import "testing"

// ── M-22: Task[T].tap(fn: T -> Unit) -> Task[T] ───────────────────────────────

func TestTaskTapReturnsTaskT(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }
fn logIt(x: Int) { print(x) }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => logIt(r))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".tap não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskTapPreservesTaskType(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".tap deve preservar Task[T], obtido: %v", result.Errors)
	}
}

func TestTaskTapChaining(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x + 1 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r)).tap(fn(r) => print(r))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".tap encadeado não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskTapRequiresOneArg(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task work(n)
  val _ = t.tap()
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal(".tap sem argumento deve gerar erro")
	}
}

func TestTaskTapLambdaReceivesCorrectType(t *testing.T) {
	src := `
fn produce() -> Float { 3.14 }

fn run() -> Float {
  val t = task produce()
  val t2 = t.tap(fn(r) => print(r))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".tap lambda deve receber tipo T correto, obtido: %v", result.Errors)
	}
}
