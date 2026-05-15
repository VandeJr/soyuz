package codegen

import (
	"fmt"
	"maps"
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
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
	oldBlockNames := g.blockNames
	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack = nil
	g.blockNames = make(map[string]int)
	defer func() {
		g.current = oldCurrent
		g.vars = oldVars
		g.heapVars = oldHeapVars
		g.scopeStack = oldScopeStack
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
	oldBlockNames := g.blockNames
	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack = nil
	g.blockNames = make(map[string]int)
	defer func() {
		g.current = oldCurrent
		g.vars = oldVars
		g.heapVars = oldHeapVars
		g.scopeStack = oldScopeStack
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
		envRaw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
			constant.NewInt(types.I64, envSize), constant.NewNull(types.I8Ptr))
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
	closureRaw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 16), constant.NewNull(types.I8Ptr))
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

	g.blockNames = make(map[string]int)
	g.current = g.newBlock("entry", fn)
	params := fn.Params
	retType := fn.Sig.RetType

	// Reset RC scope state for this function and push base scope for parameters.
	g.scopeStack = nil
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
			if err := g.matchPattern(params[j], p.Pattern, matchOk, variantNext); err != nil {
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
