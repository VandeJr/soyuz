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

// generateSyncChannelNew emits SyncChannel.new() → i8* (srt_sync_chan_t*).
func (g *Generator) generateSyncChannelNew() (value.Value, error) {
	fn := g.findFunc("srt_sync_chan_new")
	return g.current.NewCall(fn), nil
}

// generateChannelSend emits ch.send(value) → void.
func (g *Generator) generateChannelSend(obj parser.Node, n *parser.CallExpr, isSyncChan bool) (value.Value, error) {
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
	var fnName string
	if isSyncChan {
		fnName = "srt_sync_chan_send"
	} else {
		fnName = "srt_chan_send"
	}
	g.current.NewCall(g.findFunc(fnName), ch, raw)
	return constant.NewInt(types.I64, 0), nil
}

// generateChannelRecv emits ch.recv() or ch.tryRecv() → Option[T].
// tryMode selects srt_chan_try_recv (non-blocking) vs srt_chan_recv (blocking).
func (g *Generator) generateChannelRecv(obj parser.Node, st *checker.SpecializedType, tryMode bool, isSyncChan bool) (value.Value, error) {
	ch, err := g.generateExpr(obj)
	if err != nil {
		return nil, err
	}

	var innerType checker.Type = checker.Unknown
	if len(st.Params) > 0 {
		innerType = st.Params[0]
	}

	// Alloca for the out-param (i64*)
	outAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), outAlloc)

	var fnName string
	switch {
	case isSyncChan:
		fnName = "srt_sync_chan_recv"
	case tryMode:
		fnName = "srt_chan_try_recv"
	default:
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

// innerTypeFromChannelType returns the element type T from Channel[T] or SyncChannel[T].
func innerTypeFromChannelType(t checker.Type) checker.Type {
	if st, ok := t.(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 {
			if (ct.Name == "Channel" || ct.Name == "SyncChannel") && len(st.Params) > 0 {
				return st.Params[0]
			}
		}
	}
	return checker.Unknown
}

// generateSelectExpr emits LLVM IR for:
//   select {
//       binding = ch.recv() => body
//       default             => body
//   }
//
// Strategy:
//   1. For each recv arm, extract the channel object from ch.recv().
//   2. Store channel pointers into a stack-allocated i8* array.
//   3. Call srt_select (blocking) or srt_select_try (non-blocking when default is present).
//   4. Use a switch on the returned index to branch to the matching arm block.
//   5. Each arm block: load the raw i64, coerce to T, bind it, execute body, br merge.
//   6. Default arm: execute body when srt_select_try returns -1.
func (g *Generator) generateSelectExpr(n *parser.SelectExpr) (value.Value, error) {
	// Separate recv arms from the optional default arm.
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

	// Edge case: only a default arm (no recv arms).
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

	// Build channel pointer array on the stack.
	// Each element is i8* (the opaque channel pointer).
	i8PtrType := types.I8Ptr
	arrType := types.NewArray(uint64(numRecv), i8PtrType)
	chanArr := g.newAlloca(arrType)

	// Track inner element types per arm (for binding coercion later).
	armInnerTypes := make([]checker.Type, numRecv)

	for i, arm := range recvArms {
		// Extract channel object from ch.recv() / ch.tryRecv().
		chanObj := extractChannelFromRecvCall(arm.Chan)
		if chanObj == nil {
			return nil, fmt.Errorf("select arm %d: Chan deve ser ch.recv() ou ch.tryRecv()", i)
		}

		// Determine inner type from the channel's type annotation.
		chanType := g.check.NodeTypes[chanObj]
		armInnerTypes[i] = innerTypeFromChannelType(chanType)

		// Generate the channel pointer value.
		chVal, err := g.generateExpr(chanObj)
		if err != nil {
			return nil, err
		}
		// Ensure it's stored as i8*.
		if chVal.Type() != i8PtrType {
			chVal = g.current.NewBitCast(chVal, i8PtrType)
		}

		// Store into the array slot.
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

	// Switch on the ready index to jump to the correct arm block.
	// switchDefault is armBlocks[0] as a fallback (srt_select guarantees a valid index).
	switchDefault := armBlocks[0]
	cases := make([]*ir.Case, numRecv)
	for i := range recvArms {
		cases[i] = ir.NewCase(constant.NewInt(types.I64, int64(i)), armBlocks[i])
	}
	g.current.NewSwitch(readyIdx, switchDefault, cases...)

	// Generate each recv arm block.
	var lastVal value.Value = constant.NewInt(types.I64, 0)

	for i, arm := range recvArms {
		g.current = armBlocks[i]

		// Load the raw i64 value from the out-param.
		rawRecv := g.current.NewLoad(types.I64, outAlloc)

		// Bind the value to the arm's variable if requested.
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
		if bv != nil {
			lastVal = bv
		}
		// Remove binding from scope when arm exits.
		if arm.Binding != "" {
			delete(g.vars, arm.Binding)
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
			lastVal = dv
		}
		if g.current.Term == nil {
			g.current.NewBr(mergeBlock)
		}
	}

	g.current = mergeBlock
	return lastVal, nil
}
