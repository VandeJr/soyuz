package codegen

import (
	"strings"
	"testing"
)

// ── M-14: Arc[T] codegen tests ───────────────────────────────────────────────

// TestArcNewEmitsSrtArcNew verifies that Arc.new(val) emits a call to srt_arc_new.
func TestArcNewEmitsSrtArcNew(t *testing.T) {
	src := `
fn main() {
  val a = Arc.new(42)
  a.refcount()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_arc_new") {
		t.Error("esperado srt_arc_new no IR")
	}
}

// TestArcCloneEmitsSrtArcClone verifies that arc.clone() emits srt_arc_clone.
func TestArcCloneEmitsSrtArcClone(t *testing.T) {
	src := `
fn main() {
  val a = Arc.new(10)
  val b = a.clone()
  b.refcount()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_arc_clone") {
		t.Error("esperado srt_arc_clone no IR")
	}
}

// TestArcGetEmitsSrtArcGet verifies that arc.get() emits srt_arc_get.
func TestArcGetEmitsSrtArcGet(t *testing.T) {
	src := `
fn main() -> Int {
  val a = Arc.new(99)
  return a.get()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_arc_get") {
		t.Error("esperado srt_arc_get no IR")
	}
}

// TestArcRefcountEmitsSrtArcRefcount verifies that arc.refcount() emits srt_arc_refcount.
func TestArcRefcountEmitsSrtArcRefcount(t *testing.T) {
	src := `
fn main() -> Int {
  val a = Arc.new(1)
  return a.refcount()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_arc_refcount") {
		t.Error("esperado srt_arc_refcount no IR")
	}
}

// TestArcReleaseEmittedOnScopeExit verifies that srt_arc_release is emitted when
// an Arc variable goes out of scope (destructor tracking via arcVarStack).
func TestArcReleaseEmittedOnScopeExit(t *testing.T) {
	src := `
fn main() {
  val a = Arc.new(7)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_arc_release") {
		t.Error("esperado srt_arc_release no IR (drop no fim do escopo)")
	}
}

// TestArcReleaseOnBothVarsWhenCloned verifies that both the original and the clone
// get srt_arc_release emitted at scope exit.
func TestArcReleaseOnBothVarsWhenCloned(t *testing.T) {
	src := `
fn main() {
  val a = Arc.new(3)
  val b = a.clone()
}
`
	ir := compileTask(t, src)
	// Count occurrences of srt_arc_release in the IR.
	count := strings.Count(ir, "srt_arc_release")
	if count < 2 {
		t.Errorf("esperado pelo menos 2 chamadas a srt_arc_release (a e b), obtido %d\nIR:\n%s", count, ir)
	}
}

// TestArcDeclaresBuiltins verifies that all Arc builtins are declared in the module.
func TestArcDeclaresBuiltins(t *testing.T) {
	src := `fn main() {}`
	ir := compileTask(t, src)
	builtins := []string{
		"srt_arc_new",
		"srt_arc_clone",
		"srt_arc_release",
		"srt_arc_get",
		"srt_arc_refcount",
	}
	for _, b := range builtins {
		if !strings.Contains(ir, b) {
			t.Errorf("esperado builtin %q declarado no módulo IR", b)
		}
	}
}
