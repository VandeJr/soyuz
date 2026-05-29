package checker

import "testing"

// ── M-24: Task[T].always(fn: Unit -> Unit) -> Task[T] ─────────────────────────

func TestTaskAlwaysReturnsTaskT(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => print("done"))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".always não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskAlwaysPreservesTaskType(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => print("cleanup"))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".always deve preservar Task[T] inalterado, obtido: %v", result.Errors)
	}
}

func TestTaskAlwaysRequiresOneArg(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task work(n)
  val _ = t.always()
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal(".always sem argumento deve gerar erro")
	}
}

func TestTaskAlwaysChainingWithTap(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x + 1 }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.tap(fn(r) => print(r)).always(fn() => print("fim"))
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".tap seguido de .always não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskAlwaysCallbackNoArgs(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn cleanup() { print("cleanup") }

fn run(n: Int) -> Int {
  val t = task work(n)
  val t2 = t.always(fn() => cleanup())
  t2.await()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf(".always com fn sem args não deve gerar erros, obtido: %v", result.Errors)
	}
}
