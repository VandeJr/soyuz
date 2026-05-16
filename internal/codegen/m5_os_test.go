package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

const osPreamble = `
interface Error {
  fn message() -> String
  fn code() -> Int
}

extern fn soyuz_os_getenv(name: String) -> String
extern fn soyuz_os_has_env(name: String) -> Bool
extern fn soyuz_os_args() -> List[String]
extern fn soyuz_os_exec(cmd: String) -> String
extern fn soyuz_os_has_error() -> Bool
extern fn soyuz_os_last_error() -> String

pub class OSError : Error {
  pub val msg: String
  pub val errCode: Int = 1
  pub fn message(self) -> String = self.msg
  pub fn code(self) -> Int = self.errCode
}

pub fn getenv(name: String) -> Result[String] {
  val value = soyuz_os_getenv(name)
  if soyuz_os_has_error() {
    return Err(OSError { msg: soyuz_os_last_error(), errCode: 1 })
  }
  return Ok(value)
}
pub fn hasEnv(name: String) -> Bool = soyuz_os_has_env(name)
pub fn args() -> List[String] = soyuz_os_args()

pub fn exec(cmd: String) -> Result[String] {
  val output = soyuz_os_exec(cmd)
  if soyuz_os_has_error() {
    return Err(OSError { msg: soyuz_os_last_error() })
  }
  return Ok(output)
}
`

func osIR(t *testing.T, src string) string {
	t.Helper()
	full := osPreamble + src
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

func TestOsGetenvIR(t *testing.T) {
	src := `fn main() -> Result[String] { return getenv("PATH") }`
	ir := osIR(t, src)
	if !strings.Contains(ir, "@soyuz_os_getenv") {
		t.Fatalf("esperado @soyuz_os_getenv no IR, obteve:\n%s", ir)
	}
	if !strings.Contains(ir, "@soyuz_os_has_error") {
		t.Fatalf("esperado @soyuz_os_has_error no IR, obteve:\n%s", ir)
	}
}

func TestOsHasEnvIR(t *testing.T) {
	src := `fn main() -> Bool { return hasEnv("HOME") }`
	ir := osIR(t, src)
	if !strings.Contains(ir, "@soyuz_os_has_env") {
		t.Fatalf("esperado @soyuz_os_has_env no IR, obteve:\n%s", ir)
	}
}

func TestOsArgsIR(t *testing.T) {
	src := `fn main() -> List[String] { return args() }`
	ir := osIR(t, src)
	if !strings.Contains(ir, "@soyuz_os_args") {
		t.Fatalf("esperado @soyuz_os_args no IR, obteve:\n%s", ir)
	}
}

func TestOsExecIR(t *testing.T) {
	src := `fn main() -> Result[String] { return exec("echo hello") }`
	ir := osIR(t, src)
	if !strings.Contains(ir, "@soyuz_os_exec") {
		t.Fatalf("esperado @soyuz_os_exec no IR, obteve:\n%s", ir)
	}
	if !strings.Contains(ir, "@soyuz_os_has_error") {
		t.Fatalf("esperado @soyuz_os_has_error no IR, obteve:\n%s", ir)
	}
}
