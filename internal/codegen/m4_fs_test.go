package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

const fsPreamble = `
interface Error {
  fn message() -> String
  fn code() -> Int
}

extern fn soyuz_fs_read_file(path: String) -> String
extern fn soyuz_fs_write_file(path: String, content: String) -> Int
extern fn soyuz_fs_exists(path: String) -> Bool
extern fn soyuz_fs_is_dir(path: String) -> Bool
extern fn soyuz_fs_has_error() -> Bool
extern fn soyuz_fs_last_error() -> String

pub class IOError : Error {
  pub val msg: String
  pub val errCode: Int
  pub fn message(self) -> String = self.msg
  pub fn code(self) -> Int = self.errCode
}

pub fn readFile(path: String) -> Result[String] {
  val content = soyuz_fs_read_file(path)
  if soyuz_fs_has_error() {
    return Err(IOError { msg: soyuz_fs_last_error(), errCode: 1 })
  }
  return Ok(content)
}

pub fn writeFile(path: String, content: String) -> Result[Int] {
  val ok = soyuz_fs_write_file(path, content)
  if ok == 0 {
    return Err(IOError { msg: soyuz_fs_last_error(), errCode: 1 })
  }
  return Ok(1)
}

pub fn exists(path: String) -> Bool = soyuz_fs_exists(path)
pub fn isDir(path: String) -> Bool = soyuz_fs_is_dir(path)
`

func fsIR(t *testing.T, src string) string {
	t.Helper()
	full := fsPreamble + src
	tokens := lexer.Tokenize(full)
	prog := parser.New(tokens).Parse()
	res := checker.New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
	mod, err := New(res).Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	return mod.String()
}

func TestFsReadFileIR(t *testing.T) {
	src := `fn main() -> Result[String] { return readFile("/tmp/test.txt") }`
	ir := fsIR(t, src)
	if !strings.Contains(ir, "@soyuz_fs_read_file") {
		t.Fatalf("esperado @soyuz_fs_read_file no IR, obteve:\n%s", ir)
	}
	if !strings.Contains(ir, "@soyuz_fs_has_error") {
		t.Fatalf("esperado @soyuz_fs_has_error no IR, obteve:\n%s", ir)
	}
}

func TestFsWriteFileIR(t *testing.T) {
	src := `fn main() -> Result[Int] { return writeFile("/tmp/test.txt", "hello") }`
	ir := fsIR(t, src)
	if !strings.Contains(ir, "@soyuz_fs_write_file") {
		t.Fatalf("esperado @soyuz_fs_write_file no IR, obteve:\n%s", ir)
	}
}

func TestFsExistsIR(t *testing.T) {
	src := `fn main() -> Bool { return exists("/tmp") }`
	ir := fsIR(t, src)
	if !strings.Contains(ir, "@soyuz_fs_exists") {
		t.Fatalf("esperado @soyuz_fs_exists no IR, obteve:\n%s", ir)
	}
}

func TestFsIsDirIR(t *testing.T) {
	src := `fn main() -> Bool { return isDir("/tmp") }`
	ir := fsIR(t, src)
	if !strings.Contains(ir, "@soyuz_fs_is_dir") {
		t.Fatalf("esperado @soyuz_fs_is_dir no IR, obteve:\n%s", ir)
	}
}

func TestFsReadDirPatternMatchIR(t *testing.T) {
	// Regression test: pattern-matching Ok(list) on Result[List[String]] used to panic
	// with "invalid gep source type; expected pointer, got *types.IntType" because
	// the built-in Result enum's Ok variant had no pre-registered field types.
	src := `
extern fn soyuz_fs_read_dir(path: String) -> List[String]
extern fn soyuz_fs_has_error() -> Bool
extern fn soyuz_fs_last_error() -> String

pub fn readDir(path: String) -> Result[List[String]] {
  val entries = soyuz_fs_read_dir(path)
  if soyuz_fs_has_error() {
    return Err(IOError { msg: soyuz_fs_last_error(), errCode: 1 })
  }
  return Ok(entries)
}

fn main() {
  val entries = readDir("/tmp")
  match entries {
    Ok(list) => print(list.size())
    Err(_)   => print("error")
  }
}
`
	ir := fsIR(t, src)
	if !strings.Contains(ir, "@soyuz_fs_read_dir") {
		t.Fatalf("esperado @soyuz_fs_read_dir no IR, obteve:\n%s", ir)
	}
	// The list.size() call should compile to a GEP into SoyuzList (not an integer load)
	if !strings.Contains(ir, "SoyuzList") {
		t.Fatalf("esperado referência a SoyuzList no IR, obteve:\n%s", ir)
	}
}
