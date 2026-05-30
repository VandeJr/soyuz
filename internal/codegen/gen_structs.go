package codegen

import (
	"fmt"
	"strconv"
	"strings"
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

func (g *Generator) generateTopLevel(node parser.Node) error {
	switch n := node.(type) {
	case *parser.FuncDecl:
		return nil
	case *parser.RecordDecl:
		return g.generateRecordDecl(n)
	case *parser.ClassDecl:
		return g.generateClassDecl(n)
	case *parser.EnumDecl:
		return g.generateEnumDecl(n)
	case *parser.InterfaceDecl:
		g.interfaceDecls[n.Name] = n
		return nil
	case *parser.ExternDecl:
		return g.generateExternDecl(n)
	case *parser.ExtendDecl:
		return g.generateExtendDecl(n)
	default:
		return nil
	}
}

func (g *Generator) generateExternDecl(n *parser.ExternDecl) error {
	// Skip if already declared (e.g. by declareBuiltins or a previous extern).
	if g.findFunc(n.Name) != nil {
		return nil
	}
	var retType types.Type = types.Void
	if n.ReturnType != nil {
		retType = g.mapExternTypeToLLVM(n.ReturnType)
	}
	var params []*ir.Param
	for _, p := range n.Params {
		pt := g.mapExternTypeToLLVM(p.Type)
		name := ""
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok {
			name = bp.Name
		}
		params = append(params, ir.NewParam(name, pt))
	}
	// Emit declaration-only (no body) — the C implementation is linked separately.
	g.module.NewFunc(n.Name, retType, params...)
	return nil
}

func (g *Generator) generateRecordDecl(n *parser.RecordDecl) error {
	if len(n.Generics) > 0 {
		// Generic record — no concrete LLVM type yet; specialize on first use.
		g.genericRecordDecls[n.Name] = n
		return nil
	}
	indices := make(map[string]int)
	var fields []types.Type
	for i, f := range n.Fields {
		indices[f.Name] = i
		fields = append(fields, g.mapSoyuzTypeToLLVM(f.Type))
	}
	st := types.NewStruct(fields...)
	td := g.module.NewTypeDef(n.Name, st)
	si := structInfo{typ: td.(*types.StructType), fieldIndices: indices}
	g.structs[n.Name] = si
	g.destructors[n.Name] = g.generateRecordDtor(n, si)
	if trace := g.generateRecordTrace(n.Name, n, si); trace != nil {
		g.traces[n.Name] = trace
	}
	return nil
}

// llvmTypeName returns a short, identifier-safe name for an LLVM type.
// Used to build mangled names for specialized generic records.
func llvmTypeName(t types.Type) string {
	switch {
	case t == types.I64:
		return "i64"
	case t == types.I1:
		return "i1"
	case t == types.Double:
		return "f64"
	case t == types.I8Ptr:
		return "str"
	}
	if ptr, ok := t.(*types.PointerType); ok {
		if st, ok := ptr.ElemType.(*types.StructType); ok {
			return "ptr_" + st.TypeName
		}
		return "ptr"
	}
	return strings.ReplaceAll(t.String(), " ", "_")
}

// mapSoyuzTypeExprWithSub maps a type expression to LLVM, substituting type parameters.
func (g *Generator) mapSoyuzTypeExprWithSub(te parser.TypeExpr, sub map[string]types.Type) types.Type {
	if nt, ok := te.(*parser.NamedType); ok {
		if t, ok := sub[nt.Name]; ok {
			return t
		}
	}
	return g.mapSoyuzTypeToLLVM(te)
}

// getOrCreateSpecializedRecord returns (or lazily generates) the structInfo for a
// generic record instantiated with the given type substitution.
func (g *Generator) getOrCreateSpecializedRecord(decl *parser.RecordDecl, sub map[string]types.Type) (structInfo, error) {
	// Build mangled name in generic-param order for determinism.
	mangled := decl.Name
	for _, gp := range decl.Generics {
		if t, ok := sub[gp.Name]; ok {
			mangled += "__" + llvmTypeName(t)
		}
	}

	if si, ok := g.structs[mangled]; ok {
		return si, nil
	}

	indices := make(map[string]int)
	var fieldTypes []types.Type
	for i, f := range decl.Fields {
		ft := g.mapSoyuzTypeExprWithSub(f.Type, sub)
		indices[f.Name] = i
		fieldTypes = append(fieldTypes, ft)
	}

	st := types.NewStruct(fieldTypes...)
	td := g.module.NewTypeDef(mangled, st)
	si := structInfo{typ: td.(*types.StructType), fieldIndices: indices}
	g.structs[mangled] = si
	g.destructors[mangled] = g.generateSpecializedRecordDtor(mangled, decl, si, fieldTypes)
	if trace := g.generateSpecializedRecordTrace(mangled, decl, si, fieldTypes); trace != nil {
		g.traces[mangled] = trace
	}
	return si, nil
}

// generateSpecializedRecordDtor emits a destructor for a specialized generic record.
func (g *Generator) generateSpecializedRecordDtor(mangled string, decl *parser.RecordDecl, si structInfo, fieldTypes []types.Type) *ir.Func {
	dtor := g.module.NewFunc("__soyuz_dtor_"+mangled, types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")
	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(si.typ))
	release := g.findFunc("soyuz_release")

	for i, ft := range fieldTypes {
		if !g.isHeapType(ft) {
			continue
		}
		gep := entry.NewGetElementPtr(si.typ, typedPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
		fieldVal := entry.NewLoad(ft, gep)
		cast := entry.NewBitCast(fieldVal, types.I8Ptr)
		entry.NewCall(release, cast)
	}
	entry.NewRet(nil)
	return dtor
}

// generateRecordDtor emits a destructor function for a record type.
// The destructor releases any heap-typed fields before the RC runtime frees the header.
func (g *Generator) generateRecordDtor(n *parser.RecordDecl, si structInfo) *ir.Func {
	dtor := g.module.NewFunc("__soyuz_dtor_"+n.Name, types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")

	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(si.typ))
	release := g.findFunc("soyuz_release")

	for i, f := range n.Fields {
		ft := g.mapSoyuzTypeToLLVM(f.Type)
		if !g.isHeapType(ft) {
			continue
		}
		gep := entry.NewGetElementPtr(si.typ, typedPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
		fieldVal := entry.NewLoad(ft, gep)
		cast := entry.NewBitCast(fieldVal, types.I8Ptr)
		entry.NewCall(release, cast)
	}
	entry.NewRet(nil)
	return dtor
}

// generateClassDtor emits a destructor for a class that releases its heap fields.
func (g *Generator) generateClassDtor(n *parser.ClassDecl, si structInfo) *ir.Func {
	dtor := g.module.NewFunc("__soyuz_dtor_"+n.Name, types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")
	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(si.typ))
	release := g.findFunc("soyuz_release")

	fieldIdx := 0
	for _, member := range n.Body {
		v, ok := member.(*parser.VarDecl)
		if !ok {
			continue
		}
		ft := g.classFieldLLVMType(v)
		if g.isHeapType(ft) {
			gep := entry.NewGetElementPtr(si.typ, typedPtr,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(fieldIdx)))
			fieldVal := entry.NewLoad(ft, gep)
			cast := entry.NewBitCast(fieldVal, types.I8Ptr)
			entry.NewCall(release, cast)
		}
		fieldIdx++
	}
	entry.NewRet(nil)
	return dtor
}

func (g *Generator) generateRecordTrace(name string, n *parser.RecordDecl, si structInfo) *ir.Func {
	hasHeap := false
	for _, f := range n.Fields {
		if g.isHeapType(g.mapSoyuzTypeToLLVM(f.Type)) {
			hasHeap = true
			break
		}
	}
	if !hasHeap {
		return nil
	}
	return g.emitHeapFieldTrace(name, si, func(i int) types.Type {
		return g.mapSoyuzTypeToLLVM(n.Fields[i].Type)
	}, len(n.Fields))
}

func (g *Generator) generateSpecializedRecordTrace(mangled string, decl *parser.RecordDecl, si structInfo, fieldTypes []types.Type) *ir.Func {
	hasHeap := false
	for _, ft := range fieldTypes {
		if g.isHeapType(ft) {
			hasHeap = true
			break
		}
	}
	if !hasHeap {
		return nil
	}
	return g.emitHeapFieldTrace(mangled, si, func(i int) types.Type {
		return fieldTypes[i]
	}, len(fieldTypes))
}

func (g *Generator) generateClassTrace(n *parser.ClassDecl, si structInfo) *ir.Func {
	var fieldTypes []types.Type
	for _, member := range n.Body {
		if v, ok := member.(*parser.VarDecl); ok {
			fieldTypes = append(fieldTypes, g.classFieldLLVMType(v))
		}
	}
	hasHeap := false
	for _, ft := range fieldTypes {
		if g.isHeapType(ft) {
			hasHeap = true
			break
		}
	}
	if !hasHeap {
		return nil
	}
	idx := 0
	_ = idx
	return g.emitHeapFieldTrace(n.Name, si, func(i int) types.Type {
		return fieldTypes[i]
	}, len(fieldTypes))
}

func (g *Generator) emitHeapFieldTrace(name string, si structInfo, fieldType func(int) types.Type, count int) *ir.Func {
	visitFnType := types.NewPointer(&types.FuncType{
		RetType: types.Void,
		Params:  []types.Type{types.I8Ptr},
	})
	trace := g.module.NewFunc("__soyuz_trace_"+name, types.Void,
		ir.NewParam("ptr", types.I8Ptr),
		ir.NewParam("visit", visitFnType))
	entry := trace.NewBlock("entry")
	typedPtr := entry.NewBitCast(trace.Params[0], types.NewPointer(si.typ))
	for i := 0; i < count; i++ {
		ft := fieldType(i)
		if !g.isHeapType(ft) {
			continue
		}
		gep := entry.NewGetElementPtr(si.typ, typedPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
		fieldVal := entry.NewLoad(ft, gep)
		cast := entry.NewBitCast(fieldVal, types.I8Ptr)
		entry.NewCall(trace.Params[1], cast)
	}
	entry.NewRet(nil)
	return trace
}

// classFieldLLVMType returns the LLVM type for a class field.
// When the field has no explicit type annotation, the type is inferred from the
// checker's result for the init expression.
func (g *Generator) classFieldLLVMType(v *parser.VarDecl) types.Type {
	if v.Type != nil {
		return g.mapSoyuzTypeToLLVM(v.Type)
	}
	if v.Init != nil {
		if ct, ok := g.check.NodeTypes[v.Init]; ok && ct != checker.Unknown {
			return g.mapTypeToLLVM(ct)
		}
	}
	return types.I64
}

func (g *Generator) generateClassDecl(n *parser.ClassDecl) error {
	// 1. Build data-only struct (no vtable pointer embedded).
	indices := make(map[string]int)
	var fields []types.Type
	fieldIdx := 0
	for _, member := range n.Body {
		if v, ok := member.(*parser.VarDecl); ok {
			indices[v.Name] = fieldIdx
			fields = append(fields, g.classFieldLLVMType(v))
			fieldIdx++
		}
	}
	st := types.NewStruct(fields...)
	td := g.module.NewTypeDef(n.Name, st)
	si := structInfo{typ: td.(*types.StructType), fieldIndices: indices}
	g.structs[n.Name] = si
	g.destructors[n.Name] = g.generateClassDtor(n, si)
	if trace := g.generateClassTrace(n, si); trace != nil {
		g.traces[n.Name] = trace
	}

	ci := classInfo{
		typ:          td.(*types.StructType),
		fieldIndices: indices,
		methods:      make(map[string][]*ir.Func),
		vtables:      make(map[string]*ir.Global),
	}
	g.classes[n.Name] = ci

	// 2. Count how many variants each method name has (needed for name mangling).
	methodCounts := make(map[string]int)
	for _, member := range n.Body {
		if fd, ok := member.(*parser.FuncDecl); ok {
			methodCounts[fd.Name]++
		}
	}

	// 3. Generate method functions: ClassName_method(i8* __self, params...) -> retType
	// Overloaded methods get mangled: ClassName_method_N where N = non-self arity.
	for _, member := range n.Body {
		fd, ok := member.(*parser.FuncDecl)
		if !ok {
			continue
		}
		nonSelfArity := 0
		for _, p := range fd.Params {
			if bp, ok2 := p.Pattern.(*parser.BindingPattern); ok2 && bp.Name == "self" {
				continue
			}
			nonSelfArity++
		}
		methodName := fd.Name
		if methodCounts[fd.Name] > 1 {
			methodName = fmt.Sprintf("%s_%d", fd.Name, nonSelfArity)
		}
		fn := g.declareClassMethod(n.Name, si, fd, methodName)
		ci.methods[fd.Name] = append(ci.methods[fd.Name], fn)
		// Defer body generation until all top-level function signatures are declared.
		if fd.Body != nil {
			g.pendingClassBodies = append(g.pendingClassBodies, pendingClassMethodBody{
				className: n.Name,
				si:        si,
				fd:        fd,
				fn:        fn,
			})
		}
	}

	// 4. Generate vtable globals for each implemented interface.
	for _, ifaceExpr := range n.Interfaces {
		nt, ok := ifaceExpr.(*parser.NamedType)
		if !ok {
			continue
		}
		ifaceDecl, ok := g.interfaceDecls[nt.Name]
		if !ok {
			continue
		}
		vtable := g.generateVtable(n.Name, nt.Name, ifaceDecl, ci)
		ci.vtables[nt.Name] = vtable
	}
	g.classes[n.Name] = ci // update with vtables
	return nil
}

// declareClassMethod creates the LLVM function signature for a class method without
// generating the body. The body is deferred to generateClassMethodBody so that
// top-level functions declared in step 2 of Generate are visible.
func (g *Generator) declareClassMethod(className string, si structInfo, fd *parser.FuncDecl, methodName string) *ir.Func {
	ft, _ := g.check.NodeTypes[fd].(*checker.FuncType)

	var retType types.Type = types.Void
	if ft != nil && ft.Return.String() != "Unit" {
		retType = g.mapTypeToLLVM(ft.Return)
	} else if fd.ReturnType != nil {
		retType = g.mapSoyuzTypeToLLVM(fd.ReturnType)
	}

	var nonSelfParams []parser.FuncParam
	for _, p := range fd.Params {
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok && bp.Name == "self" {
			continue
		}
		nonSelfParams = append(nonSelfParams, p)
	}

	var params []*ir.Param
	params = append(params, ir.NewParam("__self", types.I8Ptr))
	for i, p := range nonSelfParams {
		var pt types.Type
		if ft != nil && i < len(ft.Params) {
			pt = g.mapTypeToLLVM(ft.Params[i])
		} else if p.Type != nil {
			pt = g.mapSoyuzTypeToLLVM(p.Type)
		} else {
			pt = types.I64
		}
		paramName := ""
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok {
			paramName = bp.Name
		}
		params = append(params, ir.NewParam(paramName, pt))
	}

	fn := g.module.NewFunc(className+"_"+methodName, retType, params...)

	// Stub for body-less methods (interface stubs, abstract-like).
	if fd.Body == nil {
		entry := fn.NewBlock("entry")
		if retType.Equal(types.Void) {
			entry.NewRet(nil)
		} else {
			entry.NewRet(g.defaultReturnValue(retType))
		}
	}
	return fn
}

// generateClassMethodBody fills in the body of a previously-declared class method.
func (g *Generator) generateClassMethodBody(className string, si structInfo, fd *parser.FuncDecl, fn *ir.Func) error {
	if fd.Body == nil {
		return nil
	}
	retType := fn.Sig.RetType

	// Save/restore outer codegen state.
	oldCurrent := g.current
	oldVars := g.vars
	oldHeapVars := g.heapVars
	oldScopeStack := g.scopeStack
	oldTaskVarStack := g.taskVarStack
	oldSyncGuardStack := g.syncGuardStack
	oldArcVarStack := g.arcVarStack
	oldBlockNames := g.blockNames
	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack = nil
	g.taskVarStack = nil
	g.syncGuardStack = nil
	g.arcVarStack = nil
	g.blockNames = make(map[string]int)
	defer func() {
		g.current = oldCurrent
		g.vars = oldVars
		g.heapVars = oldHeapVars
		g.scopeStack = oldScopeStack
		g.taskVarStack = oldTaskVarStack
		g.syncGuardStack = oldSyncGuardStack
		g.arcVarStack = oldArcVarStack
		g.blockNames = oldBlockNames
	}()

	g.current = g.newBlock("entry", fn)

	// Expose self in the method scope.
	if len(si.typ.Fields) == 0 {
		selfTyped := g.current.NewBitCast(fn.Params[0], g.soyuzStringPtrType)
		selfAlloc := g.newAlloca(g.soyuzStringPtrType)
		g.current.NewStore(selfTyped, selfAlloc)
		g.vars["self"] = selfAlloc
	} else {
		selfTyped := g.current.NewBitCast(fn.Params[0], types.NewPointer(si.typ))
		selfAlloc := g.newAlloca(types.NewPointer(si.typ))
		g.current.NewStore(selfTyped, selfAlloc)
		g.vars["self"] = selfAlloc
	}

	for _, p := range fn.Params[1:] {
		if p.LocalName != "" {
			alloc := g.newAlloca(p.Typ)
			g.current.NewStore(p, alloc)
			g.vars[p.LocalName] = alloc
		}
	}

	val, err := g.generateExpr(fd.Body)
	if err != nil {
		return err
	}

	if retType.Equal(types.Void) {
		if g.current.Term == nil {
			g.current.NewRet(nil)
		}
	} else if g.current.Term == nil {
		if val != nil {
			g.current.NewRet(val)
		} else {
			g.current.NewRet(g.defaultReturnValue(retType))
		}
	}
	return nil
}

// generateVtable emits a global constant [N x i8*] vtable for a (class, interface) pair.
// Slots are filled in interface method declaration order.
func (g *Generator) generateVtable(className, ifaceName string, ifaceDecl *parser.InterfaceDecl, ci classInfo) *ir.Global {
	n := uint64(len(ifaceDecl.Methods))
	arrType := types.NewArray(n, types.I8Ptr)

	var slots []constant.Constant
	for _, m := range ifaceDecl.Methods {
		variants := ci.methods[m.Name]
		// Count non-self params in the interface method AST to find the matching variant.
		ifaceArity := 0
		for _, p := range m.Params {
			if bp, ok := p.Pattern.(*parser.BindingPattern); ok && bp.Name == "self" {
				continue
			}
			ifaceArity++
		}
		fn := classMethodByArity(variants, ifaceArity)
		if fn != nil {
			slots = append(slots, constant.NewBitCast(fn, types.I8Ptr))
		} else {
			slots = append(slots, constant.NewNull(types.I8Ptr))
		}
	}

	arr := constant.NewArray(arrType, slots...)
	vtableName := "__vtable_" + className + "_" + ifaceName
	global := g.module.NewGlobalDef(vtableName, arr)
	global.Immutable = true
	return global
}

// classMethodByArity returns the LLVM function variant whose non-self parameter count
// matches nonSelfArity. Falls back to the first variant when no exact match is found.
func classMethodByArity(variants []*ir.Func, nonSelfArity int) *ir.Func {
	for _, fn := range variants {
		// fn.Params[0] is always __self; the rest are non-self params.
		if len(fn.Params)-1 == nonSelfArity {
			return fn
		}
	}
	if len(variants) > 0 {
		return variants[0]
	}
	return nil
}

func (g *Generator) generateEnumDecl(n *parser.EnumDecl) error {
	if len(n.Generics) > 0 {
		g.genericEnumDecls[n.Name] = n
		return nil
	}
	variants := make(map[string]variantInfo)
	for i, v := range n.Variants {
		var fields []types.Type
		for _, f := range v.Fields {
			fields = append(fields, g.mapSoyuzTypeToLLVM(f.Type))
		}
		variants[v.Name] = variantInfo{tag: i, fields: fields}
	}
	// Enum layout: { i64 tag, [64 x i8] payload }
	st := g.module.NewTypeDef(n.Name, types.NewStruct(types.I64, types.NewArray(64, types.I8)))
	ei := enumInfo{typ: st.(*types.StructType), variants: variants}
	g.enums[n.Name] = ei
	g.destructors[n.Name] = g.generateEnumDtor(n, ei)
	if trace := g.generateEnumTrace(n.Name, n.Variants, ei); trace != nil {
		g.traces[n.Name] = trace
	}
	return nil
}

// generateEnumDtor emits a destructor for an enum type.
// For each variant whose primary payload field is heap-managed, the destructor
// checks the tag, loads the payload pointer, and calls soyuz_release on it.
func (g *Generator) generateEnumDtor(n *parser.EnumDecl, ei enumInfo) *ir.Func {
	dtor := g.module.NewFunc("__soyuz_dtor_"+n.Name, types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")

	// Determine which variants have heap-typed payload (first field only).
	type heapVariant struct {
		name      string
		tag       int
		fieldType types.Type
	}
	var heapVariants []heapVariant
	for i, v := range n.Variants {
		vi := ei.variants[v.Name]
		if len(vi.fields) > 0 && g.isHeapType(vi.fields[0]) {
			heapVariants = append(heapVariants, heapVariant{v.Name, i, vi.fields[0]})
		}
	}

	if len(heapVariants) == 0 {
		entry.NewRet(nil)
		return dtor
	}

	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(ei.typ))
	tagPtr := entry.NewGetElementPtr(ei.typ, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := entry.NewLoad(types.I64, tagPtr)
	payloadPtr := entry.NewGetElementPtr(ei.typ, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	release := g.findFunc("soyuz_release")
	doneBlock := dtor.NewBlock("dtor_done")

	// Chain of tag checks, one per heap-carrying variant.
	current := entry
	for _, hv := range heapVariants {
		releaseBlock := dtor.NewBlock("dtor_" + hv.name)
		skipBlock := dtor.NewBlock("dtor_" + hv.name + "_skip")

		cond := current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, int64(hv.tag)))
		current.NewCondBr(cond, releaseBlock, skipBlock)

		castPtr := releaseBlock.NewBitCast(payloadPtr, types.NewPointer(hv.fieldType))
		fieldVal := releaseBlock.NewLoad(hv.fieldType, castPtr)
		rawPtr := releaseBlock.NewBitCast(fieldVal, types.I8Ptr)
		releaseBlock.NewCall(release, rawPtr)
		releaseBlock.NewBr(skipBlock)

		current = skipBlock
	}
	current.NewBr(doneBlock)
	doneBlock.NewRet(nil)
	return dtor
}

// generateEnumTrace emits an ORC trace function for enums with heap-typed payload fields.
func (g *Generator) generateEnumTrace(name string, variants []parser.EnumVariant, ei enumInfo) *ir.Func {
	type heapVariant struct {
		name      string
		tag       int
		fieldType types.Type
	}
	var heapVariants []heapVariant
	for i, v := range variants {
		vi := ei.variants[v.Name]
		if len(vi.fields) > 0 && g.isHeapType(vi.fields[0]) {
			heapVariants = append(heapVariants, heapVariant{v.Name, i, vi.fields[0]})
		}
	}
	if len(heapVariants) == 0 {
		return nil
	}

	visitFnType := types.NewPointer(&types.FuncType{
		RetType: types.Void,
		Params:  []types.Type{types.I8Ptr},
	})
	trace := g.module.NewFunc("__soyuz_trace_"+name, types.Void,
		ir.NewParam("ptr", types.I8Ptr),
		ir.NewParam("visit", visitFnType))
	entry := trace.NewBlock("entry")

	typedPtr := entry.NewBitCast(trace.Params[0], types.NewPointer(ei.typ))
	tagPtr := entry.NewGetElementPtr(ei.typ, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := entry.NewLoad(types.I64, tagPtr)
	payloadPtr := entry.NewGetElementPtr(ei.typ, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	doneBlock := trace.NewBlock("trace_done")
	current := entry
	for _, hv := range heapVariants {
		visitBlock := trace.NewBlock("trace_" + hv.name)
		skipBlock := trace.NewBlock("trace_" + hv.name + "_skip")

		cond := current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, int64(hv.tag)))
		current.NewCondBr(cond, visitBlock, skipBlock)

		castPtr := visitBlock.NewBitCast(payloadPtr, types.NewPointer(hv.fieldType))
		fieldVal := visitBlock.NewLoad(hv.fieldType, castPtr)
		rawPtr := visitBlock.NewBitCast(fieldVal, types.I8Ptr)
		visitBlock.NewCall(trace.Params[1], rawPtr)
		visitBlock.NewBr(skipBlock)

		current = skipBlock
	}
	current.NewBr(doneBlock)
	doneBlock.NewRet(nil)
	return trace
}

// getOrCreateSpecializedEnum lazily generates a concrete LLVM type for a generic enum
// instantiated with the given type substitution. All enums share the layout
// { i64 tag, [64 x i8] payload } regardless of type parameters.
func (g *Generator) getOrCreateSpecializedEnum(decl *parser.EnumDecl, sub map[string]types.Type) (enumInfo, error) {
	mangled := decl.Name
	for _, gp := range decl.Generics {
		if t, ok := sub[gp.Name]; ok {
			mangled += "__" + llvmTypeName(t)
		}
	}

	if ei, ok := g.enums[mangled]; ok {
		return ei, nil
	}

	// Pre-register with empty variants to break recursive self-referential cycles
	// (e.g. Tree[T] whose Node variant contains Tree[T] fields).
	st := g.module.NewTypeDef(mangled, types.NewStruct(types.I64, types.NewArray(64, types.I8)))
	ei := enumInfo{typ: st.(*types.StructType), variants: make(map[string]variantInfo)}
	g.enums[mangled] = ei

	for i, v := range decl.Variants {
		var fields []types.Type
		for _, f := range v.Fields {
			ft := g.mapSoyuzTypeExprWithSub(f.Type, sub)
			// Recursive reference: treat as i8* to avoid infinite recursion in layout.
			if ptr, ok := ft.(*types.PointerType); ok {
				if ptr.ElemType == st {
					ft = types.I8Ptr
				}
			}
			fields = append(fields, ft)
		}
		ei.variants[v.Name] = variantInfo{tag: i, fields: fields}
	}
	g.enums[mangled] = ei
	g.destructors[mangled] = g.generateSpecializedEnumDtor(mangled, decl, ei)
	if trace := g.generateEnumTrace(mangled, decl.Variants, ei); trace != nil {
		g.traces[mangled] = trace
	}
	return ei, nil
}

// generateSpecializedEnumDtor is like generateEnumDtor but uses a mangled name.
func (g *Generator) generateSpecializedEnumDtor(mangledName string, decl *parser.EnumDecl, ei enumInfo) *ir.Func {
	dtor := g.module.NewFunc("__soyuz_dtor_"+mangledName, types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")

	type heapVariant struct {
		name      string
		tag       int
		fieldType types.Type
	}
	var heapVariants []heapVariant
	for i, v := range decl.Variants {
		vi := ei.variants[v.Name]
		if len(vi.fields) > 0 && g.isHeapType(vi.fields[0]) {
			heapVariants = append(heapVariants, heapVariant{v.Name, i, vi.fields[0]})
		}
	}

	if len(heapVariants) == 0 {
		entry.NewRet(nil)
		return dtor
	}

	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(ei.typ))
	tagPtr := entry.NewGetElementPtr(ei.typ, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := entry.NewLoad(types.I64, tagPtr)
	payloadPtr := entry.NewGetElementPtr(ei.typ, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	release := g.findFunc("soyuz_release")
	doneBlock := dtor.NewBlock("dtor_done")

	current := entry
	for _, hv := range heapVariants {
		releaseBlock := dtor.NewBlock("dtor_" + hv.name)
		skipBlock := dtor.NewBlock("dtor_" + hv.name + "_skip")

		cond := current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, int64(hv.tag)))
		current.NewCondBr(cond, releaseBlock, skipBlock)

		castPtr := releaseBlock.NewBitCast(payloadPtr, types.NewPointer(hv.fieldType))
		fieldVal := releaseBlock.NewLoad(hv.fieldType, castPtr)
		rawPtr := releaseBlock.NewBitCast(fieldVal, types.I8Ptr)
		releaseBlock.NewCall(release, rawPtr)
		releaseBlock.NewBr(skipBlock)

		current = skipBlock
	}
	current.NewBr(doneBlock)
	doneBlock.NewRet(nil)
	return dtor
}

func (g *Generator) generateRecordLiteral(n *parser.RecordLiteral) (value.Value, error) {
	// Generic record: specialize on first use by inferring type params from field values.
	if decl, ok := g.genericRecordDecls[n.Name]; ok {
		return g.generateGenericRecordLiteral(n, decl)
	}

	si, ok := g.structs[n.Name]
	if !ok {
		return nil, fmt.Errorf("undefined struct in codegen: %s", n.Name)
	}
	return g.emitRecordAlloc(n, si, n.Name)
}

// generateGenericRecordLiteral generates a record literal for a generic record type.
// It infers the concrete type substitution from the field value types.
func (g *Generator) generateGenericRecordLiteral(n *parser.RecordLiteral, decl *parser.RecordDecl) (value.Value, error) {
	// Generate all field values first to determine concrete types.
	type fieldEntry struct {
		name string
		val  value.Value
	}
	var fieldEntries []fieldEntry
	for _, f := range n.Fields {
		val, err := g.generateExpr(f.Value)
		if err != nil {
			return nil, err
		}
		fieldEntries = append(fieldEntries, fieldEntry{f.Name, val})
	}

	// Build substitution: generic param name → LLVM type.
	sub := make(map[string]types.Type)
	if len(n.TypeArgs) > 0 {
		for i, gp := range decl.Generics {
			if i < len(n.TypeArgs) {
				sub[gp.Name] = g.mapSoyuzTypeToLLVM(n.TypeArgs[i])
			}
		}
	} else {
	fieldValByName := make(map[string]value.Value)
	for _, fe := range fieldEntries {
		fieldValByName[fe.name] = fe.val
	}
		for _, f := range decl.Fields {
			if nt, ok := f.Type.(*parser.NamedType); ok {
				for _, gp := range decl.Generics {
					if gp.Name == nt.Name {
						if v, ok := fieldValByName[f.Name]; ok {
							if _, already := sub[gp.Name]; !already {
								sub[gp.Name] = v.Type()
							}
						}
					}
				}
			}
		}
	}

	si, err := g.getOrCreateSpecializedRecord(decl, sub)
	if err != nil {
		return nil, err
	}

	// Determine mangled name for destructor lookup.
	mangled := decl.Name
	for _, gp := range decl.Generics {
		if t, ok := sub[gp.Name]; ok {
			mangled += "__" + llvmTypeName(t)
		}
	}

	size := int64(len(si.typ.Fields)) * 8
	if size == 0 {
		size = 8
	}
	var dtorArg value.Value
	if dtor, ok := g.destructors[mangled]; ok {
		dtorArg = g.current.NewBitCast(dtor, types.I8Ptr)
	} else {
		dtorArg = constant.NewNull(types.I8Ptr)
	}
	_ = dtorArg

	raw := g.emitSoyuzAlloc(constant.NewInt(types.I64, size), mangled)
	structPtr := g.current.NewBitCast(raw, types.NewPointer(si.typ))

	for _, fe := range fieldEntries {
		idx, ok := si.fieldIndices[fe.name]
		if !ok {
			return nil, fmt.Errorf("field %s not found in generic struct %s", fe.name, n.Name)
		}
		if g.isHeapType(fe.val.Type()) {
			g.emitRetain(fe.val)
		}
		ptr := g.current.NewGetElementPtr(si.typ, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(idx)))
		g.current.NewStore(fe.val, ptr)
	}
	return structPtr, nil
}

// emitRecordAlloc allocates a non-generic record and stores field values.
func (g *Generator) emitRecordAlloc(n *parser.RecordLiteral, si structInfo, dtorKey string) (value.Value, error) {
	size := int64(len(si.typ.Fields)) * 8
	if size == 0 {
		size = 8
	}

	raw := g.emitSoyuzAlloc(constant.NewInt(types.I64, size), dtorKey)
	structPtr := g.current.NewBitCast(raw, types.NewPointer(si.typ))

	for _, f := range n.Fields {
		idx, ok := si.fieldIndices[f.Name]
		if !ok {
			return nil, fmt.Errorf("field %s not found in struct %s", f.Name, n.Name)
		}
		val, err := g.generateExpr(f.Value)
		if err != nil {
			return nil, err
		}
		if g.isHeapType(val.Type()) {
			g.emitRetain(val)
		}
		ptr := g.current.NewGetElementPtr(si.typ, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(idx)))
		g.current.NewStore(val, ptr)
	}
	return structPtr, nil
}

func (g *Generator) generateMemberExpr(n *parser.MemberExpr) (value.Value, error) {
	// M8: sync guard value read — guard.value → T
	if _, _, ok := g.isSyncGuardValueAccess(n); ok {
		return g.generateSyncGuardRead(n)
	}
	ptr, err := g.generateMemberPtr(n)
	if err != nil {
		return nil, err
	}
	ptrType := ptr.Type().(*types.PointerType)
	return g.current.NewLoad(ptrType.ElemType, ptr), nil
}

func (g *Generator) generateMemberPtr(n *parser.MemberExpr) (value.Value, error) {
	obj, err := g.generateExpr(n.Object)
	if err != nil {
		return nil, err
	}
	ptrType, ok := obj.Type().(*types.PointerType)
	if !ok {
		alloc := g.newAlloca(obj.Type())
		g.current.NewStore(obj, alloc)
		ptrType = types.NewPointer(obj.Type())
		obj = alloc
	}
	st, ok := ptrType.ElemType.(*types.StructType)
	if !ok {
		return nil, fmt.Errorf("member access on non-struct type: %s", ptrType.ElemType)
	}

	// 1. Tuple indexing (numeric property)
	if idx, err := strconv.Atoi(n.Property); err == nil {
		if idx >= 0 && idx < len(st.Fields) {
			return g.current.NewGetElementPtr(st, obj,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(idx))), nil
		}
	}

	// 2. Record field access
	si, ok := g.structs[st.TypeName]
	if !ok {
		return nil, fmt.Errorf("unknown struct type in codegen: %s", st.TypeName)
	}
	idx, ok := si.fieldIndices[n.Property]
	if !ok {
		return nil, fmt.Errorf("field %s not found in struct %s", n.Property, st.TypeName)
	}
	return g.current.NewGetElementPtr(st, obj,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(idx))), nil
}

func (g *Generator) extendSelfLLVM(typeName string) types.Type {
	switch typeName {
	case "String":
		return g.soyuzStringPtrType
	case "Int":
		return types.I64
	case "Float":
		return types.Double
	case "Bool":
		return types.I1
	case "Char":
		return types.I32
	default:
		if si, ok := g.structs[typeName]; ok {
			return types.NewPointer(si.typ)
		}
		return types.I8Ptr
	}
}

func (g *Generator) generateExtendDecl(n *parser.ExtendDecl) error {
	if g.extensionMethods[n.TypeName] == nil {
		g.extensionMethods[n.TypeName] = make(map[string][]*ir.Func)
	}
	selfLLVM := g.extendSelfLLVM(n.TypeName)
	for _, fd := range n.Methods {
		fn := g.declareExtendMethod(n.TypeName, selfLLVM, fd)
		g.extensionMethods[n.TypeName][fd.Name] = append(g.extensionMethods[n.TypeName][fd.Name], fn)
		if fd.Body != nil {
			g.pendingExtendBodies = append(g.pendingExtendBodies, pendingExtendMethodBody{
				typeName: n.TypeName,
				selfLLVM: selfLLVM,
				fd:       fd,
				fn:       fn,
			})
		}
	}
	return nil
}

func (g *Generator) declareExtendMethod(typeName string, selfLLVM types.Type, fd *parser.FuncDecl) *ir.Func {
	ft, _ := g.check.NodeTypes[fd].(*checker.FuncType)
	var retType types.Type = types.Void
	if ft != nil && ft.Return.String() != "Unit" {
		retType = g.mapTypeToLLVM(ft.Return)
	} else if fd.ReturnType != nil {
		retType = g.mapSoyuzTypeToLLVM(fd.ReturnType)
	}
	var params []*ir.Param
	params = append(params, ir.NewParam("__self", types.I8Ptr))
	paramIdx := 0
	for _, p := range fd.Params {
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok && bp.Name == "self" {
			continue
		}
		var pt types.Type
		if ft != nil && paramIdx < len(ft.Params) {
			pt = g.mapTypeToLLVM(ft.Params[paramIdx])
		} else if p.Type != nil {
			pt = g.mapSoyuzTypeToLLVM(p.Type)
		} else {
			pt = types.I64
		}
		name := ""
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok {
			name = bp.Name
		}
		params = append(params, ir.NewParam(name, pt))
		paramIdx++
	}
	return g.module.NewFunc(typeName+"_"+fd.Name, retType, params...)
}

func (g *Generator) generateExtendMethodBody(pm pendingExtendMethodBody) error {
	if pm.fd.Body == nil {
		return nil
	}
	retType := pm.fn.Sig.RetType
	oldCurrent := g.current
	oldVars := g.vars
	oldHeapVars := g.heapVars
	oldScopeStack := g.scopeStack
	oldTaskVarStack2 := g.taskVarStack
	oldSyncGuardStack2 := g.syncGuardStack
	oldArcVarStack2 := g.arcVarStack
	oldBlockNames := g.blockNames
	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack = nil
	g.taskVarStack = nil
	g.syncGuardStack = nil
	g.arcVarStack = nil
	g.blockNames = make(map[string]int)
	defer func() {
		g.current = oldCurrent
		g.vars = oldVars
		g.heapVars = oldHeapVars
		g.scopeStack = oldScopeStack
		g.taskVarStack = oldTaskVarStack2
		g.syncGuardStack = oldSyncGuardStack2
		g.arcVarStack = oldArcVarStack2
		g.blockNames = oldBlockNames
	}()

	g.current = g.newBlock("entry", pm.fn)
	selfTyped := g.current.NewBitCast(pm.fn.Params[0], pm.selfLLVM)
	selfAlloc := g.newAlloca(pm.selfLLVM)
	g.current.NewStore(selfTyped, selfAlloc)
	g.vars["self"] = selfAlloc

	for _, p := range pm.fn.Params[1:] {
		if p.LocalName != "" {
			alloc := g.newAlloca(p.Typ)
			g.current.NewStore(p, alloc)
			g.vars[p.LocalName] = alloc
		}
	}

	val, err := g.generateExpr(pm.fd.Body)
	if err != nil {
		return err
	}
	if retType.Equal(types.Void) {
		if g.current.Term == nil {
			g.current.NewRet(nil)
		}
	} else if g.current.Term == nil {
		if val != nil {
			g.current.NewRet(val)
		} else {
			g.current.NewRet(g.defaultReturnValue(retType))
		}
	}
	return nil
}

// findBuiltin looks up a declared function by name.
func (g *Generator) findBuiltin(name string) value.Value {
	for _, f := range g.module.Funcs {
		if f.Name() == name {
			return f
		}
	}
	panic("builtin not found: " + name)
}

// findFunc looks up a declared function (returns nil if not found).
func (g *Generator) findFunc(name string) *ir.Func {
	for _, f := range g.module.Funcs {
		if f.Name() == name {
			return f
		}
	}
	return nil
}
