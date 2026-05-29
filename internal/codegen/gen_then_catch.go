package codegen

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	checker "soyuz/internal/checker"
	parser "soyuz/internal/parser"
)

// ── M-25: Task[T].then(fn: T -> U) -> Task[U] ─────────────────────────────────

// generateTaskThen implements Task[T].then(fn: T -> U) -> Task[U].
//
// Difference from ~>: .then can be attached to an already-existing Task[T]
// handle — received as argument, returned from function, or built dynamically.
//
// The then wrapper:
//  1. Awaits the source task to get T.
//  2. Calls fn(T) → U.
//  3. Stores U via srt_set_task_result.
func (g *Generator) generateTaskThen(me *parser.MemberExpr, n *parser.CallExpr, srcHandle value.Value) (value.Value, error) {
	if len(n.Args) == 0 {
		return nil, fmt.Errorf(".then: esperado argumento fn: T -> U")
	}

	// Evaluate the callback in the current context.
	callbackVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}

	// Determine inner LLVM type T from Task[T].
	innerLLVMType := types.Type(types.I64)
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		innerLLVMType = g.mapTypeToLLVM(st.Params[0])
	}

	// Determine return LLVM type U from Task[U] (the result type of this call).
	retLLVMType := types.Type(types.I64)
	if ft, ok := g.check.Specializations[n]; ok && ft != nil {
		if retST, ok2 := ft.Return.(*checker.SpecializedType); ok2 && len(retST.Params) > 0 {
			retLLVMType = g.mapTypeToLLVM(retST.Params[0])
		}
	}

	// Pack [srcHandle, callback] as a 2-slot i64 buffer.
	argsHeap := g.current.NewCall(g.findBuiltin("malloc"), constant.NewInt(types.I64, 16))
	arrType := types.NewArray(uint64(2), types.I64)
	argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(g.castToI64(srcHandle), slot0)

	slot1 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(g.castToI64(callbackVal), slot1)

	// Generate the then-wrapper specialized for (T, U) and enqueue it.
	wrapperFn := g.generateThenWrapperFunc(innerLLVMType, retLLVMType)
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	newHandle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)

	// Null source alloca — ownership transferred to the wrapper.
	if ident, ok := me.Object.(*parser.Identifier); ok {
		if alloc, exists := g.vars[ident.Name]; exists {
			g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
		}
	}

	return newHandle, nil
}

// generateThenWrapperFunc emits `void @__then_wrapper_N(i8* raw_args)`.
//
// The wrapper:
//  1. Unpacks srcHandle (slot 0) and closure (slot 1) from the args buffer.
//  2. Calls srt_await(srcHandle) → i8* result.
//  3. Casts result i8* → innerType (T) for the closure argument.
//  4. Calls the closure: fn(T) → U.
//  5. Casts U → i8* and stores via srt_set_task_result.
func (g *Generator) generateThenWrapperFunc(innerType, retType types.Type) *ir.Func {
	name := fmt.Sprintf("__then_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++
	g.getOrCreateClosureDtor()

	fn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	oldCurrent, oldVars, oldHeapVars, oldScope, oldTaskVS, oldSyncGS, oldArcVS, oldBN, oldRT :=
		g.current, g.vars, g.heapVars, g.scopeStack, g.taskVarStack, g.syncGuardStack, g.arcVarStack, g.blockNames, g.currentReturnType

	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack, g.taskVarStack, g.syncGuardStack, g.arcVarStack = nil, nil, nil, nil
	g.blockNames = make(map[string]int)
	g.current = g.newBlock("entry", fn)

	rawArgs := fn.Params[0]
	arrType := types.NewArray(uint64(2), types.I64)
	argsBuf := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handleI64 := g.current.NewLoad(types.I64, slot0)
	thenSrcHandle := g.current.NewIntToPtr(handleI64, types.I8Ptr)

	slot1 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	closureI64 := g.current.NewLoad(types.I64, slot1)
	closurePtr := g.current.NewIntToPtr(closureI64, types.I8Ptr)

	// Await source task.
	result := g.current.NewCall(g.findFunc("srt_await"), thenSrcHandle) // i8*

	// Cast result (i8*) to T for the closure argument.
	argVal := g.castFromI8Ptr(result, innerType)

	// Call closure: fn(T) → U.
	var callResult value.Value
	if retType.Equal(types.Void) {
		g.callClosureDirect(closurePtr, types.Void, []value.Value{argVal})
		g.current.NewCall(g.findFunc("srt_set_task_result"), constant.NewNull(types.I8Ptr))
	} else {
		callResult = g.callClosureDirect(closurePtr, retType, []value.Value{argVal})
		resultI8 := g.castToI8Ptr(callResult)
		g.current.NewCall(g.findFunc("srt_set_task_result"), resultI8)
	}

	g.current.NewRet(nil)

	g.current, g.vars, g.heapVars, g.scopeStack, g.taskVarStack, g.syncGuardStack, g.arcVarStack, g.blockNames, g.currentReturnType =
		oldCurrent, oldVars, oldHeapVars, oldScope, oldTaskVS, oldSyncGS, oldArcVS, oldBN, oldRT

	return fn
}

// ── M-25: Task[Result[T]].catch(fn: ErrType -> T) -> Task[Result[T]] ──────────

// generateTaskCatch implements Task[Result[T]].catch(fn: ErrType -> Result[T]) -> Task[Result[T]].
//
// The catch wrapper:
//  1. Awaits the source task to get Result[T] (as i8* → Result*).
//  2. Reads the tag: 0 = Ok, 1 = Err.
//  3. Ok path: re-stores result unchanged.
//  4. Err path: extracts error payload (i64), calls recovery fn with it,
//     stores the recovery fn's return value (Result[T]) via srt_set_task_result.
//
// The recovery fn must return Result[T] (e.g. Ok(v) or Err(e2)), not T.
// This matches the calling convention of Promise.catch where the handler
// returns the same monad type.
func (g *Generator) generateTaskCatch(me *parser.MemberExpr, n *parser.CallExpr, srcHandle value.Value) (value.Value, error) {
	if len(n.Args) == 0 {
		return nil, fmt.Errorf(".catch: esperado argumento fn de recuperação")
	}

	callbackVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}

	// Determine the LLVM return type of the recovery fn (should be *Result).
	// We use this in the wrapper so callClosureDirect uses the correct ABI.
	closureRetLLVMType := types.Type(types.I8Ptr) // default: i8* (pointer)
	if ft, ok := g.check.NodeTypes[n.Args[0]].(*checker.FuncType); ok && ft != nil {
		closureRetLLVMType = g.mapTypeToLLVM(ft.Return)
	}

	// Pack [srcHandle, callback] as a 2-slot i64 buffer.
	argsHeap := g.current.NewCall(g.findBuiltin("malloc"), constant.NewInt(types.I64, 16))
	arrType := types.NewArray(uint64(2), types.I64)
	argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(g.castToI64(srcHandle), slot0)

	slot1 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(g.castToI64(callbackVal), slot1)

	wrapperFn := g.generateCatchWrapperFunc(closureRetLLVMType)
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	newHandle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)

	// Null source alloca.
	if ident, ok := me.Object.(*parser.Identifier); ok {
		if alloc, exists := g.vars[ident.Name]; exists {
			g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
		}
	}

	return newHandle, nil
}

// generateCatchWrapperFunc emits `void @__catch_wrapper_N(i8* raw_args)`.
//
// The wrapper inspects the Result tag:
//   - tag == 0 (Ok):  re-stores original result unchanged.
//   - tag == 1 (Err): extracts error payload (as i64), calls recovery fn,
//     stores its return value (Result[T]) directly via srt_set_task_result.
//
// closureRetType must be the LLVM type that the recovery fn actually returns
// (typically *Result). Using the correct type avoids ABI mismatches when
// calling through a bitcasted function pointer.
func (g *Generator) generateCatchWrapperFunc(closureRetType types.Type) *ir.Func {
	name := fmt.Sprintf("__catch_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++
	g.getOrCreateClosureDtor()

	fn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	oldCurrent, oldVars, oldHeapVars, oldScope, oldTaskVS, oldSyncGS, oldArcVS, oldBN, oldRT :=
		g.current, g.vars, g.heapVars, g.scopeStack, g.taskVarStack, g.syncGuardStack, g.arcVarStack, g.blockNames, g.currentReturnType

	g.vars = make(map[string]value.Value)
	g.heapVars = make(map[string]bool)
	g.scopeStack, g.taskVarStack, g.syncGuardStack, g.arcVarStack = nil, nil, nil, nil
	g.blockNames = make(map[string]int)
	g.current = g.newBlock("entry", fn)

	rawArgs := fn.Params[0]
	arrType := types.NewArray(uint64(2), types.I64)
	argsBuf := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handleI64 := g.current.NewLoad(types.I64, slot0)
	catchSrcHandle := g.current.NewIntToPtr(handleI64, types.I8Ptr)

	slot1 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	closureI64 := g.current.NewLoad(types.I64, slot1)
	closurePtr := g.current.NewIntToPtr(closureI64, types.I8Ptr)

	// Await source task → i8* pointing to Result struct.
	result := g.current.NewCall(g.findFunc("srt_await"), catchSrcHandle)

	// Ensure Result struct type exists in module.
	var resultStructType *types.StructType
	for _, td := range g.module.TypeDefs {
		if st, ok := td.(*types.StructType); ok && st.TypeName == "Result" {
			resultStructType = st
			break
		}
	}
	if resultStructType == nil {
		t := g.module.NewTypeDef("Result", types.NewStruct(types.I64, types.NewArray(64, types.I8)))
		resultStructType = t.(*types.StructType)
	}

	// Read tag from Result struct.
	typedPtr := g.current.NewBitCast(result, types.NewPointer(resultStructType))
	tagPtr := g.current.NewGetElementPtr(resultStructType, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := g.current.NewLoad(types.I64, tagPtr)
	isErr := g.current.NewICmp(enum.IPredNE, tag, constant.NewInt(types.I64, 0))

	errBlock := g.newBlock("catch_err", fn)
	okBlock := g.newBlock("catch_ok", fn)
	mergeBlock := g.newBlock("catch_merge", fn)
	g.current.NewCondBr(isErr, errBlock, okBlock)

	// ── Err branch: extract error payload, call recovery fn, wrap in Ok ──
	g.current = errBlock
	payloadPtr := g.current.NewGetElementPtr(resultStructType, typedPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	payloadI64Ptr := g.current.NewBitCast(payloadPtr, types.NewPointer(types.I64))
	errPayloadI64 := g.current.NewLoad(types.I64, payloadI64Ptr)
	// Call recovery fn: Err -> Result[T].
	// Use the closure's actual return LLVM type to avoid ABI mismatch when
	// calling through a bitcasted function pointer.
	recovered := g.callClosureDirect(closurePtr, closureRetType, []value.Value{errPayloadI64})
	recoveredI8 := g.castToI8Ptr(recovered)
	g.current.NewCall(g.findFunc("srt_set_task_result"), recoveredI8)
	g.current.NewBr(mergeBlock)

	// ── Ok branch: pass through unchanged ──
	g.current = okBlock
	g.current.NewCall(g.findFunc("srt_set_task_result"), result)
	g.current.NewBr(mergeBlock)

	// ── Merge ──
	g.current = mergeBlock
	g.current.NewRet(nil)

	g.current, g.vars, g.heapVars, g.scopeStack, g.taskVarStack, g.syncGuardStack, g.arcVarStack, g.blockNames, g.currentReturnType =
		oldCurrent, oldVars, oldHeapVars, oldScope, oldTaskVS, oldSyncGS, oldArcVS, oldBN, oldRT

	return fn
}

// ensure the compiler sees value.Value used (avoids import error if no direct use).
var _ value.Value
