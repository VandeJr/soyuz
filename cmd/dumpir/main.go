package main

import (
	"fmt"
	"soyuz/internal/checker"
	"soyuz/internal/codegen"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func main() {
	src := `
extern fn soyuz_os_getenv(name: String) -> String
extern fn soyuz_os_has_error() -> Bool
extern fn soyuz_os_last_error() -> String

interface Error {
  fn message() -> String
  fn code() -> Int
}

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

fn main() -> Result[String] { return getenv("PATH") }
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := checker.New().Check(prog)
	if len(res.Errors) > 0 {
		fmt.Printf("checker errors: %v\n", res.Errors)
		return
	}
	mod, err := codegen.New(res).Generate(prog)
	if err != nil {
		fmt.Printf("codegen error: %v\n", err)
		return
	}
	fmt.Println(mod.String())
}
