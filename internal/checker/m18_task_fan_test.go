package checker

import (
	"testing"
)

// ── M-18: Task.fan — fan-out paralelo ────────────────────────────────────────

// TestTaskFanReturnsTuple verifies that Task.fan(input, f, g) returns TupleType[Task[A], Task[B]].
func TestTaskFanReturnsTuple(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn negate(n: Int) -> Int = n * -1

fn main() {
  val input = 5
  val (t1, t2) = Task.fan(input, double, negate)
  t1.detach()
  t2.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.fan não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskFanWithPipe verifies `input |> Task.fan(f, g)` syntax (pipe injects input as first arg).
func TestTaskFanWithPipe(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn toString(n: Int) -> String = "x"

fn main() {
  val n = 10
  val (t1, t2) = n |> Task.fan(double, toString)
  t1.detach()
  t2.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("n |> Task.fan(f, g) não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskFanDestructureAndAwait verifies destructuring fan result and awaiting each handle.
func TestTaskFanDestructureAndAwait(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn triple(n: Int) -> Int = n * 3

fn main() -> Int {
  val n = 7
  val (t1, t2) = n |> Task.fan(double, triple)
  val r1 = t1.await()
  val r2 = t2.await()
  return r1 + r2
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("destructure e await de Task.fan não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskFanThreeFunctionsDistinctTypes verifies fan-out with 3 functions of different return types.
func TestTaskFanThreeFunctionsDistinctTypes(t *testing.T) {
	src := `
fn toInt(n: Int) -> Int = n
fn toBool(n: Int) -> Bool = n > 0
fn toStr(n: Int) -> String = "x"

fn main() {
  val n = 42
  val (t1, t2, t3) = n |> Task.fan(toInt, toBool, toStr)
  t1.detach()
  t2.detach()
  t3.detach()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.fan com 3 funções distintas não deve gerar erros, obtido: %v", result.Errors)
	}
}

// TestTaskFanFirstArgIsFunction verifies an error when all args are functions (no input value).
func TestTaskFanFirstArgIsFunction(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn triple(n: Int) -> Int = n * 3

fn main() {
  val _ = Task.fan(double, triple)
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("Task.fan(fn, fn) sem valor de entrada deve gerar erro")
	}
}
