package compile_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func TestParseSoyuzParseDeclsFile(t *testing.T) {
	root := filepath.Join("..", "..", "..", "soyuz")
	f := filepath.Join(root, "src", "parser", "parse_decls.sy")
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	tokens := lexer.Tokenize(string(data))
	p := parser.New(tokens)
	prog := p.Parse()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("parse_decls.sy took %v (expected <2s)", elapsed)
	}
	if p.HasErrors() {
		t.Fatalf("parse errors: %v", p.Errors()[0])
	}
	if len(prog.Body) == 0 {
		t.Fatal("expected non-empty program")
	}
}

func TestCompileSoyuzParserPackage(t *testing.T) {
	root := filepath.Join("..", "..", "..", "soyuz")
	entry := filepath.Join(root, "tools", "import_load.sy")
	if _, err := os.Stat(entry); err != nil {
		t.Skip("soyuz import_load not found")
	}

	resolver := module.NewResolverWithStdlib(entry, "")
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatal(err)
	}

	allNodes, nodeFile, err := parseFiles(files, resolver)
	if err != nil {
		t.Fatal(err)
	}

	c := checker.New()
	c.SetNodeFiles(nodeFile)
	if prelude, perr := module.ResolvePrelude(resolver); perr == nil {
		c.SetPreludeFiles(prelude)
	}

	done := make(chan struct{})
	var result *checker.CheckResult
	go func() {
		result = c.Check(&parser.Program{Body: allNodes})
		close(done)
	}()

	select {
	case <-done:
		if len(result.Errors) > 0 {
			t.Logf("checker errors: %d (first: %v)", len(result.Errors), result.Errors[0])
		}
	case <-time.After(60 * time.Second):
		t.Fatal("checker.Check timed out after 60s")
	}
}

func init() {
	fmt.Println("parse_decls_compile_test loaded")
}
