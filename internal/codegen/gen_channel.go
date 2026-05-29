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

// generateChannelNew emits Channel.new(capacity) → i8* (srt_chan_t*).
func (g *Generator) generateChannelNew(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("Channel.new requer exatamente 1 argumento (capacity)")
	}
	cap, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	fn := g.findFunc("srt_chan_new")
	return g.current.NewCall(fn, cap), nil
}

// generateChannelSend emits ch.send(value) → void.
func (g *Generator) generateChannelSend(obj parser.Node, n *parser.CallExpr) (value.Value, error) {
	ch, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	if len(n.Args) != 1 {
		return nil, fmt.Errorf("Channel.send requer exatamente 1 argumento")
	}
	val, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	raw := g.coerceToI64(val)
	g.current.NewCall(g.findFunc("srt_chan_send"), ch, raw)
	return constant.NewInt(types.I64, 0), nil
}

// generateChannelRecv emits ch.recv() or ch.tryRecv() → Option[T].
// tryMode selects srt_chan_try_recv (non-blocking) vs srt_chan_recv (blocking).
func (g *Generator) generateChannelRecv(obj parser.Node, st *checker.SpecializedType, tryMode bool) (value.Value, error) {
	ch, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}

	var innerType checker.Type = checker.Unknown
	if len(st.Params) > 0 {
		innerType = st.Params[0]
	}

	outAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), outAlloc)

	var fnName string
	if tryMode {
		fnName = "srt_chan_try_recv"
	} else {
		fnName = "srt_chan_recv"
	}
	ok := g.current.NewCall(g.findFunc(fnName), ch, outAlloc)

	fn := g.current.Parent
	someBlock := g.newBlock("chan_some", fn)
	noneBlock := g.newBlock("chan_none", fn)
	mergeBlock := g.newBlock("chan_merge", fn)

	cmp := g.current.NewICmp(enum.IPredNE, ok, constant.NewInt(types.I64, 0))
	g.current.NewCondBr(cmp, someBlock, noneBlock)

	// Some branch: load raw i64, coerce to T, wrap in Option
	g.current = someBlock
	rawVal := g.current.NewLoad(types.I64, outAlloc)
	var payload value.Value
	if innerType == checker.Unknown || innerType == nil {
		payload = rawVal
	} else {
		payload = g.coerceFromI64(rawVal, innerType)
	}
	someOpt, err := g.emitOptionResultAlloc("Option", 0, payload)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	someEnd := g.current

	// None branch
	g.current = noneBlock
	noneOpt, err := g.emitOptionResultAlloc("Option", 1, nil)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	noneEnd := g.current

	g.current = mergeBlock
	phi := g.current.NewPhi(
		ir.NewIncoming(someOpt, someEnd),
		ir.NewIncoming(noneOpt, noneEnd),
	)
	return phi, nil
}

// generateChannelClose emits ch.close() using the given C function name.
func (g *Generator) generateChannelClose(obj parser.Node, fnName string) (value.Value, error) {
	ch, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	g.current.NewCall(g.findFunc(fnName), ch)
	return constant.NewInt(types.I64, 0), nil
}

// generateChannelIsClosed emits ch.isClosed() → Bool.
func (g *Generator) generateChannelIsClosed(obj parser.Node) (value.Value, error) {
	ch, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}
	raw := g.current.NewCall(g.findFunc("srt_chan_is_closed"), ch)
	return g.current.NewTrunc(raw, types.I1), nil
}

// extractChannelFromRecvCall extracts the channel object node from a ch.recv() or
// ch.tryRecv() call expression. Returns nil if the node is not a recv call.
func extractChannelFromRecvCall(node parser.Node) parser.Node {
	call, ok := node.(*parser.CallExpr)
	if !ok {
		return nil
	}
	me, ok := call.Callee.(*parser.MemberExpr)
	if !ok {
		return nil
	}
	if me.Property == "recv" || me.Property == "tryRecv" {
		return me.Object
	}
	return nil
}

// innerTypeFromChannelType returns the element type T from Channel[T].
func innerTypeFromChannelType(t checker.Type) checker.Type {
	if st, ok := t.(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 && ct.Name == "Channel" && len(st.Params) > 0 {
			return st.Params[0]
		}
	}
	return checker.Unknown
}

// isTaskAwaitCall reports whether node is a t.await() call on a Task[T].
func isTaskAwaitCall(node parser.Node) bool {
	call, ok := node.(*parser.CallExpr)
	if !ok {
		return false
	}
	me, ok := call.Callee.(*parser.MemberExpr)
	return ok && me.Property == "await"
}

// generateSelectExpr emits LLVM IR for:
//
//	select {
//	    binding = ch.recv()  => body   // channel arm
//	    binding = t.await()  => body   // task arm (M-28: synthesizes bridge channel)
//	    default               => body
//	}
//
// Task arms: a temp Channel[T](1) is created and a fire-and-forget listener
// task is spawned that awaits the task and sends the result to the channel.
// The channel is then selected alongside regular channel arms.
func (g *Generator) generateSelectExpr(n *parser.SelectExpr) (value.Value, error) {
	var recvArms []parser.SelectArm
	var defaultArm *parser.SelectArm
	for i := range n.Arms {
		arm := &n.Arms[i]
		if arm.IsDefault {
			defaultArm = arm
		} else {
			recvArms = append(recvArms, *arm)
		}
	}

	numRecv := int64(len(recvArms))
	fn := g.current.Parent
	mergeBlock := g.newBlock("sel_merge", fn)

	if numRecv == 0 {
		if defaultArm != nil {
			bodyVal, err := g.generateExpr(defaultArm.Body)
			if err != nil {
				return nil, err
			}
			if g.current.Term == nil {
				g.current.NewBr(mergeBlock)
			}
			g.current = mergeBlock
			return bodyVal, nil
		}
		g.current = mergeBlock
		return constant.NewInt(types.I64, 0), nil
	}

	i8PtrType := types.I8Ptr
	arrType := types.NewArray(uint64(numRecv), i8PtrType)
	chanArr := g.newAlloca(arrType)

	armInnerTypes := make([]checker.Type, numRecv)

	for i, arm := range recvArms {
		var chVal value.Value

		if isTaskAwaitCall(arm.Chan) {
			// Task arm: spawn a bridge listener then select on a temp channel.
			call := arm.Chan.(*parser.CallExpr)
			me := call.Callee.(*parser.MemberExpr)
			taskVal, err := g.generateExpr(me.Object)
			if err != nil {
				return nil, err
			}
			// Determine Task[T] inner type.
			taskType := g.check.NodeTypes[me.Object]
			if st, ok := taskType.(*checker.SpecializedType); ok && len(st.Params) > 0 {
				armInnerTypes[i] = st.Params[0]
			}
			// Create a temp channel with capacity 1.
			tmpCh := g.current.NewCall(g.findFunc("srt_chan_new"), constant.NewInt(types.I64, 1))
			// Spawn a fire-and-forget listener: await task → send to tmpCh.
			g.emitInternalListenerTask(taskVal, tmpCh)
			// Zero the task handle alloca so taskVarStack destructor is a no-op.
			if id, ok := me.Object.(*parser.Identifier); ok {
				if alloc, ok2 := g.vars[id.Name]; ok2 {
					g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
				}
			}
			chVal = tmpCh
		} else {
			// Channel arm: ch.recv() or ch.tryRecv().
			chanObj := extractChannelFromRecvCall(arm.Chan)
			if chanObj == nil {
				return nil, fmt.Errorf("select arm %d: Chan deve ser ch.recv(), ch.tryRecv() ou t.await()", i)
			}
			chanType := g.check.NodeTypes[chanObj]
			armInnerTypes[i] = innerTypeFromChannelType(chanType)
			var err error
			chVal, err = g.generateExpr(chanObj)
			if err != nil {
				return nil, err
			}
		}

		if chVal.Type() != i8PtrType {
			chVal = g.current.NewBitCast(chVal, i8PtrType)
		}
		elemPtr := g.current.NewGetElementPtr(arrType, chanArr,
			constant.NewInt(types.I32, 0),
			constant.NewInt(types.I32, int64(i)),
		)
		g.current.NewStore(chVal, elemPtr)
	}

	// Decay the stack array to a pointer for the runtime call (void**).
	chanArrPtr := g.current.NewGetElementPtr(arrType, chanArr,
		constant.NewInt(types.I32, 0),
		constant.NewInt(types.I32, 0),
	)
	ptrI8PtrType := types.NewPointer(i8PtrType)
	chanPtrCast := g.current.NewBitCast(chanArrPtr, ptrI8PtrType)

	// Allocate the out-param slot for the received i64 value.
	outAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), outAlloc)

	nConst := constant.NewInt(types.I64, numRecv)

	// Pre-allocate arm blocks.
	armBlocks := make([]*ir.Block, numRecv)
	for i := range recvArms {
		armBlocks[i] = g.newBlock(fmt.Sprintf("sel_arm%d", i), fn)
	}

	var defaultBlock *ir.Block
	if defaultArm != nil {
		defaultBlock = g.newBlock("sel_default", fn)
	}

	// Emit the select call and dispatch.
	var readyIdx value.Value
	if defaultArm != nil {
		// Non-blocking: srt_select_try; if -1 → default, else → dispatch.
		readyIdx = g.current.NewCall(g.findFunc("srt_select_try"), chanPtrCast, nConst, outAlloc)
		cmp := g.current.NewICmp(enum.IPredSGE, readyIdx, constant.NewInt(types.I64, 0))
		dispatchBlock := g.newBlock("sel_dispatch", fn)
		g.current.NewCondBr(cmp, dispatchBlock, defaultBlock)
		g.current = dispatchBlock
	} else {
		// Blocking: srt_select blocks until a channel is ready.
		readyIdx = g.current.NewCall(g.findFunc("srt_select"), chanPtrCast, nConst, outAlloc)
	}

	// Allocate a result slot before the switch so arm values don't violate SSA dominance.
	resultAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), resultAlloc)

	// Switch on the ready index to jump to the correct arm block.
	switchDefault := armBlocks[0]
	cases := make([]*ir.Case, numRecv)
	for i := range recvArms {
		cases[i] = ir.NewCase(constant.NewInt(types.I64, int64(i)), armBlocks[i])
	}
	g.current.NewSwitch(readyIdx, switchDefault, cases...)

	// Generate each recv arm block.
	for i, arm := range recvArms {
		g.current = armBlocks[i]

		rawRecv := g.current.NewLoad(types.I64, outAlloc)

		if arm.Binding != "" {
			innerType := armInnerTypes[i]
			var bindVal value.Value
			if innerType != checker.Unknown && innerType != nil {
				bindVal = g.coerceFromI64(rawRecv, innerType)
			} else {
				bindVal = rawRecv
			}
			alloc := g.newAlloca(bindVal.Type())
			g.current.NewStore(bindVal, alloc)
			g.vars[arm.Binding] = alloc
		}

		bv, err := g.generateExpr(arm.Body)
		if err != nil {
			return nil, err
		}
		if arm.Binding != "" {
			delete(g.vars, arm.Binding)
		}
		if bv != nil {
			g.current.NewStore(g.castToI64(bv), resultAlloc)
		}
		if g.current.Term == nil {
			g.current.NewBr(mergeBlock)
		}
	}

	// Generate the default arm block (if present).
	if defaultArm != nil {
		g.current = defaultBlock
		dv, err := g.generateExpr(defaultArm.Body)
		if err != nil {
			return nil, err
		}
		if dv != nil {
			g.current.NewStore(g.castToI64(dv), resultAlloc)
		}
		if g.current.Term == nil {
			g.current.NewBr(mergeBlock)
		}
	}

	g.current = mergeBlock
	return g.current.NewLoad(types.I64, resultAlloc), nil
}

// emitInternalListenerTask spawns a fire-and-forget wrapper task that:
//   1. awaits srcHandle
//   2. sends the raw result to dstChan
//
// Used by the polymorphic select when a t.await() arm is encountered.
func (g *Generator) emitInternalListenerTask(srcHandle value.Value, dstChan value.Value) {
	// Pack [srcHandle, dstChan] into a 2-slot heap buffer.
	argsHeap := g.current.NewCall(g.findBuiltin("malloc"), constant.NewInt(types.I64, 16))
	arrType := types.NewArray(2, types.I64)
	argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(g.current.NewPtrToInt(srcHandle, types.I64), slot0)

	slot1 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(g.current.NewPtrToInt(dstChan, types.I64), slot1)

	// Generate the wrapper function and enqueue+detach.
	wrapperFn := g.generateSelectListenerWrapper()
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	handle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
	g.current.NewCall(g.findFunc("srt_detach"), handle)

	// Zero the srcHandle alloca so the taskVarStack destructor is a no-op.
	if alloc, ok := srcHandle.(*ir.InstGetElementPtr); ok {
		_ = alloc
	}
}

// generateSelectListenerWrapper emits void @__sel_listener_N(i8* raw_args) that:
//   1. unpacks [srcHandle i64, dstChan i64]
//   2. awaits srcHandle
//   3. sends the raw result to dstChan
func (g *Generator) generateSelectListenerWrapper() *ir.Func {
	g.taskWrapperCounter++
	name := fmt.Sprintf("__sel_listener_%d", g.taskWrapperCounter)

	wrapFn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	oldCurrent := g.current
	oldVars := g.vars
	oldHeapVars := g.heapVars
	oldScopeStack := g.scopeStack
	oldTaskVarStack := g.taskVarStack
	oldSyncGuardStack := g.syncGuardStack
	oldArcVarStack := g.arcVarStack
	oldBlockNames := g.blockNames
	oldReturnType := g.currentReturnType

	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack = nil
	g.taskVarStack = nil
	g.syncGuardStack = nil
	g.arcVarStack = nil
	g.blockNames = make(map[string]int)
	g.current = g.newBlock("entry", wrapFn)

	arrType := types.NewArray(2, types.I64)
	argsPtr := g.current.NewBitCast(wrapFn.Params[0], types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	srcI64 := g.current.NewLoad(types.I64, slot0)
	srcHandle := g.current.NewIntToPtr(srcI64, types.I8Ptr)

	slot1 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	chanI64 := g.current.NewLoad(types.I64, slot1)
	dstChan := g.current.NewIntToPtr(chanI64, types.I8Ptr)

	result := g.current.NewCall(g.findFunc("srt_await"), srcHandle)
	raw := g.current.NewPtrToInt(result, types.I64)
	g.current.NewCall(g.findFunc("srt_chan_send"), dstChan, raw)
	g.current.NewRet(nil)

	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType
	return wrapFn
}
