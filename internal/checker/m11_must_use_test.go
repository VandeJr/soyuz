package checker

import (
	"strings"
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func checkMustUse(src string) *CheckResult {
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog)
}

func hasWarning(warns []TypeWarning, substr string) bool {
	for _, w := range warns {
		if strings.Contains(w.Message, substr) {
			return true
		}
	}
	return false
}

// W0300 — task em posição de statement: deve gerar warning.
func TestUnconsumedTaskWarning(t *testing.T) {
	src := `
fn doWork(n: Int) -> Int = n * 2

fn run() {
  task doWork(5)
}
`
	result := checkMustUse(src)
	if len(result.Errors) > 0 {
		t.Fatalf("não esperava erros, obtido: %v", result.Errors)
	}
	if !hasWarning(result.Warnings, "Task não consumida") {
		t.Errorf("esperava warning W0300, obtido: %v", result.Warnings)
	}
	if len(result.Warnings) != 1 {
		t.Errorf("esperava exatamente 1 warning, obtido %d: %v", len(result.Warnings), result.Warnings)
	}
}

// task top-level sem assignment: warning.
func TestUnconsumedTaskTopLevel(t *testing.T) {
	src := `
fn doWork() -> Int = 42
task doWork()
`
	result := checkMustUse(src)
	if len(result.Errors) > 0 {
		t.Fatalf("não esperava erros, obtido: %v", result.Errors)
	}
	if !hasWarning(result.Warnings, "Task não consumida") {
		t.Errorf("esperava warning W0300 no top-level, obtido: %v", result.Warnings)
	}
}

// val t = task ...: não deve gerar warning (task consumida via binding).
func TestConsumedTaskNoWarning(t *testing.T) {
	src := `
fn doWork(n: Int) -> Int = n * 2

fn run() {
  val t = task doWork(5)
  val result = t.await()
}
`
	result := checkMustUse(src)
	if len(result.Errors) > 0 {
		t.Fatalf("não esperava erros, obtido: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("não esperava warnings, obtido: %v", result.Warnings)
	}
}

// .detach() retorna Unit → não é Task → sem warning.
func TestDetachedTaskNoWarning(t *testing.T) {
	src := `
fn doWork(n: Int) -> Int = n * 2

fn run() {
  val t = task doWork(5)
  t.detach()
}
`
	result := checkMustUse(src)
	if len(result.Errors) > 0 {
		t.Fatalf("não esperava erros, obtido: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("não esperava warnings, obtido: %v", result.Warnings)
	}
}

// val _ = task ... silencia explicitamente (é VarDecl, não ExprStmt).
func TestUnderscoreTaskNoWarning(t *testing.T) {
	src := `
fn doWork(n: Int) -> Int = n * 2

fn run() {
  val _ = task doWork(5)
}
`
	result := checkMustUse(src)
	if len(result.Errors) > 0 {
		t.Fatalf("não esperava erros, obtido: %v", result.Errors)
	}
	if len(result.Warnings) > 0 {
		t.Errorf("val _ = task não deve gerar warning, obtido: %v", result.Warnings)
	}
}

// Código W0300 correto no warning.
func TestWarningCode(t *testing.T) {
	src := `
fn doWork() -> Int = 1

fn run() {
  task doWork()
}
`
	result := checkMustUse(src)
	if len(result.Warnings) == 0 {
		t.Fatal("esperava warning W0300")
	}
	if result.Warnings[0].Code != "W0300" {
		t.Errorf("código esperado W0300, obtido %s", result.Warnings[0].Code)
	}
}
