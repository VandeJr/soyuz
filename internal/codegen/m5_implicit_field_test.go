package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// Regression test: class fields with no explicit type annotation (inferred from default)
// used to produce i64 in the struct layout even when the value was i8* (String) or a
// pointer-to-enum, causing a "store operands are not compatible" panic in emitRecordAlloc.
func TestClassImplicitFieldTypes(t *testing.T) {
	src := `
enum Status {
	Inativo
	Ativo { conexoes: Int }
}

class Servidor {
	var porta = 8080
	val host = "localhost"
	var status: Status = Status.Inativo

	pub fn iniciar(self) {
		self.status = Status.Ativo(1)
		print("ok")
	}
}

fn main() -> Int {
	val app = Servidor {}
	app.iniciar()
	return 0
}
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	c := checker.New()
	res := c.Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}

	g := New(res)
	mod, err := g.Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}

	ir := mod.String()

	// Struct for Servidor should have i8* for host, not i64.
	if !strings.Contains(ir, "i8*") {
		t.Error("esperado i8* no IR para campo host (String)")
	}
	// The main function should call Servidor_iniciar.
	if !strings.Contains(ir, "Servidor_iniciar") {
		t.Error("esperado chamada a Servidor_iniciar no IR")
	}
}
