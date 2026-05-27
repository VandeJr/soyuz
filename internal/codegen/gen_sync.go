package codegen

import (
	"fmt"

	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// isSyncGuardType returns true when t is MutexGuard[T], ReadGuard[T], or WriteGuard[T].
func isSyncGuardType(t checker.Type) (guardKind string, innerType checker.Type, ok bool) {
	st, isST := t.(*checker.SpecializedType)
	if !isST {
		return "", nil, false
	}
	ct, isCT := st.Base.(*checker.ClassType)
	if !isCT {
		return "", nil, false
	}
	switch ct.Name {
	case "MutexGuard":
		if len(st.Params) > 0 {
			return "mutex", st.Params[0], true
		}
	case "ReadGuard":
		if len(st.Params) > 0 {
			return "rwlock", st.Params[0], true
		}
	case "WriteGuard":
		if len(st.Params) > 0 {
			return "rwlock", st.Params[0], true
		}
	}
	return "", nil, false
}

// coerceToI64 converts a value to i64 for uniform sync storage.
// Int: identity; Bool: zero-extend; Float: bitcast.
func (g *Generator) coerceToI64(v value.Value) value.Value {
	t := v.Type()
	if t.Equal(types.I64) {
		return v
	}
	if t.Equal(types.I1) {
		return g.current.NewZExt(v, types.I64)
	}
	if t.Equal(types.Double) {
		return g.current.NewBitCast(v, types.I64)
	}
	if t.Equal(types.I32) {
		return g.current.NewZExt(v, types.I64)
	}
	// fallback: bitcast to i64 if same size, else return as-is
	return v
}

// coerceFromI64 converts an i64 back to the target type T.
func (g *Generator) coerceFromI64(v value.Value, t checker.Type) value.Value {
	target := g.mapTypeToLLVM(t)
	if target.Equal(types.I64) {
		return v
	}
	if target.Equal(types.I1) {
		return g.current.NewTrunc(v, types.I1)
	}
	if target.Equal(types.Double) {
		return g.current.NewBitCast(v, types.Double)
	}
	if target.Equal(types.I32) {
		return g.current.NewTrunc(v, types.I32)
	}
	return v
}

// isSyncGuardValueAccess returns true when n is `guard.value` for any sync guard type.
func (g *Generator) isSyncGuardValueAccess(n *parser.MemberExpr) (kind string, innerT checker.Type, ok bool) {
	if n.Property != "value" {
		return "", nil, false
	}
	objType := g.check.NodeTypes[n.Object]
	kind, innerT, ok = isSyncGuardType(objType)
	return kind, innerT, ok
}

// generateSyncGuardRead emits a read of guard.value → T.
func (g *Generator) generateSyncGuardRead(n *parser.MemberExpr) (value.Value, error) {
	guardKind, innerT, _ := g.isSyncGuardValueAccess(n)

	obj, err := g.generateExpr(n.Object)
	if err != nil {
		return nil, err
	}
	var getFnName string
	if guardKind == "mutex" {
		getFnName = "srt_mutex_guard_get"
	} else {
		getFnName = "srt_rwlock_guard_get"
	}
	getFn := g.findFunc(getFnName)
	if getFn == nil {
		return nil, fmt.Errorf("runtime function %s not found", getFnName)
	}
	raw := g.current.NewCall(getFn, obj)
	return g.coerceFromI64(raw, innerT), nil
}

// generateSyncGuardWrite emits guard.value = val (after the val has been generated).
func (g *Generator) generateSyncGuardWrite(n *parser.MemberExpr, val value.Value) error {
	guardKind, _, _ := g.isSyncGuardValueAccess(n)

	obj, err := g.generateExpr(n.Object)
	if err != nil {
		return err
	}
	raw := g.coerceToI64(val)
	var setFnName string
	if guardKind == "mutex" {
		setFnName = "srt_mutex_guard_set"
	} else {
		setFnName = "srt_rwlock_guard_set"
	}
	setFn := g.findFunc(setFnName)
	if setFn == nil {
		return fmt.Errorf("runtime function %s not found", setFnName)
	}
	g.current.NewCall(setFn, obj, raw)
	return nil
}

// generateMutexNew emits Mutex.new(initialValue) → i8* (srt_mutex_t*).
func (g *Generator) generateMutexNew(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("Mutex.new requer exatamente 1 argumento")
	}
	init, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	raw := g.coerceToI64(init)
	fn := g.findFunc("srt_mutex_new")
	return g.current.NewCall(fn, raw), nil
}

// generateMutexLock emits mutex.lock() → i8* (srt_mutex_guard_t*).
func (g *Generator) generateMutexLock(obj parser.Node) (value.Value, error) {
	mx, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_mutex_lock")
	return g.current.NewCall(fn, mx), nil
}

// generateRwLockNew emits RwLock.new(initialValue) → i8* (srt_rwlock_t*).
func (g *Generator) generateRwLockNew(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("RwLock.new requer exatamente 1 argumento")
	}
	init, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	raw := g.coerceToI64(init)
	fn := g.findFunc("srt_rwlock_new")
	return g.current.NewCall(fn, raw), nil
}

// generateRwLockRead emits rwlock.read() → i8* (srt_rw_guard_t*, is_write=0).
func (g *Generator) generateRwLockRead(obj parser.Node) (value.Value, error) {
	rw, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_rwlock_read")
	return g.current.NewCall(fn, rw), nil
}

// generateRwLockWrite emits rwlock.write() → i8* (srt_rw_guard_t*, is_write=1).
func (g *Generator) generateRwLockWrite(obj parser.Node) (value.Value, error) {
	rw, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_rwlock_write")
	return g.current.NewCall(fn, rw), nil
}

// generateAtomicNew emits Atomic.new(initialValue) → i8* (srt_atomic_t*).
func (g *Generator) generateAtomicNew(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("Atomic.new requer exatamente 1 argumento")
	}
	init, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	raw := g.coerceToI64(init)
	fn := g.findFunc("srt_atomic_new")
	return g.current.NewCall(fn, raw), nil
}

// generateAtomicLoad emits atomic.load() → T.
func (g *Generator) generateAtomicLoad(obj parser.Node, st *checker.SpecializedType) (value.Value, error) {
	a, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_atomic_load")
	raw := g.current.NewCall(fn, a)
	if len(st.Params) > 0 {
		return g.coerceFromI64(raw, st.Params[0]), nil
	}
	return raw, nil
}

// generateAtomicStore emits atomic.store(val) → void.
func (g *Generator) generateAtomicStore(obj parser.Node, n *parser.CallExpr) (value.Value, error) {
	a, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("Atomic.store requer exatamente 1 argumento")
	}
	val, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_atomic_store")
	g.current.NewCall(fn, a, g.coerceToI64(val))
	return constant.NewInt(types.I64, 0), nil
}

// generateAtomicAdd emits atomic.add(delta) → T (the old value).
func (g *Generator) generateAtomicAdd(obj parser.Node, n *parser.CallExpr, st *checker.SpecializedType) (value.Value, error) {
	a, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("Atomic.add requer exatamente 1 argumento")
	}
	delta, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_atomic_add")
	raw := g.current.NewCall(fn, a, g.coerceToI64(delta))
	if len(st.Params) > 0 {
		return g.coerceFromI64(raw, st.Params[0]), nil
	}
	return raw, nil
}

// generateAtomicCas emits atomic.compareAndSwap(expected, desired) → Bool.
func (g *Generator) generateAtomicCas(obj parser.Node, n *parser.CallExpr) (value.Value, error) {
	a, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	if len(n.Args) != 2 {
		return nil, fmt.Errorf("Atomic.compareAndSwap requer exatamente 2 argumentos")
	}
	exp, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	des, err := g.generateExpr(n.Args[1])
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_atomic_cas")
	raw := g.current.NewCall(fn, a, g.coerceToI64(exp), g.coerceToI64(des))
	// srt_atomic_cas returns i64 (1=success, 0=fail); convert to i1 Bool.
	return g.current.NewTrunc(raw, types.I1), nil
}
