package codegen

import (
	"testing"
	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func compileCheck(t *testing.T, src string) string {
	t.Helper()
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker: %v", res.Errors)
	}
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	return mod.String()
}

// Test 1: Generic enum with record payload (Tree[T])
func TestADT1GenericEnumRecord(t *testing.T) {
	src := `
enum Tree[T] {
	Leaf { val: T }
	Node { left: Tree[T], right: Tree[T] }
}
fn main() -> Int {
	val a = Tree.Leaf(42)
	return 0
}
`
	compileCheck(t, src)
}

// Test 2: Result[T] with a class implementing Error
func TestADT2ResultWithError(t *testing.T) {
	src := `
class DivisionError {
	pub fn mensagem(self) -> String { return "divisao por zero" }
}

fn divide(a: Int, b: Int) -> Result[Int] {
	if b == 0 {
		return Err(DivisionError {})
	}
	return Ok(a)
}

fn main() -> Int {
	val r = divide(10, 2)
	return match r {
		Ok(v) => v
		Err(e) => 0
	}
}
`
	compileCheck(t, src)
}

// Test 3: Nested Option[Result[Int]] with explicit Some wrapping
func TestADT3NestedOption(t *testing.T) {
	src := `
fn buscar(id: Int) -> Result[Int]? {
	if id < 0 {
		return None
	}
	return Some(Ok(id * 2))
}

fn main() -> Int {
	val r = buscar(5)
	return match r {
		Some(res) => match res {
			Ok(v) => v
			Err(e) => -1
		}
		None => -2
	}
}
`
	compileCheck(t, src)
}
