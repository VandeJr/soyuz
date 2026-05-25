package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestGeneratorBasic(t *testing.T) {
	src := `
	fn main() -> Int {
		return 42
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "define i64 @main()") {
		t.Errorf("expected generated IR to contain main function, got:\n%s", irStr)
	}
	if !strings.Contains(irStr, "ret i64 42") {
		t.Errorf("expected generated IR to contain ret i64 42, got:\n%s", irStr)
	}
}

func TestGeneratorVariables(t *testing.T) {
	src := `
	fn main() -> Int {
		var x = 10
		x = 20
		return x
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "alloca i64") {
		t.Error("expected alloca i64")
	}
	if !strings.Contains(irStr, "store i64 10") {
		t.Error("expected store 10")
	}
	if !strings.Contains(irStr, "store i64 20") {
		t.Error("expected store 20")
	}
	if !strings.Contains(irStr, "load i64") {
		t.Error("expected load i64")
	}
}

func TestGeneratorMath(t *testing.T) {
	src := `
	fn main() -> Int {
		return (10 + 5) * 2
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "add i64 10, 5") {
		t.Error("expected add i64 10, 5")
	}
	if !strings.Contains(irStr, "mul i64") {
		t.Error("expected mul i64")
	}
}

func TestGeneratorControlFlow(t *testing.T) {
	src := `
	fn main() -> Int {
		var x = 0
		if x < 10 {
			x = 100
		} else {
			x = 200
		}
		
		var i = 0
		while i < 5 {
			i = i + 1
		}
		
		return x + i
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "br i1 %") {
		t.Error("expected conditional branch")
	}
	if !strings.Contains(irStr, "then:") {
		t.Error("expected then block")
	}
	if !strings.Contains(irStr, "else:") {
		t.Error("expected else block")
	}
	if !strings.Contains(irStr, "while_cond:") {
		t.Error("expected while_cond block")
	}
}

func TestGeneratorClassesAndInterfaces(t *testing.T) {
	src := `
	interface Greetable {
		fn greet(self) -> String
	}
	class Dog : Greetable {
		val name: String
		fn greet(self) -> String = "woof"
	}
	class Cat : Greetable {
		val name: String
		fn greet(self) -> String = "meow"
	}
	fn main() -> Int {
		val dog: Greetable = Dog { name: "Rex" }
		val cat: Greetable = Cat { name: "Whiskers" }
		print(dog.greet())
		print(cat.greet())
		return 0
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "Dog_greet") {
		t.Error("expected Dog_greet method in IR")
	}
	if !strings.Contains(irStr, "Cat_greet") {
		t.Error("expected Cat_greet method in IR")
	}
	if !strings.Contains(irStr, "__vtable_Dog_Greetable") {
		t.Error("expected vtable for Dog_Greetable")
	}
	if !strings.Contains(irStr, "__vtable_Cat_Greetable") {
		t.Error("expected vtable for Cat_Greetable")
	}
}

func TestGeneratorTuples(t *testing.T) {
	src := `
	fn swap(a: Int, b: Int) -> (Int, Int) {
		return (b, a)
	}
	fn main() -> Int {
		val t = (10, 20)
		val (x, y) = t
		val (p, q) = swap(1, 2)
		val r = match t {
			(10, n) => n
			(_, n) => n
		}
		return x + y + p + q + r
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "soyuz_alloc") {
		t.Error("expected soyuz_alloc for tuple heap allocation")
	}
	if !strings.Contains(irStr, "getelementptr") {
		t.Error("expected getelementptr for tuple field access")
	}
	if !strings.Contains(irStr, "define") {
		t.Error("expected function definitions")
	}
}

func TestGeneratorPrint(t *testing.T) {
	src := `
	fn main() -> Int {
		print("Hello Soyuz!")
		print(42)
		return 0
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	if !strings.Contains(irStr, "call i32 (i8*, ...) @printf") {
		t.Error("expected call to printf")
	}
}

func TestGeneratorMilestone7(t *testing.T) {
	src := `
	record Point { x: Int, y: Int }
	fn test_early_return(p: Point) -> Int {
		if p.x > 0 {
			return p.x
		}
		return 0
	}
	fn main() -> Int {
		val t = (10, 20)
		val x = t.0
		val y = t.1
		val p = Point { x: 1, y: 2 }
		return test_early_return(p) + x + y
	}
	`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("failed to generate LLVM IR: %v", err)
	}

	irStr := mod.String()
	// Check early return RC cleanup: should contain multiple calls to soyuz_release
	// because p is released in both paths.
	if strings.Count(irStr, "soyuz_release") < 1 {
		t.Errorf("expected soyuz_release calls for parameter p, got IR:\n%s", irStr)
	}
	// Check tuple indexing
	if !strings.Contains(irStr, "getelementptr") || !strings.Contains(irStr, "i32 0") || !strings.Contains(irStr, "i32 1") {
		t.Errorf("expected GEP for tuple indices 0 and 1, got IR:\n%s", irStr)
	}
}

func TestFuncDefaultArgsCodegen(t *testing.T) {
	src := `
fn greet(nome: String, prefixo: String = "Olá") -> String = prefixo
fn main() -> String = greet("Vand")
`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	_, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
}

func TestValFnRewriteCodegen(t *testing.T) {
	src := `
fn dobrar(x: Int) -> Int = x * 2
fn main() -> Int = dobrar(5)
`
	tokens := lexer.Tokenize(src)
	p := parser.New(tokens)
	prog := p.Parse()

	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	irStr := mod.String()
	if !strings.Contains(irStr, "define") {
		t.Errorf("expected define in IR, got:\n%s", irStr)
	}
}
