package module_test

import (
	"os"
	"path/filepath"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func checkWithStdlib(t *testing.T, stdlibDir, src string) []checker.TypeError {
	t.Helper()
	dir := t.TempDir()
	entry := writeFile(t, dir, "main.sy", src)
	resolver := module.NewResolverWithStdlib(entry, stdlibDir)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	for _, file := range files {
		data, _ := os.ReadFile(file)
		tokens := lexer.Tokenize(string(data))
		p := parser.New(tokens)
		prog := p.Parse()
		for _, node := range prog.Body {
			if imp, isImp := node.(*parser.ImportDecl); isImp {
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

	merged := &parser.Program{}
	merged.Body = allNodes
	c := checker.New()
	if len(files) > 1 {
		c.SetNodeFiles(nodeFile)
	}
	return c.Check(merged).Errors
}

// TestStdlibBareImportNamespace verifica que import "@soyuz/mock" cria namespace mock.*.
func TestStdlibBareImportNamespace(t *testing.T) {
	stdlibDir := t.TempDir()
	writeFile(t, stdlibDir, "mock.sy", `
extern fn soyuz_print_str(s: String)
pub fn assert_true(cond: Bool, name: String) {
    soyuz_print_str(name)
}
`)
	src := `
import ( "@soyuz/mock" )
fn main() {
    mock.assert_true(true, "ok")
}
`
	if errs := checkWithStdlib(t, stdlibDir, src); len(errs) > 0 {
		t.Fatalf("erros inesperados com namespace mock.*: %v", errs)
	}
}

// TestStdlibSingleNameImport verifica import nomeado { assert_true } from "@soyuz/mock".
func TestStdlibSingleNameImport(t *testing.T) {
	stdlibDir := t.TempDir()
	writeFile(t, stdlibDir, "mock.sy", `
extern fn soyuz_print_str(s: String)
pub fn assert_true(cond: Bool, name: String) {
    soyuz_print_str(name)
}
`)
	src := `
import ( { assert_true } from "@soyuz/mock" )
fn main() {
    assert_true(true, "ok")
}
`
	if errs := checkWithStdlib(t, stdlibDir, src); len(errs) > 0 {
		t.Fatalf("erros inesperados com import single name: %v", errs)
	}
}

// TestStdlibBothImportForms verifica os dois imports juntos: module e named.
func TestStdlibBothImportForms(t *testing.T) {
	stdlibDir := t.TempDir()
	writeFile(t, stdlibDir, "mock.sy", `
extern fn soyuz_print_str(s: String)
pub fn assert_true(cond: Bool, name: String) {
    soyuz_print_str(name)
}
`)
	src := `
import (
    "@soyuz/mock"
    { assert_true } from "@soyuz/mock"
)
fn main() {
    assert_true(true, "direto")
    mock.assert_true(true, "qualificado")
}
`
	if errs := checkWithStdlib(t, stdlibDir, src); len(errs) > 0 {
		t.Fatalf("erros com import duplo: %v", errs)
	}
}

// TestStdlibNestedPath verifica que std/collections/list.sy é acessível via "@soyuz/collections/list".
func TestStdlibNestedPath(t *testing.T) {
	stdlibDir := t.TempDir()
	writeFile(t, stdlibDir, filepath.Join("collections", "list.sy"), `
pub fn length() -> Int = 0
`)
	src := `
import ( "@soyuz/collections/list" )
fn main() {
    list.length()
}
`
	if errs := checkWithStdlib(t, stdlibDir, src); len(errs) > 0 {
		t.Fatalf("erros com módulo aninhado: %v", errs)
	}
}

// TestStdlibFormatterImport verifica que o parser aceita a nova sintaxe.
func TestStdlibFormatterImport(t *testing.T) {
	stdlibDir := t.TempDir()
	writeFile(t, stdlibDir, "mock.sy", `pub fn assert_true(cond: Bool, name: String) {}`)

	cases := []string{
		`import ( "@soyuz/mock" )`,
		`import ( { assert_true } from "@soyuz/mock" )`,
		`import ( { assert_true, assert_eq } from "@soyuz/mock" )`,
	}

	for _, src := range cases {
		tokens := lexer.Tokenize(src)
		p := parser.New(tokens)
		p.Parse()
		if p.HasErrors() {
			t.Fatalf("parse error for %q: %v", src, p.Errors())
		}
	}

	entry := filepath.Join(t.TempDir(), "main.sy")
	resolver := module.NewResolverWithStdlib(entry, stdlibDir)
	_ = resolver
}
