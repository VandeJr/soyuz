package codegen

// gen_arc.go — M-14: Arc[T] codegen (EBR-managed shared ownership)
//
// Arc[T] maps to i8* in LLVM IR.
// Runtime functions: srt_arc_new, srt_arc_clone, srt_arc_release, srt_arc_get, srt_arc_refcount
// Values coerced to/from i64 via coerceToI64 / coerceFromI64 (from gen_sync.go).

import (
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir/value"
)

// generateArcNew emits Arc.new(val: T) → i8*
func (g *Generator) generateArcNew(n *parser.CallExpr) (value.Value, error) {
	arg, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	argI64 := g.coerceToI64(arg)
	fn := g.findFunc("srt_arc_new")
	return g.current.NewCall(fn, argI64), nil
}

// generateArcClone emits arc.clone() → i8*
func (g *Generator) generateArcClone(obj parser.Node) (value.Value, error) {
	ptr, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	ptr = g.castToI8Ptr(ptr)
	fn := g.findFunc("srt_arc_clone")
	return g.current.NewCall(fn, ptr), nil
}

// generateArcGet emits arc.get() → T (reads inner i64, coerces back to T)
func (g *Generator) generateArcGet(obj parser.Node, st *checker.SpecializedType) (value.Value, error) {
	ptr, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	ptr = g.castToI8Ptr(ptr)
	fn := g.findFunc("srt_arc_get")
	raw := g.current.NewCall(fn, ptr) // returns i64

	if len(st.Params) == 0 {
		return raw, nil
	}
	return g.coerceFromI64(raw, st.Params[0]), nil
}

// generateArcRefcount emits arc.refcount() → Int
func (g *Generator) generateArcRefcount(obj parser.Node) (value.Value, error) {
	ptr, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	ptr = g.castToI8Ptr(ptr)
	fn := g.findFunc("srt_arc_refcount")
	return g.current.NewCall(fn, ptr), nil
}
