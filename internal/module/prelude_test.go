package module_test

import (
	"os"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
)

func TestPreludeAutoImport(t *testing.T) {
	stdlibDir := t.TempDir()
	// Minimal stdlib stubs required by prelude.sy
	writeFile(t, stdlibDir, "collections.sy", `
extern fn soyuz_range(from: Int, to: Int) -> List[Int]
pub fn range(from: Int, to: Int) -> List[Int] = soyuz_range(from, to)
pub fn rangeInclusive(from: Int, to: Int) -> List[Int] = soyuz_range(from, to)
pub fn rangeStep(from: Int, to: Int, step: Int) -> List[Int] = soyuz_range(from, to)
`)
	writeFile(t, stdlibDir, "error.sy", `
pub interface Error { fn message() -> String fn code() -> Int }
pub class NoneError : Error { pub val msg: String pub fn message(self) -> String = self.msg pub fn code(self) -> Int = 1 }
pub fn noneError(msg: String) -> Error = NoneError { msg: msg }
`)
	writeFile(t, stdlibDir, "fs.sy", `
import ( { Error } from "@soyuz/error" )
extern fn soyuz_fs_read_file(path: String) -> String
extern fn soyuz_fs_write_file(path: String, content: String) -> Int
extern fn soyuz_fs_exists(path: String) -> Bool
extern fn soyuz_fs_has_error() -> Bool
extern fn soyuz_fs_last_error() -> String
pub class IOError : Error { pub val msg: String pub val errCode: Int pub fn message(self) -> String = self.msg pub fn code(self) -> Int = self.errCode }
pub fn readFile(path: String) -> Result[String] = Ok(soyuz_fs_read_file(path))
pub fn writeFile(path: String, content: String) -> Result[Int] = Ok(1)
pub fn exists(path: String) -> Bool = soyuz_fs_exists(path)
`)
	writeFile(t, stdlibDir, "os.sy", `
import ( { Error } from "@soyuz/error" )
extern fn soyuz_os_getenv(name: String) -> String
extern fn soyuz_os_has_env(name: String) -> Bool
extern fn soyuz_os_args() -> List[String]
extern fn soyuz_os_has_error() -> Bool
extern fn soyuz_os_last_error() -> String
pub class OSError : Error { pub val msg: String pub val errCode: Int = 1 pub fn message(self) -> String = self.msg pub fn code(self) -> Int = self.errCode }
pub fn getenv(name: String) -> Result[String] = Ok(soyuz_os_getenv(name))
pub fn args() -> List[String] = soyuz_os_args()
pub fn hasEnv(name: String) -> Bool = soyuz_os_has_env(name)
`)
	writeFile(t, stdlibDir, "prelude.sy", `
import ( "@soyuz/collections" )
import ( "@soyuz/fs" )
import ( "@soyuz/os" )
import ( "@soyuz/error" )
pub fn range(from: Int, to: Int) -> List[Int] = collections.range(from, to)
pub fn exists(path: String) -> Bool = fs.exists(path)
pub fn args() -> List[String] = os.args()
`)

	dir := t.TempDir()
	entry := writeFile(t, dir, "main.sy", `
fn main() {
	val xs = range(0, 3)
	print(xs.size())
	print(exists("x"))
	print(args().size())
}
`)

	resolver := module.NewResolverWithStdlib(entry, stdlibDir)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("esperado prelude + main, obtido %d arquivos", len(files))
	}

	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	for _, file := range files {
		data, _ := os.ReadFile(file)
		prog := parser.New(lexer.Tokenize(string(data))).Parse()
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

	c := checker.New()
	c.SetNodeFiles(nodeFile)
	if preludeFiles, err := module.ResolvePrelude(resolver); err == nil {
		c.SetPreludeFiles(preludeFiles)
	}
	result := c.Check(&parser.Program{Body: allNodes})
	if len(result.Errors) > 0 {
		t.Fatalf("prelude auto-import não deve gerar erros: %v", result.Errors)
	}
}
