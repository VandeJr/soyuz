package codegen

// Task.gather(list: List[T], fn: T -> U) -> List[U]
//
// Parallel map: spawns fn(item) for each item in list (as independent tasks),
// awaits all in order, and returns List[U] with results in the original order.
//
// Implementation mirrors the old for-task desugaring:
//   Phase 1 (spawn) — iterate list, pack args, srt_enqueue → handles list.
//   Phase 2 (await) — iterate handles, srt_await → results list.

import (
	"fmt"

	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// generateTaskGather emits IR for Task.gather(list, fn).
func (g *Generator) generateTaskGather(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) != 2 {
		return nil, fmt.Errorf("Task.gather: esperado 2 argumentos")
	}

	// ── 1. Evaluate the list ──────────────────────────────────────────────────
	listVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}

	// ── 2. Determine element type from checker ────────────────────────────────
	var elemCheckerType checker.Type = checker.Unknown
	if st, ok := g.check.NodeTypes[n.Args[0]].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		elemCheckerType = st.Params[0]
	}
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)

	// ── 3. Determine result element type from checker ─────────────────────────
	var resultCheckerType checker.Type = checker.Unknown
	if st, ok := g.check.NodeTypes[n].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		resultCheckerType = st.Params[0]
	}
	resultLLVMType := g.mapTypeToLLVM(resultCheckerType)

	// ── 4. Evaluate fn arg (must be a named function identifier) ─────────────
	var targetFnName string
	switch fn := n.Args[1].(type) {
	case *parser.Identifier:
		targetFnName = fn.Name
	default:
		return nil, fmt.Errorf("Task.gather: segundo argumento deve ser uma função nomeada")
	}
	targetFunc := g.findFunc(targetFnName)
	if targetFunc == nil {
		if st, ok := g.check.Specializations[n.Args[1]]; ok {
			mangled := targetFnName
			for _, p := range st.Params {
				mangled += "__" + p.String()
			}
			targetFunc = g.specialized[mangled]
		}
	}
	if targetFunc == nil {
		return nil, fmt.Errorf("Task.gather: função '%s' não encontrada", targetFnName)
	}

	// ── 5. Build the task wrapper (single arg: the element) ───────────────────
	elemLLVMTypes := []types.Type{elemLLVMType}
	wrapperFn := g.generateTaskWrapperFunc(targetFunc, elemLLVMTypes, 1)
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)

	// ── 6. Create handles list ────────────────────────────────────────────────
	dtorPrim := g.findFunc("soyuz_list_dtor_primitive")
	handlesRaw := g.current.NewCall(g.findFunc("soyuz_list_new"),
		constant.NewInt(types.I64, 0),
		g.current.NewBitCast(dtorPrim, types.I8Ptr))
	handlesI8 := g.current.NewBitCast(handlesRaw, types.I8Ptr)

	// ── 7. Spawn loop ─────────────────────────────────────────────────────────
	fn2 := g.current.Parent
	listPtr := types.NewPointer(g.structs["SoyuzList"].typ)
	listTyped := g.current.NewBitCast(listVal, listPtr)
	sizeGEP := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size := g.current.NewLoad(types.I64, sizeGEP)

	iAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), iAlloc)

	spawnCond := g.newBlock("gather_spawn_cond", fn2)
	spawnBody := g.newBlock("gather_spawn_body", fn2)
	spawnIncr := g.newBlock("gather_spawn_incr", fn2)
	spawnAfter := g.newBlock("gather_spawn_after", fn2)
	g.current.NewBr(spawnCond)

	g.current = spawnCond
	iVal := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, iVal, size)
	g.current.NewCondBr(cond, spawnBody, spawnAfter)

	g.current = spawnBody
	listAsI8 := g.current.NewBitCast(listTyped, types.I8Ptr)
	iLoad := g.current.NewLoad(types.I64, iAlloc)
	rawElem := g.current.NewCall(g.findFunc("soyuz_list_get"), listAsI8, iLoad)

	var elem value.Value
	switch {
	case elemLLVMType.Equal(types.I64):
		elem = g.current.NewPtrToInt(rawElem, types.I64)
	case elemLLVMType.Equal(types.Double):
		i64v := g.current.NewPtrToInt(rawElem, types.I64)
		elem = g.current.NewBitCast(i64v, types.Double)
	case elemLLVMType.Equal(types.I1):
		i64v := g.current.NewPtrToInt(rawElem, types.I64)
		elem = g.current.NewTrunc(i64v, types.I1)
	default:
		elem = g.current.NewBitCast(rawElem, elemLLVMType)
	}

	argsHeap := g.current.NewCall(g.findBuiltin("malloc"), constant.NewInt(types.I64, 8))
	arrType := types.NewArray(1, types.I64)
	argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
	slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(g.castToI64(elem), slotPtr)

	handle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
	g.current.NewCall(g.findFunc("soyuz_list_append"), handlesI8, handle)
	g.current.NewBr(spawnIncr)

	g.current = spawnIncr
	cur := g.current.NewLoad(types.I64, iAlloc)
	next := g.current.NewAdd(cur, constant.NewInt(types.I64, 1))
	g.current.NewStore(next, iAlloc)
	g.current.NewBr(spawnCond)

	g.current = spawnAfter

	// ── 8. Create results list ────────────────────────────────────────────────
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

	// ── 9. Await loop ─────────────────────────────────────────────────────────
	jAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), jAlloc)

	handlesTyped := g.current.NewBitCast(handlesRaw, listPtr)
	handlesSizeGEP := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, handlesTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handlesSize := g.current.NewLoad(types.I64, handlesSizeGEP)

	awaitCond := g.newBlock("gather_await_cond", fn2)
	awaitBody := g.newBlock("gather_await_body", fn2)
	awaitIncr := g.newBlock("gather_await_incr", fn2)
	awaitAfter := g.newBlock("gather_await_after", fn2)
	g.current.NewBr(awaitCond)

	g.current = awaitCond
	jVal := g.current.NewLoad(types.I64, jAlloc)
	awaitCondVal := g.current.NewICmp(enum.IPredSLT, jVal, handlesSize)
	g.current.NewCondBr(awaitCondVal, awaitBody, awaitAfter)

	g.current = awaitBody
	jLoad := g.current.NewLoad(types.I64, jAlloc)
	rawHandle := g.current.NewCall(g.findFunc("soyuz_list_get"), handlesI8, jLoad)
	rawResult := g.current.NewCall(g.findFunc("srt_await"), rawHandle)

	var resultVal value.Value
	switch {
	case resultLLVMType.Equal(types.I64):
		resultVal = g.current.NewPtrToInt(rawResult, types.I64)
	case resultLLVMType.Equal(types.Double):
		i64v := g.current.NewPtrToInt(rawResult, types.I64)
		resultVal = g.current.NewBitCast(i64v, types.Double)
	case resultLLVMType.Equal(types.I1):
		i64v := g.current.NewPtrToInt(rawResult, types.I64)
		resultVal = g.current.NewTrunc(i64v, types.I1)
	default:
		resultVal = g.current.NewBitCast(rawResult, resultLLVMType)
	}

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

	g.current = awaitIncr
	jCur := g.current.NewLoad(types.I64, jAlloc)
	jNext := g.current.NewAdd(jCur, constant.NewInt(types.I64, 1))
	g.current.NewStore(jNext, jAlloc)
	g.current.NewBr(awaitCond)

	// ── 10. Return results list ───────────────────────────────────────────────
	g.current = awaitAfter
	if listST := g.check.NodeTypes[n]; listST != nil {
		llvmListType := g.mapTypeToLLVM(listST)
		if llvmListType != nil && !llvmListType.Equal(types.I8Ptr) {
			return g.current.NewBitCast(resultsRaw, llvmListType), nil
		}
	}
	return resultsRaw, nil
}
