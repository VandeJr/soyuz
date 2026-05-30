package codegen

import (
	"strings"
	"testing"
)

func TestFuncParamClosureCall(t *testing.T) {
	src := `
fn dobrar(x: Int) -> Int = x * 2
fn aplicar(f: (Int) -> Int, x: Int) -> Int = f(x)
fn main() -> Int {
    return aplicar(dobrar, 5)
}`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "__shim_dobrar") {
		t.Errorf("esperado shim __shim_dobrar no IR")
	}
	if !strings.Contains(ir, "SoyuzClosure") {
		t.Errorf("esperado alocação SoyuzClosure no IR")
	}
}

func TestListMapNamedFunc(t *testing.T) {
	src := `
import @soyuz/prelude
fn dobrar(x: Int) -> Int = x * 2
fn main() -> Int {
    val ds = [1, 2, 3].map(dobrar)
    return ds.get(2)
}`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "__shim_dobrar") {
		t.Errorf("esperado shim para dobrar em map")
	}
}
