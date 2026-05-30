package codegen

import (
	"fmt"
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

type loopCtx struct {
	cond         *ir.Block
	after        *ir.Block
	resultAlloca value.Value // non-nil when loop returns a value via `break expr`
	resultLLVM   types.Type
}

type structInfo struct {
	typ          *types.StructType
	fieldIndices map[string]int
}

type enumInfo struct {
	typ      *types.StructType
	variants map[string]variantInfo
}

type variantInfo struct {
	tag    int
	fields []types.Type
}

type classInfo struct {
	typ          *types.StructType
	fieldIndices map[string]int
	methods      map[string][]*ir.Func // may have multiple variants (overloaded by arity)
	vtables      map[string]*ir.Global // interface name → vtable global
}

type pendingExtendMethodBody struct {
	typeName string
	selfLLVM types.Type
	fd       *parser.FuncDecl
	fn       *ir.Func
}

type pendingClassMethodBody struct {
	className string
	si        structInfo
	fd        *parser.FuncDecl
	fn        *ir.Func
}

// Generator takes a parsed Program and emits an LLVM IR Module.
type Generator struct {
	module       *ir.Module
	current      *ir.Block
	vars         map[string]value.Value
	loops        []loopCtx
	structs      map[string]structInfo
	enums        map[string]enumInfo
	classes      map[string]classInfo
	extensionMethods map[string]map[string][]*ir.Func
	interfaceDecls map[string]*parser.InterfaceDecl
	check        *checker.CheckResult
	specialized        map[string]*ir.Func
	genericDecls       map[string]*parser.FuncDecl
	genericRecordDecls map[string]*parser.RecordDecl
	genericEnumDecls   map[string]*parser.EnumDecl
	// Class method bodies deferred until all top-level function signatures are declared.
	pendingClassBodies []pendingClassMethodBody
	pendingExtendBodies []pendingExtendMethodBody
	// RC fields
	destructors map[string]*ir.Func // record name → generated destructor function
	traces      map[string]*ir.Func // record name → ORC trace function
	heapVars     map[string]bool     // which in-scope named vars hold RC-managed pointers
	scopeStack   [][]string          // stack of owned heap var names, one slice per scope level
	taskVarStack [][]string          // stack of task variable names per scope (for drop-on-exit)
	syncGuardStack [][]syncGuardEntry // stack of sync-guard variables per scope (for unlock-on-exit)
	arcVarStack  [][]string          // stack of Arc[T] variable names per scope (for srt_arc_release on exit)
	// block name deduplication within the current function
	blockNames  map[string]int
	// counter for unique task wrapper function names
	taskWrapperCounter int
	closureType *types.StructType // { i8*, i8* } — shared closure fat-pointer layout
	closureDtor *ir.Func           // releases captured env when closure is freed
	envDtorCounter int
	// SoyuzString RC-managed string type
	soyuzStringType    *types.StructType
	soyuzStringPtrType types.Type
	// Checker return type of the function currently being codegen'd (for interface coercion).
	currentReturnType checker.Type
	// Cached i8* closures wrapping top-level functions for use as first-class values.
	topLevelClosureCache map[string]value.Value
}

// New returns a new Generator.
func New(check *checker.CheckResult) *Generator {
	return &Generator{
		module:         ir.NewModule(),
		vars:           make(map[string]value.Value),
		structs:        make(map[string]structInfo),
		enums:          make(map[string]enumInfo),
		classes:        make(map[string]classInfo),
		extensionMethods: make(map[string]map[string][]*ir.Func),
		interfaceDecls: make(map[string]*parser.InterfaceDecl),
		specialized:        make(map[string]*ir.Func),
		genericDecls:       make(map[string]*parser.FuncDecl),
		genericRecordDecls: make(map[string]*parser.RecordDecl),
		genericEnumDecls:   make(map[string]*parser.EnumDecl),
		destructors:  make(map[string]*ir.Func),
		traces:      make(map[string]*ir.Func),
		heapVars:     make(map[string]bool),
		blockNames:             make(map[string]int),
		check:                  check,
		topLevelClosureCache:   make(map[string]value.Value),
	}
}

// isHeapType returns true for pointer-to-struct types (records, enums) managed by RC.
// Primitive types (Int, Float, Bool) and i8* string literals are not RC-managed.
func (g *Generator) isHeapType(t types.Type) bool {
	ptr, ok := t.(*types.PointerType)
	if !ok {
		return false
	}
	_, isStruct := ptr.ElemType.(*types.StructType)
	return isStruct
}

// emitRetain emits a call to soyuz_retain for a heap pointer.
func (g *Generator) emitRetain(v value.Value) {
	cast := g.current.NewBitCast(v, types.I8Ptr)
	g.current.NewCall(g.findFunc("soyuz_retain"), cast)
}

// emitRelease emits a call to soyuz_release for a heap pointer.
func (g *Generator) emitRelease(v value.Value) {
	cast := g.current.NewBitCast(v, types.I8Ptr)
	g.current.NewCall(g.findFunc("soyuz_release"), cast)
}

type syncGuardEntry struct {
	name     string
	unlockFn string // "srt_mutex_unlock" or "srt_rwlock_unlock"
}

// pushScope begins a new ownership scope (RC heap vars + task vars + sync guards + Arc vars).
func (g *Generator) pushScope() {
	g.scopeStack = append(g.scopeStack, nil)
	g.taskVarStack = append(g.taskVarStack, nil)
	g.syncGuardStack = append(g.syncGuardStack, nil)
	g.arcVarStack = append(g.arcVarStack, nil)
}

// ownArcVar records that the current scope owns an Arc[T] variable by name.
// At scope exit, srt_arc_release will be emitted for it.
func (g *Generator) ownArcVar(name string) {
	n := len(g.arcVarStack)
	if n > 0 {
		g.arcVarStack[n-1] = append(g.arcVarStack[n-1], name)
	}
}

// emitArcDrops emits srt_arc_release for each Arc variable in the given list.
func (g *Generator) emitArcDrops(names []string) {
	releaseFn := g.findFunc("srt_arc_release")
	if releaseFn == nil {
		return
	}
	for _, name := range names {
		if alloc, ok := g.vars[name]; ok {
			if ptr, ok2 := alloc.Type().(*types.PointerType); ok2 {
				loaded := g.current.NewLoad(ptr.ElemType, alloc)
				var handleI8 value.Value
				if loaded.Type().Equal(types.I8Ptr) {
					handleI8 = loaded
				} else {
					handleI8 = g.current.NewBitCast(loaded, types.I8Ptr)
				}
				g.current.NewCall(releaseFn, handleI8)
			}
		}
	}
}

// ownVar records that the current scope owns a heap variable by name.
func (g *Generator) ownVar(name string) {
	n := len(g.scopeStack)
	if n > 0 {
		g.scopeStack[n-1] = append(g.scopeStack[n-1], name)
	}
}

// ownTaskVar records that the current scope owns a Task[T] variable.
// At scope exit, srt_drop_task_handle will be emitted for it unless consumed.
func (g *Generator) ownTaskVar(name string) {
	n := len(g.taskVarStack)
	if n > 0 {
		g.taskVarStack[n-1] = append(g.taskVarStack[n-1], name)
	}
}

// ownSyncGuard records that the current scope owns a sync guard variable.
// At scope exit, the appropriate unlock function is emitted.
func (g *Generator) ownSyncGuard(name, unlockFn string) {
	n := len(g.syncGuardStack)
	if n > 0 {
		g.syncGuardStack[n-1] = append(g.syncGuardStack[n-1], syncGuardEntry{name: name, unlockFn: unlockFn})
	}
}

// emitSyncGuardDrops emits unlock calls for each sync guard in the list.
// Null-checks guard: if the alloca holds null, the guard was already released.
func (g *Generator) emitSyncGuardDrops(guards []syncGuardEntry) {
	for _, ge := range guards {
		unlockFn := g.findFunc(ge.unlockFn)
		if unlockFn == nil {
			continue
		}
		if alloc, ok := g.vars[ge.name]; ok {
			if ptr, ok2 := alloc.Type().(*types.PointerType); ok2 {
				loaded := g.current.NewLoad(ptr.ElemType, alloc)
				var handleI8 value.Value
				if loaded.Type().Equal(types.I8Ptr) {
					handleI8 = loaded
				} else {
					handleI8 = g.current.NewBitCast(loaded, types.I8Ptr)
				}
				g.current.NewCall(unlockFn, handleI8)
			}
		}
	}
}

// emitTaskDrops emits srt_drop_task_handle for each task variable in the given list.
// The alloca stores the raw i8* handle; loading NULL means the task was already consumed.
func (g *Generator) emitTaskDrops(names []string) {
	dropFn := g.findFunc("srt_drop_task_handle")
	if dropFn == nil {
		return
	}
	for _, name := range names {
		if alloc, ok := g.vars[name]; ok {
			if ptr, ok2 := alloc.Type().(*types.PointerType); ok2 {
				loaded := g.current.NewLoad(ptr.ElemType, alloc)
				// Cast to i8* if needed (task handles are stored as i8*)
				var handleI8 value.Value
				if loaded.Type().Equal(types.I8Ptr) {
					handleI8 = loaded
				} else {
					handleI8 = g.current.NewBitCast(loaded, types.I8Ptr)
				}
				g.current.NewCall(dropFn, handleI8)
			}
		}
	}
}

// popScopeAndRelease emits release calls for all heap vars owned by the current scope,
// then pops the scope. Safe to call even if the current block is already terminated.
func (g *Generator) popScopeAndRelease() {
	n := len(g.scopeStack)
	if n == 0 {
		return
	}
	owned := g.scopeStack[n-1]
	g.scopeStack = g.scopeStack[:n-1]

	var taskOwned []string
	if len(g.taskVarStack) > 0 {
		taskOwned = g.taskVarStack[len(g.taskVarStack)-1]
		g.taskVarStack = g.taskVarStack[:len(g.taskVarStack)-1]
	}

	var guardOwned []syncGuardEntry
	if len(g.syncGuardStack) > 0 {
		guardOwned = g.syncGuardStack[len(g.syncGuardStack)-1]
		g.syncGuardStack = g.syncGuardStack[:len(g.syncGuardStack)-1]
	}

	var arcOwned []string
	if len(g.arcVarStack) > 0 {
		arcOwned = g.arcVarStack[len(g.arcVarStack)-1]
		g.arcVarStack = g.arcVarStack[:len(g.arcVarStack)-1]
	}

	// Skip instruction emission if the block is already terminated (e.g. after break/return).
	blocked := g.current == nil || g.current.Term != nil
	if !blocked {
		g.emitSyncGuardDrops(guardOwned)
		g.emitTaskDrops(taskOwned)
		g.emitArcDrops(arcOwned)
	}
	for _, name := range owned {
		if !blocked {
			if alloc, ok := g.vars[name]; ok {
				if ptr, ok2 := alloc.Type().(*types.PointerType); ok2 {
					loaded := g.current.NewLoad(ptr.ElemType, alloc)
					g.emitRelease(loaded)
				}
			}
		}
		delete(g.heapVars, name)
	}
	if !blocked && len(g.scopeStack) == 0 {
		g.current.NewCall(g.findBuiltin("soyuz_orc_collect"))
	}
}

func (g *Generator) emitSoyuzAlloc(size value.Value, typeKey string) value.Value {
	dtorArg := value.Value(constant.NewNull(types.I8Ptr))
	traceArg := value.Value(constant.NewNull(types.I8Ptr))
	if dtor, ok := g.destructors[typeKey]; ok {
		dtorArg = g.current.NewBitCast(dtor, types.I8Ptr)
		if trace, ok := g.traces[typeKey]; ok {
			traceArg = g.current.NewBitCast(trace, types.I8Ptr)
		}
	}
	return g.current.NewCall(g.findBuiltin("soyuz_alloc"), size, dtorArg, traceArg)
}

// releaseAllScopes emits release calls for ALL heap vars in the current scope stack.
// Used for early returns to ensure everything is cleaned up before exiting the function.
func (g *Generator) releaseAllScopes() {
	if g.current == nil || g.current.Term != nil {
		return
	}
	// Iterate backwards: inner scopes first.
	for i := len(g.syncGuardStack) - 1; i >= 0; i-- {
		g.emitSyncGuardDrops(g.syncGuardStack[i])
	}
	for i := len(g.taskVarStack) - 1; i >= 0; i-- {
		g.emitTaskDrops(g.taskVarStack[i])
	}
	for i := len(g.arcVarStack) - 1; i >= 0; i-- {
		g.emitArcDrops(g.arcVarStack[i])
	}
	for i := len(g.scopeStack) - 1; i >= 0; i-- {
		owned := g.scopeStack[i]
		for _, name := range owned {
			if alloc, ok := g.vars[name]; ok {
				if ptr, ok2 := alloc.Type().(*types.PointerType); ok2 {
					loaded := g.current.NewLoad(ptr.ElemType, alloc)
					g.emitRelease(loaded)
				}
			}
		}
	}
}

// prepareReturn retains heap return values so they survive releaseAllScopes.
func (g *Generator) prepareReturn(val value.Value) value.Value {
	if val != nil && g.isHeapType(val.Type()) {
		g.emitRetain(val)
	}
	return val
}

func (g *Generator) enumRCFnArgs(typeName string) (dtorArg, traceArg value.Value) {
	dtorArg = constant.NewNull(types.I8Ptr)
	traceArg = constant.NewNull(types.I8Ptr)
	if dtor, ok := g.destructors[typeName]; ok {
		dtorArg = g.current.NewBitCast(dtor, types.I8Ptr)
	}
	if trace, ok := g.traces[typeName]; ok {
		traceArg = g.current.NewBitCast(trace, types.I8Ptr)
	}
	return dtorArg, traceArg
}

func (g *Generator) newBlock(name string, fn *ir.Func) *ir.Block {
	count := g.blockNames[name]
	g.blockNames[name]++
	if count == 0 {
		return fn.NewBlock(name)
	}
	return fn.NewBlock(fmt.Sprintf("%s_%d", name, count))
}

// newAlloca inserts an alloca in the function's entry block so it dominates all uses.
func (g *Generator) newAlloca(t types.Type) *ir.InstAlloca {
	entry := g.current.Parent.Blocks[0]
	alloc := ir.NewAlloca(t)
	entry.Insts = append([]ir.Instruction{alloc}, entry.Insts...)
	return alloc
}

// Generate translates the AST Program into an LLVM Module.
func (g *Generator) Generate(prog *parser.Program) (*ir.Module, error) {
	g.declareBuiltins()

	// 1. Generate non-function top-level nodes (records, enums, classes)
	for _, node := range prog.Body {
		if _, ok := node.(*parser.FuncDecl); ok {
			continue
		}
		if err := g.generateTopLevel(node); err != nil {
			return nil, err
		}
	}

	// 2. Declare all function signatures
	for name, variants := range g.check.FuncVariants {
		g.declareFuncVariants(name, variants)
	}

	// 2.5. Generate class method bodies (deferred from step 1 so that top-level
	// functions declared in step 2 are visible when method bodies call them).
	for _, pm := range g.pendingClassBodies {
		if err := g.generateClassMethodBody(pm.className, pm.si, pm.fd, pm.fn); err != nil {
			return nil, err
		}
	}
	for _, pm := range g.pendingExtendBodies {
		if err := g.generateExtendMethodBody(pm); err != nil {
			return nil, err
		}
	}

	// 3. Generate function bodies
	for name, variants := range g.check.FuncVariants {
		if err := g.generateFuncVariantsBody(name, variants); err != nil {
			return nil, err
		}
	}

	return g.module, nil
}

func (g *Generator) declareBuiltins() {
	printf := g.module.NewFunc("printf", types.I32, ir.NewParam("", types.I8Ptr))
	printf.Sig.Variadic = true

	sprintf := g.module.NewFunc("sprintf", types.I32, ir.NewParam("", types.I8Ptr), ir.NewParam("", types.I8Ptr))
	sprintf.Sig.Variadic = true

	g.module.NewFunc("malloc", types.I8Ptr, ir.NewParam("", types.I64))
	g.module.NewFunc("free", types.Void, ir.NewParam("", types.I8Ptr))

	// RC runtime — implemented in runtime/rc.c, linked alongside the compiled output.
	g.module.NewFunc("soyuz_alloc", types.I8Ptr,
		ir.NewParam("size", types.I64),
		ir.NewParam("dtor", types.I8Ptr),
		ir.NewParam("trace", types.I8Ptr))
	g.module.NewFunc("soyuz_retain", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_release", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_orc_collect", types.Void)

	// SoyuzString type: %SoyuzString = { i64 len }
	g.soyuzStringType = g.module.NewTypeDef("SoyuzString", types.NewStruct(types.I64)).(*types.StructType)
	g.soyuzStringPtrType = types.NewPointer(g.soyuzStringType)

	// String construction helpers
	g.module.NewFunc("soyuz_str_new", g.soyuzStringPtrType,
		ir.NewParam("data", types.I8Ptr),
		ir.NewParam("len", types.I64))
	g.module.NewFunc("soyuz_str_from_cstr", g.soyuzStringPtrType,
		ir.NewParam("cstr", types.I8Ptr))
	g.module.NewFunc("soyuz_str_from_printf_buf", g.soyuzStringPtrType,
		ir.NewParam("buf", types.I8Ptr))
	g.module.NewFunc("soyuz_str_len", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_is_empty", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_trim", g.soyuzStringPtrType,
		ir.NewParam("s", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_to_upper", g.soyuzStringPtrType,
		ir.NewParam("s", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_to_lower", g.soyuzStringPtrType,
		ir.NewParam("s", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_substring", g.soyuzStringPtrType,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("start", types.I64),
		ir.NewParam("end", types.I64))
	g.module.NewFunc("soyuz_str_replace", g.soyuzStringPtrType,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("from", g.soyuzStringPtrType),
		ir.NewParam("to", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_contains", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("sub", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_starts_with", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("prefix", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_ends_with", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("suffix", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_index_of", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("sub", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_last_index_of", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("sub", g.soyuzStringPtrType))

	g.module.NewFunc("soyuz_int_to_str", g.soyuzStringPtrType,
		ir.NewParam("n", types.I64))
	g.module.NewFunc("soyuz_int_abs", types.I64, ir.NewParam("n", types.I64))
	g.module.NewFunc("soyuz_int_to_float", types.Double, ir.NewParam("n", types.I64))

	// List primitives
	g.module.NewFunc("soyuz_list_new", types.I8Ptr,
		ir.NewParam("capacity", types.I64),
		ir.NewParam("dtor", types.I8Ptr))
	g.module.NewFunc("soyuz_list_append", types.Void,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("value", types.I8Ptr))
	g.module.NewFunc("soyuz_list_get", types.I8Ptr,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("index", types.I64))
	g.module.NewFunc("soyuz_list_dtor_rc", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_list_dtor_primitive", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_list_set", types.Void,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("index", types.I64),
		ir.NewParam("value", types.I8Ptr))
	g.module.NewFunc("soyuz_list_set_rc", types.Void,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("index", types.I64),
		ir.NewParam("value", types.I8Ptr))
	g.module.NewFunc("soyuz_list_remove", types.I8Ptr,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("index", types.I64))
	g.module.NewFunc("soyuz_list_pop", types.I8Ptr,
		ir.NewParam("list", types.I8Ptr))
	g.module.NewFunc("soyuz_list_prepend", types.Void,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("value", types.I8Ptr))
	g.module.NewFunc("soyuz_list_clear_rc", types.Void, ir.NewParam("list", types.I8Ptr))
	g.module.NewFunc("soyuz_list_clear_primitive", types.Void, ir.NewParam("list", types.I8Ptr))
	g.module.NewFunc("soyuz_list_copy", types.I8Ptr,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("elem_is_heap", types.I64))
	g.module.NewFunc("soyuz_list_concat", types.I8Ptr,
		ir.NewParam("list_a", types.I8Ptr),
		ir.NewParam("list_b", types.I8Ptr),
		ir.NewParam("elem_is_heap", types.I64))
	g.module.NewFunc("soyuz_list_size", types.I64,
		ir.NewParam("list", types.I8Ptr))
	g.module.NewFunc("soyuz_list_to_string", g.soyuzStringPtrType,
		ir.NewParam("list", types.I8Ptr),
		ir.NewParam("elem_kind", types.I64))
	g.module.NewFunc("soyuz_map_size", types.I64,
		ir.NewParam("map", types.I8Ptr))
	g.module.NewFunc("soyuz_map_to_string", g.soyuzStringPtrType,
		ir.NewParam("map", types.I8Ptr),
		ir.NewParam("key_is_string", types.I64),
		ir.NewParam("val_kind", types.I64))

	// Map primitives
	g.module.NewFunc("soyuz_map_new", types.I8Ptr,
		ir.NewParam("is_string_key", types.I64),
		ir.NewParam("dtor", types.I8Ptr))
	g.module.NewFunc("soyuz_map_set", types.Void,
		ir.NewParam("map", types.I8Ptr),
		ir.NewParam("key", types.I8Ptr),
		ir.NewParam("value", types.I8Ptr))
	g.module.NewFunc("soyuz_map_get", types.I8Ptr,
		ir.NewParam("map", types.I8Ptr),
		ir.NewParam("key", types.I8Ptr))
	g.module.NewFunc("soyuz_map_dtor_primitive", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_map_dtor_rc_key", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_map_dtor_rc_val", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_map_dtor_rc_both", types.Void, ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("soyuz_map_keys", types.I8Ptr,
		ir.NewParam("map_ptr", types.I8Ptr),
		ir.NewParam("key_is_heap", types.I64))
	g.module.NewFunc("soyuz_map_values", types.I8Ptr,
		ir.NewParam("map_ptr", types.I8Ptr),
		ir.NewParam("val_is_heap", types.I64))
	g.module.NewFunc("soyuz_str_concat", g.soyuzStringPtrType,
		ir.NewParam("s1", g.soyuzStringPtrType),
		ir.NewParam("s2", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_split", types.I8Ptr,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("delim", g.soyuzStringPtrType))
	g.module.NewFunc("soyuz_str_byte_at", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("index", types.I64))
	g.module.NewFunc("soyuz_str_unicode_at", types.I64,
		ir.NewParam("s", g.soyuzStringPtrType),
		ir.NewParam("char_index", types.I64))

	// Shared closure fat-pointer layout: { fn_ptr: i8*, env_ptr: i8* }
	g.closureType = g.module.NewTypeDef("SoyuzClosure",
		types.NewStruct(types.I8Ptr, types.I8Ptr)).(*types.StructType)

	// List struct layout: { size: i64, cap: i64, data: i8** }
	listStruct := g.module.NewTypeDef("SoyuzList",
		types.NewStruct(types.I64, types.I64, types.NewPointer(types.I8Ptr))).(*types.StructType)
	g.structs["SoyuzList"] = structInfo{
		typ: listStruct,
		fieldIndices: map[string]int{
			"size":     0,
			"capacity": 1,
			"data":     2,
		},
	}

	// Map struct layout: { size: i64, cap: i64, entries: i8*, is_string_key: i64 }
	mapStruct := g.module.NewTypeDef("SoyuzMap",
		types.NewStruct(types.I64, types.I64, types.I8Ptr, types.I64)).(*types.StructType)
	g.structs["SoyuzMap"] = structInfo{
		typ: mapStruct,
		fieldIndices: map[string]int{
			"size":          0,
			"capacity":      1,
			"entries":       2,
			"is_string_key": 3,
		},
	}

	// Iterator layout: { list: i8*, index: i64 }
	iterStruct := g.module.NewTypeDef("SoyuzIterator",
		types.NewStruct(types.I8Ptr, types.I64)).(*types.StructType)
	g.structs["SoyuzIterator"] = structInfo{
		typ: iterStruct,
		fieldIndices: map[string]int{
			"list":  0,
			"index": 1,
		},
	}

	// Task runtime — soyuz_rt.c
	g.module.NewFunc("srt_enqueue", types.I8Ptr,
		ir.NewParam("fn", types.I8Ptr),
		ir.NewParam("args", types.I8Ptr))
	g.module.NewFunc("srt_await", types.I8Ptr,
		ir.NewParam("handle", types.I8Ptr))
	g.module.NewFunc("srt_detach", types.Void,
		ir.NewParam("handle", types.I8Ptr))
	g.module.NewFunc("srt_set_task_result", types.Void,
		ir.NewParam("result", types.I8Ptr))
	g.module.NewFunc("srt_drop_task_handle", types.Void,
		ir.NewParam("handle", types.I8Ptr))
	g.module.NewFunc("srt_cancel", types.Void,
		ir.NewParam("handle", types.I8Ptr))
	g.module.NewFunc("srt_await_any", types.I8Ptr,
		ir.NewParam("handles", types.NewPointer(types.I8Ptr)),
		ir.NewParam("n", types.I64))
	g.module.NewFunc("srt_task_handle_current", types.I8Ptr)
	g.module.NewFunc("srt_task_cancelled", types.I64,
		ir.NewParam("handle", types.I8Ptr))
	g.module.NewFunc("srt_task_set_progress", types.Void,
		ir.NewParam("handle", types.I8Ptr),
		ir.NewParam("progress", types.Double))

	// M8: stdlib/sync — Mutex[T], RwLock[T], Atomic[T]
	g.module.NewFunc("srt_mutex_new", types.I8Ptr,
		ir.NewParam("initial", types.I64))
	g.module.NewFunc("srt_mutex_lock", types.I8Ptr,
		ir.NewParam("mx", types.I8Ptr))
	g.module.NewFunc("srt_mutex_unlock", types.Void,
		ir.NewParam("guard", types.I8Ptr))
	g.module.NewFunc("srt_mutex_guard_get", types.I64,
		ir.NewParam("guard", types.I8Ptr))
	g.module.NewFunc("srt_mutex_guard_set", types.Void,
		ir.NewParam("guard", types.I8Ptr),
		ir.NewParam("val", types.I64))
	g.module.NewFunc("srt_rwlock_new", types.I8Ptr,
		ir.NewParam("initial", types.I64))
	g.module.NewFunc("srt_rwlock_read", types.I8Ptr,
		ir.NewParam("rw", types.I8Ptr))
	g.module.NewFunc("srt_rwlock_write", types.I8Ptr,
		ir.NewParam("rw", types.I8Ptr))
	g.module.NewFunc("srt_rwlock_unlock", types.Void,
		ir.NewParam("guard", types.I8Ptr))
	g.module.NewFunc("srt_rwlock_guard_get", types.I64,
		ir.NewParam("guard", types.I8Ptr))
	g.module.NewFunc("srt_rwlock_guard_set", types.Void,
		ir.NewParam("guard", types.I8Ptr),
		ir.NewParam("val", types.I64))
	g.module.NewFunc("srt_atomic_new", types.I8Ptr,
		ir.NewParam("initial", types.I64))
	g.module.NewFunc("srt_atomic_load", types.I64,
		ir.NewParam("a", types.I8Ptr))
	g.module.NewFunc("srt_atomic_store", types.Void,
		ir.NewParam("a", types.I8Ptr),
		ir.NewParam("val", types.I64))
	g.module.NewFunc("srt_atomic_add", types.I64,
		ir.NewParam("a", types.I8Ptr),
		ir.NewParam("delta", types.I64))
	g.module.NewFunc("srt_atomic_cas", types.I64,
		ir.NewParam("a", types.I8Ptr),
		ir.NewParam("expected", types.I64),
		ir.NewParam("desired", types.I64))

	// M9: Channel[T], SyncChannel[T]
	g.module.NewFunc("srt_chan_new", types.I8Ptr,
		ir.NewParam("capacity", types.I64))
	g.module.NewFunc("srt_chan_send", types.Void,
		ir.NewParam("ch", types.I8Ptr),
		ir.NewParam("val", types.I64))
	g.module.NewFunc("srt_chan_recv", types.I64,
		ir.NewParam("ch", types.I8Ptr),
		ir.NewParam("out", types.NewPointer(types.I64)))
	g.module.NewFunc("srt_chan_try_recv", types.I64,
		ir.NewParam("ch", types.I8Ptr),
		ir.NewParam("out", types.NewPointer(types.I64)))
	g.module.NewFunc("srt_chan_close", types.Void,
		ir.NewParam("ch", types.I8Ptr))
	g.module.NewFunc("srt_chan_is_closed", types.I64,
		ir.NewParam("ch", types.I8Ptr))
	g.module.NewFunc("srt_sync_chan_new", types.I8Ptr)
	g.module.NewFunc("srt_sync_chan_send", types.Void,
		ir.NewParam("ch", types.I8Ptr),
		ir.NewParam("val", types.I64))
	g.module.NewFunc("srt_sync_chan_recv", types.I64,
		ir.NewParam("ch", types.I8Ptr),
		ir.NewParam("out", types.NewPointer(types.I64)))
	g.module.NewFunc("srt_sync_chan_close", types.Void,
		ir.NewParam("ch", types.I8Ptr))
	// M20: select { ch.recv() => ... }
	g.module.NewFunc("srt_select", types.I64,
		ir.NewParam("channels", types.NewPointer(types.I8Ptr)),
		ir.NewParam("n", types.I64),
		ir.NewParam("out", types.NewPointer(types.I64)))
	g.module.NewFunc("srt_select_try", types.I64,
		ir.NewParam("channels", types.NewPointer(types.I8Ptr)),
		ir.NewParam("n", types.I64),
		ir.NewParam("out", types.NewPointer(types.I64)))

	// M14: Arc[T] with EBR
	g.module.NewFunc("srt_arc_new", types.I8Ptr,
		ir.NewParam("value", types.I64))
	g.module.NewFunc("srt_arc_clone", types.I8Ptr,
		ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("srt_arc_release", types.Void,
		ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("srt_arc_get", types.I64,
		ir.NewParam("ptr", types.I8Ptr))
	g.module.NewFunc("srt_arc_refcount", types.I64,
		ir.NewParam("ptr", types.I8Ptr))

	// Pre-register builtin Option and Result enum types so mapSoyuzTypeToLLVM can use them
	// before any user source generates a Some/None/Ok/Err expression.
	optionTyp := g.module.NewTypeDef("Option", types.NewStruct(types.I64, types.NewArray(64, types.I8))).(*types.StructType)
	g.enums["Option"] = enumInfo{
		typ: optionTyp,
		variants: map[string]variantInfo{
			"Some": {tag: 0},
			"None": {tag: 1},
		},
	}
	resultTyp := g.module.NewTypeDef("Result", types.NewStruct(types.I64, types.NewArray(64, types.I8))).(*types.StructType)
	g.enums["Result"] = enumInfo{
		typ: resultTyp,
		variants: map[string]variantInfo{
			"Ok":  {tag: 0},
			"Err": {tag: 1},
		},
	}
}

// callClosureI8Ptr calls a Soyuz closure value (i8* pointing to SoyuzClosure{ fn, env }).
// The underlying function expects (i8* env, original_args...).
func (g *Generator) callClosureI8Ptr(n *parser.CallExpr, closureI8 value.Value, args []value.Value) (value.Value, error) {
	// Return type from checker.
	retType := types.Type(types.I64)
	if ft, ok := g.check.Specializations[n]; ok && ft != nil {
		retType = g.mapTypeToLLVM(ft.Return)
	}

	// Bitcast i8* → SoyuzClosure*
	closurePtr := g.current.NewBitCast(closureI8, types.NewPointer(g.closureType))

	// Load fn_ptr (i8*)
	fnField := g.current.NewGetElementPtr(g.closureType, closurePtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	fnRaw := g.current.NewLoad(types.I8Ptr, fnField)

	// Load env_ptr (i8*)
	envField := g.current.NewGetElementPtr(g.closureType, closurePtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	env := g.current.NewLoad(types.I8Ptr, envField)

	// Build concrete function type: (i8* env, arg_types...) → retType
	paramTypes := []types.Type{types.I8Ptr}
	for _, a := range args {
		paramTypes = append(paramTypes, a.Type())
	}
	fnType := types.NewFunc(retType, paramTypes...)
	fnPtr := g.current.NewBitCast(fnRaw, types.NewPointer(fnType))

	allArgs := append([]value.Value{env}, args...)
	return g.current.NewCall(fnPtr, allArgs...), nil
}

// callClosureDirect calls a closure i8* with a known return type (no *parser.CallExpr needed).
func (g *Generator) callClosureDirect(closureI8 value.Value, retType types.Type, args []value.Value) value.Value {
	closurePtr := g.current.NewBitCast(closureI8, types.NewPointer(g.closureType))
	fnField := g.current.NewGetElementPtr(g.closureType, closurePtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	fnRaw := g.current.NewLoad(types.I8Ptr, fnField)
	envField := g.current.NewGetElementPtr(g.closureType, closurePtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	env := g.current.NewLoad(types.I8Ptr, envField)
	paramTypes := []types.Type{types.I8Ptr}
	for _, a := range args {
		paramTypes = append(paramTypes, a.Type())
	}
	fnType := types.NewFunc(retType, paramTypes...)
	fnPtr := g.current.NewBitCast(fnRaw, types.NewPointer(fnType))
	allArgs := append([]value.Value{env}, args...)
	return g.current.NewCall(fnPtr, allArgs...)
}

func (g *Generator) defaultReturnValue(t types.Type) value.Value {
	switch typ := t.(type) {
	case *types.IntType:
		return constant.NewInt(typ, 0)
	case *types.FloatType:
		return constant.NewFloat(typ, 0)
	case *types.PointerType:
		return constant.NewNull(typ)
	default:
		return nil
	}
}

// strData extrai o char* inline de um %SoyuzString* usando GEP para o elemento após a struct.
func (g *Generator) strData(strPtr value.Value) value.Value {
	dataField := g.current.NewGetElementPtr(g.soyuzStringType, strPtr,
		constant.NewInt(types.I64, 1))
	return g.current.NewBitCast(dataField, types.I8Ptr)
}

func (g *Generator) castToI8Ptr(v value.Value) value.Value {
	if v.Type().Equal(types.I64) {
		return g.current.NewIntToPtr(v, types.I8Ptr)
	}
	if _, ok := v.Type().(*types.PointerType); ok {
		return g.current.NewBitCast(v, types.I8Ptr)
	}
	return g.current.NewBitCast(v, types.I8Ptr)
}

func (g *Generator) castFromI8Ptr(v value.Value, target types.Type) value.Value {
	// void is not a valid value type in LLVM IR — return the raw i8* as-is.
	if target == nil || target.Equal(types.Void) {
		return v
	}
	if target.Equal(types.I64) {
		return g.current.NewPtrToInt(v, types.I64)
	}
	if _, ok := target.(*types.PointerType); ok {
		return g.current.NewBitCast(v, target)
	}
	return g.current.NewBitCast(v, target)
}
