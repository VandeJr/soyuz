package lsp

import (
	"fmt"
	"os"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func TestCheckLexerSy(t *testing.T) {
	filePath := "../../soyuz-new/lib/lexer/lexer.sy"
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Skip("soyuz-new/lib/lexer/lexer.sy not found:", err)
	}
	src := string(data)
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()

	resolver := module.NewResolverWithStdlib(filePath, "../../std/lib")
	resolver.Root = "../../soyuz-new"

	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	visited := make(map[string]bool)
	visited[filePath] = true

	var load func(file string, p *parser.Program)
	load = func(file string, p *parser.Program) {
		for _, node := range p.Body {
			imp, isImp := node.(*parser.ImportDecl)
			if !isImp {
				if nodeFile[node] == "" {
					nodeFile[node] = file
					allNodes = append(allNodes, node)
				}
				continue
			}
			resolved, rerr := resolver.Resolve(imp)
			if rerr != nil {
				t.Logf("unresolved import: %v", rerr)
				if nodeFile[node] == "" {
					nodeFile[node] = file
					allNodes = append(allNodes, node)
				}
				continue
			}
			imp.ResolvedFiles = resolved
			for _, f := range resolved {
				if visited[f] {
					continue
				}
				visited[f] = true
				d, _ := os.ReadFile(f)
				sub := parser.New(lexer.Tokenize(string(d))).Parse()
				load(f, sub)
			}
			if nodeFile[node] == "" {
				nodeFile[node] = file
				allNodes = append(allNodes, node)
			}
		}
	}
	load(filePath, prog)

	merged := &parser.Program{Body: allNodes}
	c := checker.New()
	c.SetNodeFiles(nodeFile)
	res := c.Check(merged)

	if len(res.Errors) > 0 {
		for _, e := range res.Errors {
			fmt.Printf("  error: %v\n", e)
		}
		t.Fatalf("%d checker error(s) in lexer.sy", len(res.Errors))
	}
}
