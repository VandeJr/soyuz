package codegen

import (
	"os"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func TestInterfaceReturnWrapsFatPointer(t *testing.T) {
	src := `fn unwrapResult(r: Result[Int]) -> Int = match r {
    Ok(v) => v
    Err(e) => { print("Erro: $(e.message())"); 0 }
}
fn main() { print(unwrapResult(Err(noneError("falhou")))) }`
	entry := t.TempDir() + "/main.sy"
	if err := os.WriteFile(entry, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	stdlib := "../../std/lib"
	resolver := module.NewResolverWithStdlib(entry, stdlib)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []parser.Node
	nodeFile := map[parser.Node]string{}
	for _, f := range files {
		data, _ := os.ReadFile(f)
		prog := parser.New(lexer.Tokenize(string(data))).Parse()
		for _, n := range prog.Body {
			nodeFile[n] = f
			nodes = append(nodes, n)
		}
	}
	c := checker.New()
	c.SetNodeFiles(nodeFile)
	if pf, _ := module.ResolvePrelude(resolver); pf != nil {
		c.SetPreludeFiles(pf)
	}
	res := c.Check(&parser.Program{Body: nodes})
	if len(res.Errors) > 0 {
		t.Fatalf("checker: %v", res.Errors)
	}
	mod, err := New(res).Generate(&parser.Program{Body: nodes})
	if err != nil {
		t.Fatal(err)
	}
	ir := mod.String()
	if !containsAll(ir,
		"define i8* @noneError",
		"__vtable_NoneError_Error",
		"define i64 @unwrapResult",
	) {
		t.Fatalf("IR missing expected interface return symbols:\n%s", ir)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
