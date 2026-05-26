//go:build ignore

package main

import (
	"fmt"
	"os"
	"time"
	"soyuz/internal/checker"
	"soyuz/internal/codegen"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
	soyuzstdlib "soyuz/std"
	"path/filepath"
)

func phase(name string, fn func() error) {
	fmt.Fprintf(os.Stderr, "[%s] %s starting...\n", time.Now().Format("15:04:05.000"), name)
	if err := fn(); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] %s ERROR: %v\n", time.Now().Format("15:04:05.000"), name, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "[%s] %s done\n", time.Now().Format("15:04:05.000"), name)
}

func main() {
	inputFile := "soyuz-new/main.sy"
	absInput, _ := filepath.Abs(inputFile)

	tmpDir, _ := os.MkdirTemp("", "soyuz-diag-")
	defer os.RemoveAll(tmpDir)

	stdlibDir := filepath.Join(tmpDir, "stdlib")
	os.MkdirAll(stdlibDir, 0755)
	for name, data := range soyuzstdlib.Files {
		dest := filepath.Join(stdlibDir, name)
		os.MkdirAll(filepath.Dir(dest), 0755)
		os.WriteFile(dest, data, 0644)
	}

	var files []string
	phase("resolve imports", func() error {
		resolver := module.NewResolverWithStdlib(absInput, stdlibDir)
		var err error
		files, err = module.Collect(absInput, resolver)
		fmt.Fprintf(os.Stderr, "  -> %d files\n", len(files))
		for _, f := range files {
			fmt.Fprintf(os.Stderr, "  -> %s\n", filepath.Base(f))
		}
		return err
	})

	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	resolver := module.NewResolverWithStdlib(absInput, stdlibDir)

	phase("parse all files", func() error {
		for _, file := range files {
			data, _ := os.ReadFile(file)
			tokens := lexer.Tokenize(string(data))
			p := parser.New(tokens)
			prog := p.Parse()
			for _, node := range prog.Body {
				if imp, isImport := node.(*parser.ImportDecl); isImport {
					if resolved, rerr := resolver.Resolve(imp); rerr == nil {
						imp.ResolvedFiles = resolved
					}
					nodeFile[node] = file
					allNodes = append(allNodes, node)
					continue
				}
				nodeFile[node] = file
				allNodes = append(allNodes, node)
			}
		}
		fmt.Fprintf(os.Stderr, "  -> %d AST nodes\n", len(allNodes))
		return nil
	})

	mergedProg := &parser.Program{Body: allNodes}
	var result *checker.CheckResult

	phase("type check", func() error {
		c := checker.New()
		c.SetNodeFiles(nodeFile)
		result = c.Check(mergedProg)
		fmt.Fprintf(os.Stderr, "  -> %d errors\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  error: %v\n", e)
		}
		return nil
	})

	phase("codegen", func() error {
		g := codegen.New(result)
		_, err := g.Generate(mergedProg)
		return err
	})

	fmt.Fprintln(os.Stderr, "All phases complete!")
}
