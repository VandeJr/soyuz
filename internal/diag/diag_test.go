package diag

import (
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func TestFromParseErrorsSpan(t *testing.T) {
	errs := []parser.ParseError{{
		Position: lexer.Position{Line: 1, Column: 5},
		End:      lexer.Position{Line: 1, Column: 9},
		Message:  "token inesperado",
	}}
	diags := FromParseErrors("main.sy", errs)
	if len(diags) != 1 {
		t.Fatalf("esperado 1 diagnóstico, obtido %d", len(diags))
	}
	if diags[0].Code != "E0100" {
		t.Errorf("esperado código E0100, obtido %s", diags[0].Code)
	}
	if diags[0].End.Column != 9 {
		t.Errorf("esperado fim na coluna 9, obtido %d", diags[0].End.Column)
	}
}

func TestFromTypeErrorsCode(t *testing.T) {
	errs := []checker.TypeError{{
		Pos:     lexer.Position{Line: 2, Column: 1},
		End:     lexer.Position{Line: 2, Column: 5},
		Code:    "E0200",
		Message: "tipo incompatível",
	}}
	diags := FromTypeErrors(errs)
	if diags[0].Code != "E0200" {
		t.Errorf("esperado E0200, obtido %s", diags[0].Code)
	}
}

func TestMergeDiagnostics(t *testing.T) {
	a := []Diagnostic{{Message: "a"}}
	b := []Diagnostic{{Message: "b"}}
	merged := Merge(a, b)
	if len(merged) != 2 {
		t.Fatalf("esperado 2 diagnósticos mesclados, obtido %d", len(merged))
	}
}
