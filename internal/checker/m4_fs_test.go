package checker

import (
	"testing"

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

func checkFS(src string) []TypeError {
	tokens := lexer.Tokenize(fsPreamble + src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog).Errors
}

func TestFsReadFileReturnsResult(t *testing.T) {
	src := `fn main() -> Result[String] { return readFile("/tmp/test.txt") }`
	if errs := checkFS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestFsWriteFileReturnsResult(t *testing.T) {
	src := `fn main() -> Result[Int] { return writeFile("/tmp/test.txt", "hello") }`
	if errs := checkFS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestFsExistsReturnsBool(t *testing.T) {
	src := `fn main() -> Bool { return exists("/tmp") }`
	if errs := checkFS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestFsIsDirReturnsBool(t *testing.T) {
	src := `fn main() -> Bool { return isDir("/tmp") }`
	if errs := checkFS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestIOErrorImplementsError(t *testing.T) {
	src := `fn main() -> Result[String] {
    return Err(IOError { msg: "falhou", errCode: 1 })
}`
	if errs := checkFS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestFsMatchReadFileResult(t *testing.T) {
	src := `fn main() -> String {
    val r = readFile("/tmp/test.txt")
    return match r {
        Ok(content) => content
        Err(e) => "erro"
    }
}`
	if errs := checkFS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}
