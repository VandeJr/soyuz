package checker

import (
	"testing"

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

pub fn getenv(name: String) -> String = soyuz_os_getenv(name)
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

func checkOS(src string) []TypeError {
	tokens := lexer.Tokenize(osPreamble + src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog).Errors
}

func TestOsGetenvReturnsString(t *testing.T) {
	src := `fn main() -> String { return getenv("PATH") }`
	if errs := checkOS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestOsHasEnvReturnsBool(t *testing.T) {
	src := `fn main() -> Bool { return hasEnv("HOME") }`
	if errs := checkOS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestOsArgsReturnsList(t *testing.T) {
	src := `fn main() -> List[String] { return args() }`
	if errs := checkOS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestOsExecReturnsResult(t *testing.T) {
	src := `fn main() -> Result[String] { return exec("echo hello") }`
	if errs := checkOS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestOSErrorImplementsError(t *testing.T) {
	src := `fn main() -> Result[String] {
    return Err(OSError { msg: "falhou" })
}`
	if errs := checkOS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}

func TestOsExecMatchResult(t *testing.T) {
	src := `fn main() -> String {
    val r = exec("echo hello")
    return match r {
        Ok(output) => output
        Err(e) => "erro"
    }
}`
	if errs := checkOS(src); len(errs) > 0 {
		t.Fatalf("erros inesperados: %v", errs)
	}
}
