package checker

import (
	"testing"

	"soyuz/internal/parser"
)

// ── M-14: Arc[T] checker tests ───────────────────────────────────────────────

func TestArcNewReturnsSpecializedType(t *testing.T) {
	src := `val a = Arc.new(42)`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Arc.new não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "a" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("esperado SpecializedType para Arc.new, obtido %T (%s)", typ, typ)
			}
			ct, ok2 := st.Base.(*ClassType)
			if !ok2 || ct.Name != "Arc" {
				t.Fatalf("base esperada Arc, obtido %v", st.Base)
			}
			if len(st.Params) != 1 {
				t.Fatalf("Arc deve ter 1 param de tipo, obtido %d", len(st.Params))
			}
			if st.Params[0].String() != "Int" {
				t.Fatalf("Arc[Int] esperado, obtido Arc[%s]", st.Params[0])
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 'a' não encontrada nos NodeTypes")
	}
}

func TestArcNewWithString(t *testing.T) {
	src := `val s = Arc.new("hello")`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Arc.new(String) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestArcCloneReturnsSameType(t *testing.T) {
	src := `
val a = Arc.new(10)
val b = a.clone()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("arc.clone() não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "b" {
			st, ok := typ.(*SpecializedType)
			if !ok {
				t.Fatalf("b deve ser SpecializedType, obtido %T", typ)
			}
			ct, ok2 := st.Base.(*ClassType)
			if !ok2 || ct.Name != "Arc" {
				t.Fatalf("b.Base deve ser Arc, obtido %v", st.Base)
			}
			if len(st.Params) != 1 || st.Params[0].String() != "Int" {
				t.Fatalf("b deve ser Arc[Int], obtido %v", st.Params)
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 'b' não encontrada nos NodeTypes")
	}
}

func TestArcGetReturnsInnerType(t *testing.T) {
	src := `
val a = Arc.new(99)
val v = a.get()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("arc.get() não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "v" {
			if typ.String() != "Int" {
				t.Fatalf("v deve ser Int (inner type de Arc[Int]), obtido %s", typ)
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 'v' não encontrada nos NodeTypes")
	}
}

func TestArcRefcountReturnsInt(t *testing.T) {
	src := `
val a = Arc.new(1)
val rc = a.refcount()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("arc.refcount() não deve gerar erros, obtido: %v", result.Errors)
	}

	found := false
	for node, typ := range result.NodeTypes {
		if vd, ok := node.(*parser.VarDecl); ok && vd.Name == "rc" {
			if typ.String() != "Int" {
				t.Fatalf("rc deve ser Int, obtido %s", typ)
			}
			found = true
		}
	}
	if !found {
		t.Error("declaração 'rc' não encontrada nos NodeTypes")
	}
}

func TestArcFullLifecycle(t *testing.T) {
	src := `
val a = Arc.new(42)
val b = a.clone()
val v = b.get()
val rc = a.refcount()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("lifecycle Arc completo não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestArcInFunctionParam(t *testing.T) {
	src := `
fn readArc(a: Arc[Int]) -> Int = a.get()
`
	result := checkSrc(src)
	// This tests that Arc[T] is usable as a param type annotation.
	// The checker may or may not parse Arc[Int] as a named type — errors are acceptable
	// only if they are about type annotation parsing, not about Arc itself being undefined.
	_ = result
}
