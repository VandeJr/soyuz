package codegen

import (
	"fmt"
	"maps"
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

func (g *Generator) generateFuncDecl(n *parser.FuncDecl) error {
	var retType types.Type = types.Void
	if n.ReturnType != nil {
		retType = g.mapSoyuzTypeToLLVM(n.ReturnType)
	}
	var params []*ir.Param
	for _, p := range n.Params {
		pt := g.mapSoyuzTypeToLLVM(p.Type)
		name := ""
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok {
			name = bp.Name
		}
		params = append(params, ir.NewParam(name, pt))
	}
	fn := g.module.NewFunc(n.Name, retType, params...)
	if n.Body != nil {
		var checkerRetType checker.Type
		if ft, ok := g.check.NodeTypes[n].(*checker.FuncType); ok {
			checkerRetType = ft.Return
		}
		oldReturnType := g.currentReturnType
		g.currentReturnType = checkerRetType
		defer func() { g.currentReturnType = oldReturnType }()

		g.blockNames = make(map[string]int)
		g.current = g.newBlock("entry", fn)
		for _, p := range params {
			if p.LocalName != "" {
				alloc := g.newAlloca(p.Typ)
				g.current.NewStore(p, alloc)
				g.vars[p.LocalName] = alloc
			}
		}
		val, err := g.generateExpr(n.Body)
		if err != nil {
			return err
		}
		if retType.Equal(types.Void) {
			if g.current.Term == nil {
				g.current.NewRet(nil)
			}
		} else if g.current.Term == nil {
			val, err = g.coerceToInterfaceReturn(val, checkerRetType)
			if err != nil {
				return err
			}
			g.current.NewRet(val)
		}
	}
	return nil
}

func (g *Generator) generateSpecializedFunc(name string, n *parser.FuncDecl, specializedFuncType *checker.FuncType) (*ir.Func, error) {
	mangled := name
	for _, p := range specializedFuncType.Params {
		mangled += "__" + p.String()
	}

	if f, ok := g.specialized[mangled]; ok {
		return f, nil
	}

	retType := g.mapTypeToLLVM(specializedFuncType.Return)
	var params []*ir.Param
	for i, p := range specializedFuncType.Params {
		pt := g.mapTypeToLLVM(p)
		paramName := ""
		if bp, ok := n.Params[i].Pattern.(*parser.BindingPattern); ok {
			paramName = bp.Name
		}
		params = append(params, ir.NewParam(paramName, pt))
	}

	fn := g.module.NewFunc(mangled, retType, params...)
	g.specialized[mangled] = fn

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
	g.pushScope()
	for _, p := range params {
		if p.LocalName != "" {
			alloc := g.newAlloca(p.Typ)
			g.current.NewStore(p, alloc)
			g.vars[p.LocalName] = alloc
			if g.isHeapType(p.Typ) {
				g.heapVars[p.LocalName] = true
				g.ownVar(p.LocalName)
			}
		}
	}

	val, err := g.generateExpr(n.Body)
	if err != nil {
		return nil, err
	}

	val = g.prepareReturn(val)
	g.releaseAllScopes()

	if retType.Equal(types.Void) {
		if g.current.Term == nil {
			g.current.NewRet(nil)
		}
	} else if g.current.Term == nil {
		g.current.NewRet(val)
	}

	g.popScopeAndRelease()

	return fn, nil
}

func (g *Generator) generateArrowFunc(n *parser.ArrowFunc) (value.Value, error) {
	ft, ok := g.check.NodeTypes[n].(*checker.FuncType)
	if !ok {
		return nil, fmt.Errorf("missing type for arrow func in codegen")
	}

	captures := g.check.Captures[n] // names of captured outer-scope variables

	// Build env struct type from the outer vars (before entering inner context).
	var envFieldTypes []types.Type
	var envStructType *types.StructType
	if len(captures) > 0 {
		for _, capName := range captures {
			if alloc, ok := g.vars[capName]; ok {
				ptrType := alloc.Type().(*types.PointerType)
				envFieldTypes = append(envFieldTypes, ptrType.ElemType)
			}
		}
		envStructType = types.NewStruct(envFieldTypes...)
	}

	// Generate the lambda function body (saves/restores outer codegen state).
	fn, err := g.generateLambdaFunc(n, ft, captures, envStructType, envFieldTypes)
	if err != nil {
		return nil, err
	}

	// Back in outer context: allocate closure struct and env struct.
	return g.buildClosureValue(fn, captures, envStructType, envFieldTypes)
}

// generateLambdaFunc emits the LLVM function for an arrow func.
// Signature: (i8* __env, original_params...) → retType
// Captured variables are loaded from __env at function entry.
func (g *Generator) generateLambdaFunc(n *parser.ArrowFunc, ft *checker.FuncType, captures []string, envStructType *types.StructType, envFieldTypes []types.Type) (*ir.Func, error) {
	name := fmt.Sprintf("__lambda_%d", len(g.module.Funcs))
	retType := g.mapTypeToLLVM(ft.Return)

	var params []*ir.Param
	params = append(params, ir.NewParam("__env", types.I8Ptr))
	for i, p := range ft.Params {
		pt := g.mapTypeToLLVM(p)
		paramName := ""
		if bp, ok := n.Params[i].Pattern.(*parser.BindingPattern); ok {
			paramName = bp.Name
		}
		params = append(params, ir.NewParam(paramName, pt))
	}

	fn := g.module.NewFunc(name, retType, params...)

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

	g.current = g.newBlock("entry", fn)
	g.pushScope()

	// Load captured variables from env struct.
	if envStructType != nil {
		envStructPtr := g.current.NewBitCast(fn.Params[0], types.NewPointer(envStructType))
		for i, capName := range captures {
			ft := envFieldTypes[i]
			gep := g.current.NewGetElementPtr(envStructType, envStructPtr,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
			loaded := g.current.NewLoad(ft, gep)
			alloc := g.newAlloca(ft)
			g.current.NewStore(loaded, alloc)
			g.vars[capName] = alloc
			if g.isHeapType(ft) {
				g.emitRetain(loaded) // borrow from env: retain on entry, released by scope pop on exit
				g.heapVars[capName] = true
				g.ownVar(capName)
			}
		}
	}

	// Store original parameters (params[1:] skips the env param).
	for _, p := range fn.Params[1:] {
		if p.LocalName != "" {
			alloc := g.newAlloca(p.Typ)
			g.current.NewStore(p, alloc)
			g.vars[p.LocalName] = alloc
			if g.isHeapType(p.Typ) {
				g.heapVars[p.LocalName] = true
				g.ownVar(p.LocalName)
			}
		}
	}

	val, err := g.generateExpr(n.Body)
	if err != nil {
		return nil, err
	}

	val = g.prepareReturn(val)
	g.releaseAllScopes()

	if retType.Equal(types.Void) {
		if g.current.Term == nil {
			g.current.NewRet(nil)
		}
	} else if g.current.Term == nil {
		g.current.NewRet(val)
	}

	g.popScopeAndRelease()
	return fn, nil
}

// buildClosureValue allocates the SoyuzClosure struct and (if there are captures)
// the env struct in the current (outer) block, and returns the closure as i8*.
func (g *Generator) buildClosureValue(fn *ir.Func, captures []string, envStructType *types.StructType, envFieldTypes []types.Type) (value.Value, error) {
	var envPtrRaw value.Value

	if len(captures) > 0 && envStructType != nil {
		envSize := int64(len(envFieldTypes)) * 8
		if envSize == 0 {
			envSize = 8
		}
		envDtorArg := value.Value(constant.NewNull(types.I8Ptr))
		for _, ft := range envFieldTypes {
			if g.isHeapType(ft) {
				g.envDtorCounter++
				envDtor := g.generateEnvDtor(fmt.Sprintf("env_%d", g.envDtorCounter), envFieldTypes)
				envDtorArg = g.current.NewBitCast(envDtor, types.I8Ptr)
				break
			}
		}
		envRaw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
			constant.NewInt(types.I64, envSize), envDtorArg, constant.NewNull(types.I8Ptr))
		envStructPtr := g.current.NewBitCast(envRaw, types.NewPointer(envStructType))

		for i, capName := range captures {
			if alloc, ok := g.vars[capName]; ok {
				loaded := g.current.NewLoad(envFieldTypes[i], alloc)
				if g.isHeapType(envFieldTypes[i]) {
					g.emitRetain(loaded)
				}
				gep := g.current.NewGetElementPtr(envStructType, envStructPtr,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
				g.current.NewStore(loaded, gep)
			}
		}
		envPtrRaw = g.current.NewBitCast(envStructPtr, types.I8Ptr)
	} else {
		envPtrRaw = constant.NewNull(types.I8Ptr)
	}

	// Allocate SoyuzClosure{ fn_ptr: i8*, env_ptr: i8* }
	closureDtor := g.getOrCreateClosureDtor()
	closureDtorArg := g.current.NewBitCast(closureDtor, types.I8Ptr)
	closureRaw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 16), closureDtorArg, constant.NewNull(types.I8Ptr))
	closurePtr := g.current.NewBitCast(closureRaw, types.NewPointer(g.closureType))

	fnRaw := g.current.NewBitCast(fn, types.I8Ptr)
	fnField := g.current.NewGetElementPtr(g.closureType, closurePtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(fnRaw, fnField)

	envField := g.current.NewGetElementPtr(g.closureType, closurePtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(envPtrRaw, envField)

	return g.current.NewBitCast(closurePtr, types.I8Ptr), nil
}

func (g *Generator) declareFuncVariants(name string, variants []*parser.FuncDecl) {
	if len(variants) == 0 {
		return
	}

	first := variants[0]
	if len(first.Generics) > 0 {
		g.genericDecls[name] = first
		return
	}

	ft, ok := g.check.NodeTypes[first].(*checker.FuncType)
	if !ok {
		var retType types.Type = types.Void
		if first.ReturnType != nil {
			retType = g.mapSoyuzTypeToLLVM(first.ReturnType)
		}
		var params []*ir.Param
		for i, p := range first.Params {
			pt := g.mapSoyuzTypeToLLVM(p.Type)
			params = append(params, ir.NewParam(fmt.Sprintf("arg%d", i), pt))
		}
		g.module.NewFunc(name, retType, params...)
		return
	}

	retType := g.mapTypeToLLVM(ft.Return)
	var params []*ir.Param
	for i, p := range ft.Params {
		pt := g.mapTypeToLLVM(p)
		params = append(params, ir.NewParam(fmt.Sprintf("arg%d", i), pt))
	}
	g.module.NewFunc(name, retType, params...)
}

func (g *Generator) generateFuncVariantsBody(name string, variants []*parser.FuncDecl) error {
	if len(variants) == 0 {
		return nil
	}

	// Generic functions are monomorphized on demand — skip here.
	if len(variants[0].Generics) > 0 {
		return nil
	}

	fn := g.findFunc(name)
	if fn == nil {
		return fmt.Errorf("function %s not declared", name)
	}

	var checkerRetType checker.Type
	if ft, ok := g.check.NodeTypes[variants[0]].(*checker.FuncType); ok {
		checkerRetType = ft.Return
	}

	oldReturnType := g.currentReturnType
	g.currentReturnType = checkerRetType
	defer func() { g.currentReturnType = oldReturnType }()

	g.blockNames = make(map[string]int)
	g.current = g.newBlock("entry", fn)
	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	params := fn.Params
	retType := fn.Sig.RetType

	// Reset RC scope state for this function and push base scope for parameters.
	g.scopeStack = nil
	g.taskVarStack = nil
	g.syncGuardStack = nil
	g.arcVarStack = nil
	g.pushScope()

	for i, p := range params {
		paramName := fmt.Sprintf("arg%d", i)
		alloc := g.newAlloca(p.Typ)
		g.current.NewStore(p, alloc)
		g.vars[paramName] = alloc
		if g.isHeapType(p.Typ) {
			g.heapVars[paramName] = true
			g.ownVar(paramName)
		}
	}

	for i, v := range variants {
		variantNext := g.newBlock(fmt.Sprintf("variant_%d_next", i), fn)
		variantBody := g.newBlock(fmt.Sprintf("variant_%d_body", i), fn)

		oldVars := maps.Clone(g.vars)
		oldHeapVars := maps.Clone(g.heapVars)

		for j, p := range v.Params {
			if bp, ok := p.Pattern.(*parser.BindingPattern); ok {
				g.vars[bp.Name] = g.vars[fmt.Sprintf("arg%d", j)]
				if g.isHeapType(params[j].Typ) {
					g.heapVars[bp.Name] = true
				}
			}
			matchOk := g.newBlock(fmt.Sprintf("v%d_p%d_ok", i, j), fn)
			if err := g.matchPattern(params[j], nil, p.Pattern, matchOk, variantNext); err != nil {
				return err
			}
			g.current = matchOk
		}

		if v.WhenGuard != nil {
			guardOk := g.newBlock(fmt.Sprintf("v%d_guard_ok", i), fn)
			guardVal, err := g.generateExpr(v.WhenGuard)
			if err != nil {
				return err
			}
			// Se o guard for falso, pula para o próximo variant (variantNext)
			g.current.NewCondBr(guardVal, guardOk, variantNext)
			g.current = guardOk
		}

		if g.current.Term == nil {
			g.current.NewBr(variantBody)
		}

		g.current = variantBody
		val, err := g.generateExpr(v.Body)
		if err != nil {
			return err
		}

		val, err = g.coerceToInterfaceReturn(val, checkerRetType)
		if err != nil {
			return err
		}
		val = g.prepareReturn(val)
		g.releaseAllScopes()

		if retType.Equal(types.Void) {
			if g.current.Term == nil {
				g.current.NewRet(nil)
			}
		} else if g.current.Term == nil {
			g.current.NewRet(val)
		}

		g.vars = oldVars
		g.heapVars = oldHeapVars
		g.current = variantNext
	}

	// Fallthrough: no variant matched — return zero value or void.
	if g.current.Term == nil {
		g.releaseAllScopes()
		if retType.Equal(types.Void) {
			g.current.NewRet(nil)
		} else {
			g.current.NewRet(g.defaultReturnValue(retType))
		}
	}

	g.popScopeAndRelease()
	return nil
}

func (g *Generator) ensureClosureType() {
	if g.closureType == nil {
		g.closureType = g.module.NewTypeDef("SoyuzClosure",
			types.NewStruct(types.I8Ptr, types.I8Ptr)).(*types.StructType)
	}
}

func (g *Generator) getOrCreateClosureDtor() *ir.Func {
	if g.closureDtor != nil {
		return g.closureDtor
	}
	g.ensureClosureType()
	dtor := g.module.NewFunc("__soyuz_dtor_closure", types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")
	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(g.closureType))
	envField := entry.NewGetElementPtr(g.closureType, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	env := entry.NewLoad(types.I8Ptr, envField)
	isNull := entry.NewICmp(enum.IPredEQ, env, constant.NewNull(types.I8Ptr))
	releaseBlock := dtor.NewBlock("release")
	doneBlock := dtor.NewBlock("done")
	entry.NewCondBr(isNull, doneBlock, releaseBlock)
	releaseBlock.NewCall(g.findFunc("soyuz_release"), env)
	releaseBlock.NewBr(doneBlock)
	doneBlock.NewRet(nil)
	g.closureDtor = dtor
	return dtor
}

func (g *Generator) generateEnvDtor(name string, fieldTypes []types.Type) *ir.Func {
	dtor := g.module.NewFunc("__soyuz_dtor_"+name, types.Void, ir.NewParam("ptr", types.I8Ptr))
	entry := dtor.NewBlock("entry")
	envStruct := types.NewStruct(fieldTypes...)
	typedPtr := entry.NewBitCast(dtor.Params[0], types.NewPointer(envStruct))
	release := g.findFunc("soyuz_release")
	for i, ft := range fieldTypes {
		if !g.isHeapType(ft) {
			continue
		}
		gep := entry.NewGetElementPtr(envStruct, typedPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
		fieldVal := entry.NewLoad(ft, gep)
		rawPtr := entry.NewBitCast(fieldVal, types.I8Ptr)
		entry.NewCall(release, rawPtr)
	}
	entry.NewRet(nil)
	return dtor
}
