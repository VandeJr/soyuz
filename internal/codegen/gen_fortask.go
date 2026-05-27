package codegen

// M-21: `for task binding in iterable { body }` — parallel map.
//
// Desugars into two loops:
//   1. Spawn loop — for each element, spawn a task → collect handles into a handles List.
//   2. Await loop — for each handle, await → collect results into a results List[U].
//
// Body must be a call expression or pipe chain using the binding variable.
// Result type: List[U] where U = return type of the body expression.

import (
	"fmt"
	"sort"

	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// extractForTaskBodyExpr returns the single expression from a for-task body block.
// Returns nil if the body doesn't have exactly one expression statement.
func extractForTaskBodyExpr(body *parser.BlockStmt) parser.Node {
	if len(body.Statements) == 1 {
		switch s := body.Statements[0].(type) {
		case *parser.ExprStmt:
			return s.Expr
		default:
			return s
		}
	}
	return nil
}

// generateForTaskStmt emits LLVM IR for:
//   for task binding in iterable { body_expr }
//
// Two-phase execution:
//   Phase 1 (spawn)  — iterate list, pack args per element, call srt_enqueue.
//   Phase 2 (await)  — iterate handles list, call srt_await, collect results.
//
// Supports body types:
//   • CallExpr  : f(binding) or f(binding, extra_args...)
//   • PipeExpr  : binding |> f |> g  (via pipe wrapper mechanism)
func (g *Generator) generateForTaskStmt(n *parser.ForTaskStmt) (value.Value, error) {
	// ── 1. Evaluate iterable list ──────────────────────────────────────────────
	listVal, err := g.generateExpr(n.Iterable)
	if err != nil {
		return nil, err
	}

	// ── 2. Determine element type ──────────────────────────────────────────────
	var elemCheckerType checker.Type = checker.Unknown
	if st, ok := g.check.NodeTypes[n.Iterable].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		elemCheckerType = st.Params[0]
	}
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)

	// ── 3. Determine result element type ─────────────────────────────────────
	var resultCheckerType checker.Type = checker.Unknown
	if st, ok := g.check.NodeTypes[n].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		resultCheckerType = st.Params[0]
	}
	resultLLVMType := g.mapTypeToLLVM(resultCheckerType)

	// ── 4. Extract body expression ─────────────────────────────────────────────
	bodyExpr := extractForTaskBodyExpr(n.Body)
	if bodyExpr == nil {
		return nil, fmt.Errorf("for task: body deve conter exatamente uma expressão")
	}

	// ── 5. Set up binding alloca (needed before wrapper gen so collectCaptures finds it) ──
	var zeroVal value.Value
	if elemLLVMType.Equal(types.I64) {
		zeroVal = constant.NewInt(types.I64, 0)
	} else if elemLLVMType.Equal(types.Double) {
		zeroVal = constant.NewFloat(types.Double, 0)
	} else if elemLLVMType.Equal(types.I1) {
		zeroVal = constant.False
	} else {
		if pt, ok2 := elemLLVMType.(*types.PointerType); ok2 {
			zeroVal = constant.NewNull(pt)
		} else {
			zeroVal = constant.NewNull(types.I8Ptr)
		}
	}
	bindAlloc := g.newAlloca(elemLLVMType)
	g.current.NewStore(zeroVal, bindAlloc)
	g.vars[n.Binding] = bindAlloc

	// ── 6. Generate task wrapper (once, before the spawn loop) ─────────────────
	var wrapperFn *ir.Func
	var capturedNames []string // for pipe mode
	var callArgNodes []parser.Node // for call mode

	switch body := bodyExpr.(type) {
	case *parser.CallExpr:
		// Call mode: f(binding) or f(binding, extra_args...)
		id, ok := body.Callee.(*parser.Identifier)
		if !ok {
			return nil, fmt.Errorf("for task: callee deve ser uma função nomeada")
		}
		targetFunc := g.findFunc(id.Name)
		if targetFunc == nil {
			if st, ok2 := g.check.Specializations[body]; ok2 {
				mangled := id.Name
				for _, p := range st.Params {
					mangled += "__" + p.String()
				}
				targetFunc = g.specialized[mangled]
			}
		}
		if targetFunc == nil {
			return nil, fmt.Errorf("for task: função '%s' não encontrada", id.Name)
		}
		// Determine LLVM arg types from checker nodeTypes (no side-effects).
		numArgs := len(body.Args)
		origArgTypes := make([]types.Type, numArgs)
		for i, argNode := range body.Args {
			ct := g.check.NodeTypes[argNode]
			origArgTypes[i] = g.mapTypeToLLVM(ct)
		}
		wrapperFn = g.generateTaskWrapperFunc(targetFunc, origArgTypes, numArgs)
		callArgNodes = body.Args

	case *parser.PipeExpr, *parser.PipeQuestExpr:
		// Pipe mode: binding |> f |> g
		// Collect free variables from the pipe (binding + any outer vars).
		rawCaptures := g.collectTaskCaptures(bodyExpr)
		capVals := make([]value.Value, 0, len(rawCaptures))
		filtered := make([]string, 0, len(rawCaptures))
		for _, name := range rawCaptures {
			alloc, ok := g.vars[name]
			if !ok {
				continue
			}
			ptr, ok2 := alloc.Type().(*types.PointerType)
			if !ok2 {
				continue
			}
			// Load a dummy value just to get the LLVM type for the wrapper.
			dummy := g.current.NewLoad(ptr.ElemType, alloc)
			capVals = append(capVals, dummy)
			filtered = append(filtered, name)
		}
		// Ensure deterministic ordering.
		type capPair struct {
			name string
			val  value.Value
		}
		pairs := make([]capPair, len(filtered))
		for i := range filtered {
			pairs[i] = capPair{filtered[i], capVals[i]}
		}
		sort.Slice(pairs, func(a, b int) bool { return pairs[a].name < pairs[b].name })
		capturedNames = make([]string, len(pairs))
		sortedCapVals := make([]value.Value, len(pairs))
		for i, p := range pairs {
			capturedNames[i] = p.name
			sortedCapVals[i] = p.val
		}
		wrapperFn = g.generateTaskPipeWrapperFunc(bodyExpr, capturedNames, sortedCapVals)

	default:
		return nil, fmt.Errorf("for task: body deve ser call expression ou pipe chain, obtido %T", bodyExpr)
	}

	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)

	// ── 7. Create handles list (stores i8* task handles) ──────────────────────
	dtorPrim := g.findFunc("soyuz_list_dtor_primitive")
	handlesRaw := g.current.NewCall(g.findFunc("soyuz_list_new"),
		constant.NewInt(types.I64, 0),
		g.current.NewBitCast(dtorPrim, types.I8Ptr))
	handlesI8 := g.current.NewBitCast(handlesRaw, types.I8Ptr)

	// ── 8. Spawn loop: iterate list elements, spawn a task per element ─────────
	fn := g.current.Parent
	listPtr := types.NewPointer(g.structs["SoyuzList"].typ)
	listTyped := g.current.NewBitCast(listVal, listPtr)
	sizeGEP := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size := g.current.NewLoad(types.I64, sizeGEP)

	iAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), iAlloc)

	spawnCond := g.newBlock("ft_spawn_cond", fn)
	spawnBody := g.newBlock("ft_spawn_body", fn)
	spawnIncr := g.newBlock("ft_spawn_incr", fn)
	spawnAfter := g.newBlock("ft_spawn_after", fn)
	g.current.NewBr(spawnCond)

	// spawn_cond: if i < size goto body else after
	g.current = spawnCond
	iVal := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, iVal, size)
	g.current.NewCondBr(cond, spawnBody, spawnAfter)

	// spawn_body: get element, update binding, pack args, enqueue, append handle
	g.current = spawnBody
	listAsI8 := g.current.NewBitCast(listTyped, types.I8Ptr)
	iLoad := g.current.NewLoad(types.I64, iAlloc)
	rawElem := g.current.NewCall(g.findFunc("soyuz_list_get"), listAsI8, iLoad)
	// Convert raw i8* to elemLLVMType
	var elem value.Value
	if elemLLVMType.Equal(types.I64) {
		elem = g.current.NewPtrToInt(rawElem, types.I64)
	} else if elemLLVMType.Equal(types.Double) {
		i64v := g.current.NewPtrToInt(rawElem, types.I64)
		elem = g.current.NewBitCast(i64v, types.Double)
	} else if elemLLVMType.Equal(types.I1) {
		i64v := g.current.NewPtrToInt(rawElem, types.I64)
		elem = g.current.NewTrunc(i64v, types.I1)
	} else {
		elem = g.current.NewBitCast(rawElem, elemLLVMType)
	}
	// Store current element into binding alloca.
	g.current.NewStore(elem, bindAlloc)

	// Pack args into a heap buffer and enqueue.
	var handle value.Value
	switch bodyExpr.(type) {
	case *parser.CallExpr:
		// Evaluate all call args (binding is now set to current element).
		callArgs := make([]value.Value, len(callArgNodes))
		for i, argNode := range callArgNodes {
			v, err := g.generateExpr(argNode)
			if err != nil {
				return nil, err
			}
			callArgs[i] = v
		}
		numArgs := len(callArgs)
		var argsHeap value.Value
		if numArgs > 0 {
			argsHeap = g.current.NewCall(g.findBuiltin("malloc"),
				constant.NewInt(types.I64, int64(numArgs*8)))
			arrType := types.NewArray(uint64(numArgs), types.I64)
			argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
			for i, a := range callArgs {
				slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
				g.current.NewStore(g.castToI64(a), slotPtr)
			}
		} else {
			argsHeap = constant.NewNull(types.I8Ptr)
		}
		handle = g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)

	default: // pipe mode
		// Re-collect capture values with the updated binding.
		numCaps := len(capturedNames)
		var argsHeap value.Value
		if numCaps > 0 {
			argsHeap = g.current.NewCall(g.findBuiltin("malloc"),
				constant.NewInt(types.I64, int64(numCaps*8)))
			arrType := types.NewArray(uint64(numCaps), types.I64)
			argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
			for i, capName := range capturedNames {
				alloc := g.vars[capName]
				ptr := alloc.Type().(*types.PointerType)
				v := g.current.NewLoad(ptr.ElemType, alloc)
				slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
				g.current.NewStore(g.castToI64(v), slotPtr)
			}
		} else {
			argsHeap = constant.NewNull(types.I8Ptr)
		}
		handle = g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
	}

	// Append task handle to handles list.
	g.current.NewCall(g.findFunc("soyuz_list_append"), handlesI8, handle)
	g.current.NewBr(spawnIncr)

	// spawn_incr: i++
	g.current = spawnIncr
	cur := g.current.NewLoad(types.I64, iAlloc)
	next := g.current.NewAdd(cur, constant.NewInt(types.I64, 1))
	g.current.NewStore(next, iAlloc)
	g.current.NewBr(spawnCond)

	// ── 9. After spawn loop: clean up binding ──────────────────────────────────
	g.current = spawnAfter
	delete(g.vars, n.Binding)

	// ── 10. Create results list ────────────────────────────────────────────────
	var resultsDtorName string
	if g.isHeapType(resultLLVMType) {
		resultsDtorName = "soyuz_list_dtor_rc"
	} else {
		resultsDtorName = "soyuz_list_dtor_primitive"
	}
	resultsDtor := g.findFunc(resultsDtorName)
	resultsRaw := g.current.NewCall(g.findFunc("soyuz_list_new"),
		constant.NewInt(types.I64, 0),
		g.current.NewBitCast(resultsDtor, types.I8Ptr))
	resultsI8 := g.current.NewBitCast(resultsRaw, types.I8Ptr)

	// ── 11. Await loop: for each handle, await and collect result ──────────────
	jAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), jAlloc)

	// Get handles list size (= same as iterable list size).
	handlesTyped := g.current.NewBitCast(handlesRaw, listPtr)
	handlesSizeGEP := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, handlesTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handlesSize := g.current.NewLoad(types.I64, handlesSizeGEP)

	awaitCond := g.newBlock("ft_await_cond", fn)
	awaitBody := g.newBlock("ft_await_body", fn)
	awaitIncr := g.newBlock("ft_await_incr", fn)
	awaitAfter := g.newBlock("ft_await_after", fn)
	g.current.NewBr(awaitCond)

	// await_cond: if j < handlesSize goto body else after
	g.current = awaitCond
	jVal := g.current.NewLoad(types.I64, jAlloc)
	awaitCondVal := g.current.NewICmp(enum.IPredSLT, jVal, handlesSize)
	g.current.NewCondBr(awaitCondVal, awaitBody, awaitAfter)

	// await_body: get handle, await, coerce result, append to results
	g.current = awaitBody
	jLoad := g.current.NewLoad(types.I64, jAlloc)
	rawHandle := g.current.NewCall(g.findFunc("soyuz_list_get"), handlesI8, jLoad)
	// rawHandle is already i8* (task handle)
	rawResult := g.current.NewCall(g.findFunc("srt_await"), rawHandle)
	// Coerce rawResult (i8*) to resultLLVMType
	var resultVal value.Value
	if resultLLVMType.Equal(types.I64) {
		resultVal = g.current.NewPtrToInt(rawResult, types.I64)
	} else if resultLLVMType.Equal(types.Double) {
		i64v := g.current.NewPtrToInt(rawResult, types.I64)
		resultVal = g.current.NewBitCast(i64v, types.Double)
	} else if resultLLVMType.Equal(types.I1) {
		i64v := g.current.NewPtrToInt(rawResult, types.I64)
		resultVal = g.current.NewTrunc(i64v, types.I1)
	} else {
		resultVal = g.current.NewBitCast(rawResult, resultLLVMType)
	}
	// Append to results list.
	var resultValI8 value.Value
	if resultVal.Type().Equal(types.I64) {
		resultValI8 = g.current.NewIntToPtr(resultVal, types.I8Ptr)
	} else {
		resultValI8 = g.current.NewBitCast(resultVal, types.I8Ptr)
	}
	if g.isHeapType(resultLLVMType) {
		g.emitRetain(resultVal)
	}
	g.current.NewCall(g.findFunc("soyuz_list_append"), resultsI8, resultValI8)
	g.current.NewBr(awaitIncr)

	// await_incr: j++
	g.current = awaitIncr
	jCur := g.current.NewLoad(types.I64, jAlloc)
	jNext := g.current.NewAdd(jCur, constant.NewInt(types.I64, 1))
	g.current.NewStore(jNext, jAlloc)
	g.current.NewBr(awaitCond)

	// ── 12. Return results list ────────────────────────────────────────────────
	g.current = awaitAfter

	// Cast results list to the typed List[U] pointer.
	listST := g.check.NodeTypes[n]
	if listST != nil {
		llvmListType := g.mapTypeToLLVM(listST)
		if llvmListType != nil && !llvmListType.Equal(types.I8Ptr) {
			return g.current.NewBitCast(resultsRaw, llvmListType), nil
		}
	}
	return resultsRaw, nil
}
