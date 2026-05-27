package checker

import "testing"

// ── M-21: for task — parallel map sugar ───────────────────────────────────────

func TestForTaskCallExpr(t *testing.T) {
	src := `
fn double(x: Int) -> Int { x * 2 }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { double(n) }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("for task com call simples não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestForTaskPipeExpr(t *testing.T) {
	src := `
fn square(x: Int) -> Int { x * x }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { n |> square }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("for task com pipe não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestForTaskReturnTypeIsListOfBodyType(t *testing.T) {
	src := `
fn negate(x: Int) -> Int { x * -1 }

fn run(nums: List[Int]) -> List[Int] {
  for task n in nums { negate(n) }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("for task deve retornar List[U] (tipo do body), obtido: %v", result.Errors)
	}
}

func TestForTaskIterableMustBeList(t *testing.T) {
	src := `
fn double(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  for task x in n { double(x) }
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("for task com iterável não-List deve gerar erro")
	}
}

func TestForTaskBindingVisibleInBody(t *testing.T) {
	src := `
fn process(x: Int) -> Int { x + 1 }

fn run(items: List[Int]) -> List[Int] {
  for task item in items { process(item) }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("binding deve ser visível no body, erros: %v", result.Errors)
	}
}
