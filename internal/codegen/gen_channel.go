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
