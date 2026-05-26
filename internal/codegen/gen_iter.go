package codegen

import (
	"fmt"

	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

func (g *Generator) iteratorStruct() *types.StructType {
	return g.structs["SoyuzIterator"].typ
}

func (g *Generator) generateListIter(listVal value.Value) value.Value {
	iterTyp := g.iteratorStruct()
	raw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 16), constant.NewNull(types.I8Ptr), constant.NewNull(types.I8Ptr))
	iterPtr := g.current.NewBitCast(raw, types.NewPointer(iterTyp))
	listAsI8 := g.current.NewBitCast(listVal, types.I8Ptr)
	listField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(listAsI8, listField)
	indexField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(constant.NewInt(types.I64, 0), indexField)
	return iterPtr
}

func (g *Generator) generateIteratorNext(iterVal value.Value, st *checker.SpecializedType) (value.Value, error) {
	iterTyp := g.iteratorStruct()
	iterPtr := iterVal
	if _, ok := iterVal.Type().(*types.PointerType); !ok {
		alloc := g.newAlloca(iterVal.Type())
		g.current.NewStore(iterVal, alloc)
		iterPtr = alloc
	}

	listField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	indexField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	listAsI8 := g.current.NewLoad(types.I8Ptr, listField)
	index := g.current.NewLoad(types.I64, indexField)

	listTyped := g.current.NewBitCast(listAsI8, types.NewPointer(g.structs["SoyuzList"].typ))
	sizePtr := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size := g.current.NewLoad(types.I64, sizePtr)

	elemLLVMType := g.mapTypeToLLVM(st.Params[0])
	ei := g.enums["Option"]

	fn := g.current.Parent
	hasBlock := g.newBlock("iter_next_has", fn)
	doneBlock := g.newBlock("iter_next_done", fn)
	mergeBlock := g.newBlock("iter_next_merge", fn)

	hasMore := g.current.NewICmp(enum.IPredSLT, index, size)
	g.current.NewCondBr(hasMore, hasBlock, doneBlock)

	// Some branch: fetch element and advance index.
	g.current = hasBlock
	raw := g.current.NewCall(g.findFunc("soyuz_list_get"), listAsI8, index)
	var elem value.Value
	if elemLLVMType.Equal(types.I64) {
		elem = g.current.NewPtrToInt(raw, types.I64)
	} else {
		elem = g.current.NewBitCast(raw, elemLLVMType)
		if g.isHeapType(elemLLVMType) {
			g.emitRetain(elem)
		}
	}
	nextIndex := g.current.NewAdd(index, constant.NewInt(types.I64, 1))
	g.current.NewStore(nextIndex, indexField)
	someVal, err := g.emitOptionResultAlloc("Option", 0, elem)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	hasBlockOut := g.current

	// None branch.
	g.current = doneBlock
	noneVal, err := g.generateNoneLiteral(&parser.NoneLiteral{})
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	doneBlockOut := g.current

	g.current = mergeBlock
	_ = ei
	return mergeBlock.NewPhi(
		ir.NewIncoming(someVal, hasBlockOut),
		ir.NewIncoming(noneVal, doneBlockOut),
	), nil
}

func (g *Generator) generateIteratorIsEmpty(iterVal value.Value) (value.Value, error) {
	iterTyp := g.iteratorStruct()
	iterPtr := iterVal
	if _, ok := iterVal.Type().(*types.PointerType); !ok {
		alloc := g.newAlloca(iterVal.Type())
		g.current.NewStore(iterVal, alloc)
		iterPtr = alloc
	}

	listField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	indexField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	listAsI8 := g.current.NewLoad(types.I8Ptr, listField)
	index := g.current.NewLoad(types.I64, indexField)

	listTyped := g.current.NewBitCast(listAsI8, types.NewPointer(g.structs["SoyuzList"].typ))
	sizePtr := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size := g.current.NewLoad(types.I64, sizePtr)

	return g.current.NewICmp(enum.IPredSGE, index, size), nil
}

func (g *Generator) generateForMap(n *parser.ForStmt) (value.Value, error) {
	st, ok := g.check.NodeTypes[n.Iterable].(*checker.SpecializedType)
	if !ok {
		return nil, fmt.Errorf("for-in map: tipo inválido")
	}
	mapVal, err := g.generateExpr(n.Iterable)
	if err != nil {
		return nil, err
	}
	keysList, err := g.generateMapKeys(mapVal, st)
	if err != nil {
		return nil, err
	}
	return g.generateForListValue(n.Binding, keysList, st.Params[0], n.Body)
}

func (g *Generator) generateForIterator(n *parser.ForStmt) (value.Value, error) {
	st, ok := g.check.NodeTypes[n.Iterable].(*checker.SpecializedType)
	if !ok {
		return nil, fmt.Errorf("for-in iterator: tipo inválido")
	}
	iterVal, err := g.generateExpr(n.Iterable)
	if err != nil {
		return nil, err
	}

	iterTyp := g.iteratorStruct()
	iterPtr := iterVal
	if _, ok := iterVal.Type().(*types.PointerType); !ok {
		alloc := g.newAlloca(iterVal.Type())
		g.current.NewStore(iterVal, alloc)
		iterPtr = alloc
	}

	listField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	indexField := g.current.NewGetElementPtr(iterTyp, iterPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	listAsI8 := g.current.NewLoad(types.I8Ptr, listField)
	listTyped := g.current.NewBitCast(listAsI8, types.NewPointer(g.structs["SoyuzList"].typ))

	sizePtr := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size := g.current.NewLoad(types.I64, sizePtr)

	elemCheckerType := st.Params[0]
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)

	fn := g.current.Parent
	condBlock := g.newBlock("fori_cond", fn)
	bodyBlock := g.newBlock("fori_body", fn)
	incrBlock := g.newBlock("fori_incr", fn)
	afterBlock := g.newBlock("fori_after", fn)

	bindAlloc := g.newAlloca(elemLLVMType)
	if g.isHeapType(elemLLVMType) {
		g.current.NewStore(g.defaultReturnValue(elemLLVMType), bindAlloc)
	}
	g.vars[n.Binding] = bindAlloc

	g.current.NewBr(condBlock)

	g.current = condBlock
	index := g.current.NewLoad(types.I64, indexField)
	cond := g.current.NewICmp(enum.IPredSLT, index, size)
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	g.current = bodyBlock
	raw := g.current.NewCall(g.findFunc("soyuz_list_get"), listAsI8, index)
	var elem value.Value
	if elemLLVMType.Equal(types.I64) {
		elem = g.current.NewPtrToInt(raw, types.I64)
	} else {
		elem = g.current.NewBitCast(raw, elemLLVMType)
		if g.isHeapType(elemLLVMType) {
			g.emitRetain(elem)
		}
	}
	if g.isHeapType(elemLLVMType) {
		old := g.current.NewLoad(elemLLVMType, bindAlloc)
		g.emitRelease(old)
	}
	g.current.NewStore(elem, bindAlloc)

	g.loops = append(g.loops, loopCtx{cond: incrBlock, after: afterBlock})
	if _, err = g.generateExpr(n.Body); err != nil {
		return nil, err
	}
	if g.current.Term == nil {
		g.current.NewBr(incrBlock)
	}
	g.loops = g.loops[:len(g.loops)-1]

	g.current = incrBlock
	next := g.current.NewAdd(index, constant.NewInt(types.I64, 1))
	g.current.NewStore(next, indexField)
	g.current.NewBr(condBlock)

	g.current = afterBlock
	if g.isHeapType(elemLLVMType) {
		final := g.current.NewLoad(elemLLVMType, bindAlloc)
		g.emitRelease(final)
	}
	delete(g.vars, n.Binding)
	return nil, nil
}

func (g *Generator) generateForListValue(binding string, listVal value.Value, elemCheckerType checker.Type, body *parser.BlockStmt) (value.Value, error) {
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)
	listPtr := types.NewPointer(g.structs["SoyuzList"].typ)
	listTyped := g.current.NewBitCast(listVal, listPtr)

	sizePtr := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size := g.current.NewLoad(types.I64, sizePtr)

	fn := g.current.Parent
	iAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), iAlloc)

	bindAlloc := g.newAlloca(elemLLVMType)
	if g.isHeapType(elemLLVMType) {
		g.current.NewStore(g.defaultReturnValue(elemLLVMType), bindAlloc)
	}
	g.vars[binding] = bindAlloc

	condBlock := g.newBlock("forl_cond", fn)
	bodyBlock := g.newBlock("forl_body", fn)
	incrBlock := g.newBlock("forl_incr", fn)
	afterBlock := g.newBlock("forl_after", fn)

	g.current.NewBr(condBlock)

	g.current = condBlock
	i := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, i, size)
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	g.current = bodyBlock
	listAsI8 := g.current.NewBitCast(listTyped, types.I8Ptr)
	iLoad := g.current.NewLoad(types.I64, iAlloc)
	raw := g.current.NewCall(g.findFunc("soyuz_list_get"), listAsI8, iLoad)

	var elem value.Value
	if elemLLVMType.Equal(types.I64) {
		elem = g.current.NewPtrToInt(raw, types.I64)
	} else if elemLLVMType.Equal(types.Double) {
		i64Val := g.current.NewPtrToInt(raw, types.I64)
		elem = g.current.NewBitCast(i64Val, types.Double)
	} else if elemLLVMType.Equal(types.I1) {
		i64Val := g.current.NewPtrToInt(raw, types.I64)
		elem = g.current.NewTrunc(i64Val, types.I1)
	} else {
		elem = g.current.NewBitCast(raw, elemLLVMType)
		if g.isHeapType(elemLLVMType) {
			g.emitRetain(elem)
		}
	}
	if g.isHeapType(elemLLVMType) {
		old := g.current.NewLoad(elemLLVMType, bindAlloc)
		g.emitRelease(old)
	}
	g.current.NewStore(elem, bindAlloc)

	g.loops = append(g.loops, loopCtx{cond: incrBlock, after: afterBlock})
	if _, err := g.generateExpr(body); err != nil {
		return nil, err
	}
	if g.current.Term == nil {
		g.current.NewBr(incrBlock)
	}
	g.loops = g.loops[:len(g.loops)-1]

	g.current = incrBlock
	cur := g.current.NewLoad(types.I64, iAlloc)
	next := g.current.NewAdd(cur, constant.NewInt(types.I64, 1))
	g.current.NewStore(next, iAlloc)
	g.current.NewBr(condBlock)

	g.current = afterBlock
	if g.isHeapType(elemLLVMType) {
		final := g.current.NewLoad(elemLLVMType, bindAlloc)
		g.emitRelease(final)
	}
	delete(g.vars, binding)
	return nil, nil
}
