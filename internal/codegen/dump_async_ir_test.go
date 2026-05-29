package codegen_test

import (
	"os"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/codegen"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func TestDumpAsyncIR(t *testing.T) {
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

	var allNodes []parser.Node
	nodeFile := map[parser.Node]string{}
	for _, file := range files {
		data, _ := os.ReadFile(file)
		tokens := lexer.Tokenize(string(data))
		p := parser.New(tokens)
		prog := p.Parse()
		for _, node := range prog.Body {
			nodeFile[node] = file
			allNodes = append(allNodes, node)
		}
	}

	c := checker.New()
	c.SetNodeFiles(nodeFile)
	if pf, _ := module.ResolvePrelude(resolver); pf != nil {
		c.SetPreludeFiles(pf)
	}

	mergedProg := &parser.Program{Body: allNodes}
	res := c.Check(mergedProg)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	mod, genErr := codegen.New(res).Generate(mergedProg)
	if genErr != nil {
		t.Fatalf("codegen error: %v", genErr)
	}

	os.WriteFile("/tmp/async_current.ll", []byte(mod.String()), 0644)
	t.Log("IR written to /tmp/async_current.ll")
}
