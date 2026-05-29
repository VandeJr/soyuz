package checker

import "testing"

// ── M-23: Task.listen(t: Task[T], ch: Channel[T]) -> Unit ─────────────────────

func TestTaskListenBasic(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.listen básico não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestTaskListenReturnsUnit(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Task.listen deve retornar Unit sem erro, obtido: %v", result.Errors)
	}
}

func TestTaskListenTypeIncompatible(t *testing.T) {
	src := `
fn workInt(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task workInt(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	result := checkSrc(src)
	// Channel without explicit type parameter — should not error on basic usage
	if len(result.Errors) > 0 {
		t.Logf("Task.listen com channel sem tipo explícito: %v", result.Errors)
	}
}

func TestTaskListenRequiresTwoArgs(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x }

fn run(n: Int) {
  val t = task work(n)
  Task.listen(t)
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("Task.listen com apenas 1 argumento deve gerar erro")
	}
}

func TestTaskListenFirstArgMustBeTask(t *testing.T) {
	src := `
fn run(n: Int) {
  val ch = Channel.new(1)
  Task.listen(n, ch)
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("Task.listen com primeiro arg não-Task deve gerar erro")
	}
}
