package codegen

import (
	"os"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func TestAsyncSyIR(t *testing.T) {
	src, err := os.ReadFile("../../examples/async.sy")
	if err != nil {
		t.Fatalf("cannot read async.sy: %v", err)
	}
	entry := t.TempDir() + "/async.sy"
	if err := os.WriteFile(entry, src, 0644); err != nil {
		t.Fatal(err)
	}
	stdlib := "../../std/lib"
	resolver := module.NewResolverWithStdlib(entry, stdlib)
	files, err2 := module.Collect(entry, resolver)
	if err2 != nil {
		t.Fatal(err2)
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
	mod, genErr := New(res).Generate(&parser.Program{Body: nodes})
	if genErr != nil {
		t.Fatalf("codegen: %v", genErr)
	}
	ir := mod.String()
	os.WriteFile("/tmp/async.ll", []byte(ir), 0644)
	t.Logf("IR written to /tmp/async.ll (len=%d)", len(ir))
}
