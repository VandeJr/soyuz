package codegen

import (
	"fmt"
	"sort"
	"soyuz/internal/checker"
	"soyuz/internal/parser"
	"strconv"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

func (g *Generator) generateExpr(node parser.Node) (value.Value, error) {
	switch n := node.(type) {
	case *parser.VarDecl:
		return g.generateVarDecl(n)

	case *parser.IntLiteral:
		v, err := strconv.ParseInt(n.Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid int literal: %v", err)
		}
		return constant.NewInt(types.I64, v), nil

	case *parser.FloatLiteral:
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float literal: %v", err)
		}
		return constant.NewFloat(types.Double, v), nil

	case *parser.BoolLiteral:
		if n.Value {
			return constant.True, nil
		}
		return constant.False, nil

	case *parser.CharLiteral:
		return constant.NewInt(types.I32, int64(n.Value)), nil

	case *parser.StringLiteral:
		data := n.Value
		dataLen := len(data)

		// Combined static type: { SoyuzHeader{ i64, i8* }, SoyuzString{ i64 }, [dataLen+1 x i8] }
		soyuzHeaderType := types.NewStruct(types.I64, types.I8Ptr)
		charArrType := types.NewArray(uint64(dataLen+1), types.I8)
		combinedType := types.NewStruct(soyuzHeaderType, g.soyuzStringType, charArrType)

		// SoyuzHeader with SOYUZ_STATIC_REFCOUNT sentinel (INT64_MAX = 9223372036854775807)
		headerConst := constant.NewStruct(soyuzHeaderType,
			constant.NewInt(types.I64, 9223372036854775807),
			constant.NewNull(types.I8Ptr))
		strStructConst := constant.NewStruct(g.soyuzStringType,
			constant.NewInt(types.I64, int64(dataLen)))
		dataConst := constant.NewCharArrayFromString(data + "\x00")

		combinedConst := constant.NewStruct(combinedType, headerConst, strStructConst, dataConst)
		glob := g.module.NewGlobalDef("", combinedConst)
		glob.Immutable = true

		// GEP to field [0, 1] = the SoyuzString part (after the SoyuzHeader)
		strField := g.current.NewGetElementPtr(combinedType, glob,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 1))
		// Bitcast { i64 }* → %SoyuzString*
		return g.current.NewBitCast(strField, g.soyuzStringPtrType), nil

	case *parser.InterpolatedString:
		return g.generateInterpolatedString(n)

	case *parser.Identifier:
		if alloc, ok := g.vars[n.Name]; ok {
			ptrType, ok := alloc.Type().(*types.PointerType)
			if !ok {
				return nil, fmt.Errorf("variable %s is not a pointer in codegen", n.Name)
			}
			return g.current.NewLoad(ptrType.ElemType, alloc), nil
		}
		// Zero-argument non-generic enum constructor (e.g. `Red` instead of `Red()`).
		if ft, ok := g.check.NodeTypes[n].(*checker.FuncType); ok && len(ft.Params) == 0 {
			if et, ok := ft.Return.(*checker.EnumType); ok {
				if ei, exists := g.enums[et.Name]; exists {
					if vi, ok := ei.variants[n.Name]; ok {
						fakeCall := &parser.CallExpr{}
						return g.generateEnumConstructor(ei, vi, fakeCall)
					}
				}
			}
		}
		// Zero-argument generic enum constructor (e.g. `Nothing` with type `Maybe[Unknown]`).
		if st, ok := g.check.NodeTypes[n].(*checker.SpecializedType); ok {
			if et, ok := st.Base.(*checker.EnumType); ok {
				if decl, ok := g.genericEnumDecls[et.Name]; ok {
					// Find an existing specialization that has this variant.
					prefix := et.Name + "__"
					for name, ei := range g.enums {
						if strings.HasPrefix(name, prefix) {
							if vi, ok2 := ei.variants[n.Name]; ok2 {
								fakeCall := &parser.CallExpr{}
								return g.generateEnumConstructor(ei, vi, fakeCall)
							}
						}
					}
					// No specialization exists yet — create a dummy one with i64 for each type param.
					sub := make(map[string]types.Type)
					for _, gp := range decl.Generics {
						sub[gp.Name] = types.I64
					}
					ei, err := g.getOrCreateSpecializedEnum(decl, sub)
					if err == nil {
						if vi, ok2 := ei.variants[n.Name]; ok2 {
							fakeCall := &parser.CallExpr{}
							return g.generateEnumConstructor(ei, vi, fakeCall)
						}
					}
				}
			}
		}
		// Fall back to global function — as a value, wrap in SoyuzClosure for FuncType.
		if f := g.findFunc(n.Name); f != nil {
			if ft, ok := g.check.NodeTypes[n].(*checker.FuncType); ok {
				return g.getOrCreateTopLevelClosure(n.Name, f, ft)
			}
			return f, nil
		}
		return nil, fmt.Errorf("undefined identifier in codegen: %s", n.Name)

	case *parser.CallExpr:
		return g.generateCallExpr(n)

	case *parser.RecordLiteral:
		return g.generateRecordLiteral(n)

	case *parser.MemberExpr:
		// Enum dot syntax as value: Enum.Variant (zero-arg constructor)
		if _, ok := n.Object.(*parser.Identifier); ok {
			if et, isEnum := g.check.NodeTypes[n.Object].(*checker.EnumType); isEnum {
				if ei, exists := g.enums[et.Name]; exists {
					if vi, ok2 := ei.variants[n.Property]; ok2 {
						fakeCall := &parser.CallExpr{}
						return g.generateEnumConstructor(ei, vi, fakeCall)
					}
				}
			}
		}
		return g.generateMemberExpr(n)

	case *parser.MatchExpr:
		return g.generateMatchExpr(n)

	case *parser.SelfExpr:
		if alloc, ok := g.vars["self"]; ok {
			ptrType := alloc.Type().(*types.PointerType)
			return g.current.NewLoad(ptrType.ElemType, alloc), nil
		}
		return nil, fmt.Errorf("self usado fora de um método de classe no codegen")

	case *parser.ArrowFunc:
		return g.generateArrowFunc(n)

	case *parser.TupleExpr:
		if len(n.Elements) == 0 {
			return constant.NewInt(types.I64, 0), nil
		}
		return g.generateTupleExpr(n)

	case *parser.ListExpr:
		return g.generateListExpr(n)

	case *parser.MapExpr:
		return g.generateMapExpr(n)

	case *parser.SomeExpr:
		return g.generateSomeExpr(n)

	case *parser.NoneLiteral:
		return g.generateNoneLiteral(n)

	case *parser.OkExpr:
		return g.generateOkExpr(n)

	case *parser.ErrExpr:
		return g.generateErrExpr(n)

	case *parser.PipeExpr:
		var call *parser.CallExpr
		if rc, ok := n.Right.(*parser.CallExpr); ok {
			newArgs := append([]parser.Node{n.Left}, rc.Args...)
			call = &parser.CallExpr{Callee: rc.Callee, Args: newArgs}
		} else {
			call = &parser.CallExpr{Callee: n.Right, Args: []parser.Node{n.Left}}
		}
		g.check.Specializations[call] = g.check.Specializations[n]
		return g.generateCallExpr(call)

	case *parser.TaskExpr:
		return g.generateTaskExpr(n)

	case *parser.SelectExpr:
		return g.generateSelectExpr(n)

	case *parser.AsyncPipeExpr:
		return g.generateAsyncPipeExpr(n)

	case *parser.PipeQuestExpr:
		return g.generatePipeQuestExpr(n)

	case *parser.ElvisExpr:
		return g.generateElvisExpr(n)

	case *parser.SafeNavExpr:
		return g.generateSafeNavExpr(n)

	case *parser.AssignExpr:
		val, err := g.generateExpr(n.Right)
		if err != nil {
			return nil, err
		}

		switch l := n.Left.(type) {
		case *parser.Identifier:
			alloc, ok := g.vars[l.Name]
			if !ok {
				return nil, fmt.Errorf("undefined variable in assignment: %s", l.Name)
			}

			if g.isHeapType(val.Type()) {
				ptrType := alloc.Type().(*types.PointerType)
				old := g.current.NewLoad(ptrType.ElemType, alloc)
				g.emitRelease(old)
				g.emitRetain(val)
			}

			g.current.NewStore(val, alloc)
		case *parser.MemberExpr:
			// M8: sync guard value write — guard.value = expr
			if _, _, ok := g.isSyncGuardValueAccess(l); ok {
				if err2 := g.generateSyncGuardWrite(l, val); err2 != nil {
					return nil, err2
				}
				return val, nil
			}
			ptr, err := g.generateMemberPtr(l)
			if err != nil {
				return nil, err
			}

			if g.isHeapType(val.Type()) {
				old := g.current.NewLoad(val.Type(), ptr)
				g.emitRelease(old)
				g.emitRetain(val)
			}

			g.current.NewStore(val, ptr)
		default:
			return nil, fmt.Errorf("left side of assignment must be an identifier or member expr in codegen")
		}
		return val, nil

	case *parser.UnaryExpr:
		operand, err := g.generateExpr(n.Operand)
		if err != nil {
			return nil, err
		}
		switch n.Operator {
		case "-":
			if operand.Type().Equal(types.Double) {
				return g.current.NewFNeg(operand), nil
			}
			return g.current.NewSub(constant.NewInt(types.I64, 0), operand), nil
		case "!":
			return g.current.NewXor(operand, constant.NewInt(types.I1, 1)), nil
		case "~":
			return g.current.NewXor(operand, constant.NewInt(types.I64, -1)), nil
		default:
			return nil, fmt.Errorf("unsupported unary operator in codegen: %s", n.Operator)
		}

	case *parser.BinaryExpr:
		if n.Operator == "&&" || n.Operator == "||" {
			return g.generateLogicalExpr(n)
		}
		return g.generateBinaryExpr(n)

	case *parser.IfStmt:
		return g.generateIfStmt(n)

	case *parser.WhileStmt:
		return g.generateWhileStmt(n)

	case *parser.LoopStmt:
		return g.generateLoopStmt(n)

	case *parser.ForStmt:
		return g.generateForStmt(n)

	case *parser.BreakStmt:
		if len(g.loops) == 0 {
			return nil, fmt.Errorf("break outside of loop")
		}
		lc := g.loops[len(g.loops)-1]
		if n.Value != nil {
			val, err := g.generateExpr(n.Value)
			if err != nil {
				return nil, err
			}
			if lc.resultAlloca != nil {
				g.current.NewStore(val, lc.resultAlloca)
			}
		}
		g.current.NewBr(lc.after)
		return nil, nil

	case *parser.ContinueStmt:
		if len(g.loops) == 0 {
			return nil, fmt.Errorf("continue outside of loop")
		}
		g.current.NewBr(g.loops[len(g.loops)-1].cond)
		return nil, nil

	case *parser.RecordDecl:
		return nil, g.generateRecordDecl(n)

	case *parser.EnumDecl:
		return nil, g.generateEnumDecl(n)

	case *parser.BlockStmt:
		return g.generateBlock(n)

	case *parser.ReturnStmt:
		var val value.Value
		var err error
		if n.Value != nil {
			val, err = g.generateExpr(n.Value)
			if err != nil {
				return nil, err
			}
			var coerceErr error
			val, coerceErr = g.coerceToInterfaceReturn(val, g.currentReturnType)
			if coerceErr != nil {
				return nil, coerceErr
			}
			val = g.prepareReturn(val)
		}
		g.releaseAllScopes()

		retType := g.current.Parent.Sig.RetType
		if retType.Equal(types.Void) {
			g.current.NewRet(nil)
		} else {
			if val == nil {
				g.current.NewRet(g.defaultReturnValue(retType))
			} else {
				val = g.coerceToLLVMType(val, retType)
				g.current.NewRet(val)
			}
		}
		return nil, nil

	case *parser.ExprStmt:
		val, err := g.generateExpr(n.Expr)
		if err == nil && val != nil && g.isHeapType(val.Type()) {
			g.emitRelease(val)
		}
		return val, err

	default:
		return nil, fmt.Errorf("unsupported expression node in codegen: %T", node)
	}
}

func (g *Generator) generateBinaryExpr(n *parser.BinaryExpr) (value.Value, error) {
	left, err := g.generateExpr(n.Left)
	if err != nil {
		return nil, err
	}
	right, err := g.generateExpr(n.Right)
	if err != nil {
		return nil, err
	}

	isFloat := left.Type().Equal(types.Double) || right.Type().Equal(types.Double)

	switch n.Operator {
	case "+":
		if isFloat {
			return g.current.NewFAdd(left, right), nil
		}
		return g.current.NewAdd(left, right), nil
	case "-":
		if isFloat {
			return g.current.NewFSub(left, right), nil
		}
		return g.current.NewSub(left, right), nil
	case "*":
		if isFloat {
			return g.current.NewFMul(left, right), nil
		}
		return g.current.NewMul(left, right), nil
	case "/":
		if isFloat {
			return g.current.NewFDiv(left, right), nil
		}
		return g.current.NewSDiv(left, right), nil
	case "%":
		return g.current.NewSRem(left, right), nil
	case "&":
		return g.current.NewAnd(left, right), nil
	case "|":
		return g.current.NewOr(left, right), nil
	case "^":
		return g.current.NewXor(left, right), nil
	case "<<":
		return g.current.NewShl(left, right), nil
	case ">>":
		return g.current.NewAShr(left, right), nil
	case "==":
		if isFloat {
			return g.current.NewFCmp(enum.FPredOEQ, left, right), nil
		}
		return g.current.NewICmp(enum.IPredEQ, left, right), nil
	case "!=":
		if isFloat {
			return g.current.NewFCmp(enum.FPredONE, left, right), nil
		}
		return g.current.NewICmp(enum.IPredNE, left, right), nil
	case "<":
		if isFloat {
			return g.current.NewFCmp(enum.FPredOLT, left, right), nil
		}
		return g.current.NewICmp(enum.IPredSLT, left, right), nil
	case ">":
		if isFloat {
			return g.current.NewFCmp(enum.FPredOGT, left, right), nil
		}
		return g.current.NewICmp(enum.IPredSGT, left, right), nil
	case "<=":
		if isFloat {
			return g.current.NewFCmp(enum.FPredOLE, left, right), nil
		}
		return g.current.NewICmp(enum.IPredSLE, left, right), nil
	case ">=":
		if isFloat {
			return g.current.NewFCmp(enum.FPredOGE, left, right), nil
		}
		return g.current.NewICmp(enum.IPredSGE, left, right), nil
	default:
		return nil, fmt.Errorf("unsupported binary operator in codegen: %s", n.Operator)
	}
}

func (g *Generator) generateVarDecl(n *parser.VarDecl) (value.Value, error) {
	if n.Pattern != nil {
		if n.Init == nil {
			return nil, fmt.Errorf("destructuring declaration requires an initializer")
		}
		tupleVal, err := g.generateExpr(n.Init)
		if err != nil {
			return nil, err
		}
		return g.generateDestructure(tupleVal, n.Pattern)
	}
	if n.Init == nil {
		alloc := g.newAlloca(types.I64)
		g.vars[n.Name] = alloc
		return alloc, nil
	}
	val, err := g.generateExpr(n.Init)
	if err != nil {
		return nil, err
	}

	// Interface coercion: wrap a class pointer in a fat pointer {obj_ptr, vtable_ptr}
	// when the declared type is an interface.
	if n.Type != nil {
		if nt, ok := n.Type.(*parser.NamedType); ok {
			if _, isIface := g.interfaceDecls[nt.Name]; isIface {
				if ptrType, ok2 := val.Type().(*types.PointerType); ok2 {
					if st, ok3 := ptrType.ElemType.(*types.StructType); ok3 {
						if ci, ok4 := g.classes[st.TypeName]; ok4 {
							if vtable, ok5 := ci.vtables[nt.Name]; ok5 {
								val, err = g.wrapInInterfaceFatPtr(val, vtable)
								if err != nil {
									return nil, err
								}
							}
						}
					}
				}
			}
		}
	}

	alloc := g.newAlloca(val.Type())
	g.vars[n.Name] = alloc
	g.current.NewStore(val, alloc)

	if g.isHeapType(val.Type()) {
		// Copy from another heap var: retain so both variables co-own the object.
		// Fresh allocations (RecordLiteral via soyuz_alloc) already start at refcount=1.
		if id, ok := n.Init.(*parser.Identifier); ok && g.heapVars[id.Name] {
			g.emitRetain(val)
		}
		g.heapVars[n.Name] = true
		g.ownVar(n.Name)
	}

	// Track Task[T] variables so their handle is dropped at scope exit.
	if g.isTaskType(g.check.NodeTypes[n]) {
		g.ownTaskVar(n.Name)
	}

	// M8: Track sync guard variables so they are unlocked at scope exit.
	if unlockFn := g.syncGuardUnlockFn(g.check.NodeTypes[n]); unlockFn != "" {
		g.ownSyncGuard(n.Name, unlockFn)
	}

	// M14: Track Arc[T] variables so srt_arc_release is called at scope exit.
	if g.isArcType(g.check.NodeTypes[n]) {
		g.ownArcVar(n.Name)
	}

	return val, nil
}

// isArcType returns true when t is Arc[T] (SpecializedType with base ClassType "Arc").
func (g *Generator) isArcType(t checker.Type) bool {
	st, ok := t.(*checker.SpecializedType)
	if !ok {
		return false
	}
	ct, ok2 := st.Base.(*checker.ClassType)
	return ok2 && ct.Name == "Arc"
}

// syncGuardUnlockFn returns the runtime unlock function name for a sync guard type,
// or "" if the type is not a sync guard.
func (g *Generator) syncGuardUnlockFn(t checker.Type) string {
	if st, ok := t.(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 {
			switch ct.Name {
			case "MutexGuard":
				return "srt_mutex_unlock"
			case "ReadGuard", "WriteGuard":
				return "srt_rwlock_unlock"
			}
		}
	}
	return ""
}

// isTaskType returns true if the checker type is Task[T].
func (g *Generator) isTaskType(t checker.Type) bool {
	if st, ok := t.(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 {
			return ct.Name == "Task"
		}
	}
	return false
}

// wrapInInterfaceFatPtr packs a class pointer and a vtable into a SoyuzClosure fat pointer,
// returning the result as i8* (the runtime representation of an interface value).
func (g *Generator) wrapInInterfaceFatPtr(objPtr value.Value, vtable *ir.Global) (value.Value, error) {
	closureRaw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 16), constant.NewNull(types.I8Ptr), constant.NewNull(types.I8Ptr))
	closureStructPtr := g.current.NewBitCast(closureRaw, types.NewPointer(g.closureType))

	objAsI8 := g.current.NewBitCast(objPtr, types.I8Ptr)
	objField := g.current.NewGetElementPtr(g.closureType, closureStructPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(objAsI8, objField)

	vtableAsI8 := g.current.NewBitCast(vtable, types.I8Ptr)
	vtableField := g.current.NewGetElementPtr(g.closureType, closureStructPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(vtableAsI8, vtableField)

	return g.current.NewBitCast(closureStructPtr, types.I8Ptr), nil
}

// coerceClassToInterface wraps a concrete class pointer as an interface fat pointer.
func (g *Generator) coerceClassToInterface(val value.Value, ifaceName string) (value.Value, error) {
	if ptrType, ok := val.Type().(*types.PointerType); ok {
		if st, ok2 := ptrType.ElemType.(*types.StructType); ok2 {
			if ci, ok3 := g.classes[st.TypeName]; ok3 {
				if vtable, ok4 := ci.vtables[ifaceName]; ok4 {
					return g.wrapInInterfaceFatPtr(val, vtable)
				}
			}
		}
	}
	return val, nil
}

// coerceToInterfaceReturn wraps a class value when the function return type is an interface.
func (g *Generator) coerceToInterfaceReturn(val value.Value, retType checker.Type) (value.Value, error) {
	if val == nil || retType == nil {
		return val, nil
	}
	it, ok := retType.(*checker.InterfaceType)
	if !ok {
		return val, nil
	}
	return g.coerceClassToInterface(val, it.Name)
}

func (g *Generator) generateTupleExpr(n *parser.TupleExpr) (value.Value, error) {
	elems := make([]value.Value, len(n.Elements))
	elemTypes := make([]types.Type, len(n.Elements))
	for i, e := range n.Elements {
		v, err := g.generateExpr(e)
		if err != nil {
			return nil, err
		}
		elems[i] = v
		elemTypes[i] = v.Type()
	}
	st := types.NewStruct(elemTypes...)
	size := constant.NewInt(types.I64, int64(len(elems))*8)
	rawPtr := g.current.NewCall(g.findBuiltin("soyuz_alloc"), size, constant.NewNull(types.I8Ptr), constant.NewNull(types.I8Ptr))
	structPtr := g.current.NewBitCast(rawPtr, types.NewPointer(st))
	for i, v := range elems {
		fieldPtr := g.current.NewGetElementPtr(st, structPtr,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(i)))
		g.current.NewStore(v, fieldPtr)
	}
	return structPtr, nil
}

func (g *Generator) generateDestructure(tupleVal value.Value, pat parser.Pattern) (value.Value, error) {
	tp, ok := pat.(*parser.TuplePattern)
	if !ok {
		return nil, fmt.Errorf("only tuple patterns supported in val destructuring")
	}
	ptrType, ok := tupleVal.Type().(*types.PointerType)
	if !ok {
		return nil, fmt.Errorf("destructuring: expected tuple pointer, got %v", tupleVal.Type())
	}
	st, ok := ptrType.ElemType.(*types.StructType)
	if !ok {
		return nil, fmt.Errorf("destructuring: expected struct pointer, got %v", ptrType.ElemType)
	}
	for i, elem := range tp.Elements {
		ptr := g.current.NewGetElementPtr(st, tupleVal,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(i)))
		fieldVal := g.current.NewLoad(st.Fields[i], ptr)
		switch e := elem.(type) {
		case *parser.BindingPattern:
			alloc := g.newAlloca(fieldVal.Type())
			g.current.NewStore(fieldVal, alloc)
			g.vars[e.Name] = alloc
		case *parser.WildcardPattern:
			// skip
		}
	}
	return tupleVal, nil
}

func (g *Generator) generateCallArgs(args []parser.Node) ([]value.Value, error) {
	var res []value.Value
	for _, a := range args {
		av, err := g.generateExpr(a)
		if err != nil {
			return nil, err
		}
		if g.isHeapType(av.Type()) {
			// If it's an existing owned value, retain it for the callee.
			if _, ok := a.(*parser.Identifier); ok {
				g.emitRetain(av)
			} else if _, ok := a.(*parser.MemberExpr); ok {
				g.emitRetain(av)
			}
		}
		res = append(res, av)
	}
	return res, nil
}

func (g *Generator) generateCallExpr(n *parser.CallExpr) (value.Value, error) {
	// M4b: curried call — generate a closure instead of a direct call
	if af, ok := g.check.CurriedCalls[n]; ok {
		return g.generateArrowFunc(af)
	}

	// Safe navigation method call: obj?.method(args)
	if sn, ok := n.Callee.(*parser.SafeNavExpr); ok {
		return g.generateSafeNavMethodCall(sn, n)
	}

	// 1. Built-in print
	if id, ok := n.Callee.(*parser.Identifier); ok && id.Name == "print" {
		return g.generatePrint(n)
	}

	// 2. Enum variant constructor (concrete enums first, then generic)
	if id, ok := n.Callee.(*parser.Identifier); ok {
		for _, ei := range g.enums {
			if vi, ok := ei.variants[id.Name]; ok {
				return g.generateEnumConstructor(ei, vi, n)
			}
		}
		// Generic enum constructor: specialize via the checker's return type.
		if ft, ok := g.check.Specializations[n]; ok {
			llvmRet := g.mapTypeToLLVM(ft.Return)
			if ptr, ok := llvmRet.(*types.PointerType); ok {
				if st, ok2 := ptr.ElemType.(*types.StructType); ok2 {
					for _, ei := range g.enums {
						if ei.typ == st {
							if vi, ok3 := ei.variants[id.Name]; ok3 {
								return g.generateEnumConstructor(ei, vi, n)
							}
						}
					}
				}
			}
		}
	}

	// 3. Generic function — monomorphize
	if id, ok := n.Callee.(*parser.Identifier); ok {
		if decl, ok := g.genericDecls[id.Name]; ok {
			if st, ok := g.check.Specializations[n]; ok {
				fn, err := g.generateSpecializedFunc(id.Name, decl, st)
				if err != nil {
					return nil, err
				}
				args, err := g.generateCallArgs(n.Args)
				if err != nil {
					return nil, err
				}
				return g.current.NewCall(fn, args...), nil
			}
		}
	}
	if se, ok := n.Callee.(*parser.SpecializedExpr); ok {
		if id, ok := se.Base.(*parser.Identifier); ok {
			if decl, ok := g.genericDecls[id.Name]; ok {
				if st, ok := g.check.Specializations[n]; ok {
					fn, err := g.generateSpecializedFunc(id.Name, decl, st)
					if err != nil {
						return nil, err
					}
					args, err := g.generateCallArgs(n.Args)
					if err != nil {
						return nil, err
					}
					return g.current.NewCall(fn, args...), nil
				}
			}
		}
	}

	// M14: Arc static constructor Arc.new(val).
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if ct, ok2 := g.check.NodeTypes[me.Object].(*checker.ClassType); ok2 && ct.Name == "Arc" && me.Property == "new" && len(n.Args) == 1 {
			return g.generateArcNew(n)
		}
	}

	// M14: Arc instance methods — clone, get, refcount.
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if st, ok2 := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok2 {
			if ct, ok3 := st.Base.(*checker.ClassType); ok3 && ct.Name == "Arc" {
				switch me.Property {
				case "clone":
					return g.generateArcClone(me.Object)
				case "get":
					return g.generateArcGet(me.Object, st)
				case "refcount":
					return g.generateArcRefcount(me.Object)
				}
			}
		}
	}

	// Channel static constructor — Channel.new(capacity: Int).
	// capacity=0 → rendezvous semantics; N>0 → buffered.
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if ct, ok2 := g.check.NodeTypes[me.Object].(*checker.ClassType); ok2 && me.Property == "new" && ct.Name == "Channel" {
			return g.generateChannelNew(n)
		}
	}

	// Channel[T] instance methods.
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if st, ok2 := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok2 {
			if ct, ok3 := st.Base.(*checker.ClassType); ok3 && ct.Name == "Channel" {
				switch me.Property {
				case "send":
					return g.generateChannelSend(me.Object, n)
				case "recv":
					return g.generateChannelRecv(me.Object, st, false)
				case "tryRecv":
					return g.generateChannelRecv(me.Object, st, true)
				case "close":
					return g.generateChannelClose(me.Object, "srt_chan_close")
				case "isClosed":
					return g.generateChannelIsClosed(me.Object)
				}
			}
		}
	}

	// M8: Sync constructors — Mutex.new, RwLock.new, Atomic.new.
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if ct, ok2 := g.check.NodeTypes[me.Object].(*checker.ClassType); ok2 && me.Property == "new" {
			switch ct.Name {
			case "Mutex":
				return g.generateMutexNew(n)
			case "RwLock":
				return g.generateRwLockNew(n)
			case "Atomic":
				return g.generateAtomicNew(n)
			}
		}
	}

	// M8: Sync instance methods — mutex.lock(), rwlock.read/write(), atomic.load/store/add/cas().
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if st, ok2 := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok2 {
			if ct, ok3 := st.Base.(*checker.ClassType); ok3 {
				switch ct.Name {
				case "Mutex":
					if me.Property == "lock" {
						return g.generateMutexLock(me.Object)
					}
				case "RwLock":
					switch me.Property {
					case "read":
						return g.generateRwLockRead(me.Object)
					case "write":
						return g.generateRwLockWrite(me.Object)
					}
				case "Atomic":
					switch me.Property {
					case "load":
						return g.generateAtomicLoad(me.Object, st)
					case "store":
						return g.generateAtomicStore(me.Object, n)
					case "add":
						return g.generateAtomicAdd(me.Object, n, st)
					case "compareAndSwap":
						return g.generateAtomicCas(me.Object, n)
					}
				}
			}
		}
	}

	// M6: Task.all / Task.any / Task.allSettled — static combinators.
	// M18: Task.fan — fan-out paralelo.
	// M19: Task.pipe — pipeline paralelo com channels.
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if ct, ok2 := g.check.NodeTypes[me.Object].(*checker.ClassType); ok2 && ct.Name == "Task" {
			switch me.Property {
			case "all", "allSettled":
				return g.generateTaskAll(n)
			case "fan":
				return g.generateTaskFan(n)
			case "pipe":
				return g.generateTaskPipeline(n)
			case "gather":
				return g.generateTaskGather(n)
			}
		}
	}

	// M7: TaskHandle.current() — wraps srt_task_handle_current() in Option[TaskHandle].
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		if ct, ok2 := g.check.NodeTypes[me.Object].(*checker.ClassType); ok2 && ct.Name == "TaskHandle" {
			if me.Property == "current" {
				return g.generateTaskHandleCurrent()
			}
		}
	}

	// 4. Method call: obj.method(args) — or enum dot constructor: Enum.Variant(args)
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		objCheckerType := g.check.NodeTypes[me.Object]
		// Enum dot constructor: Enum.Variant(args) — concrete or generic.
		if et, isEnum := objCheckerType.(*checker.EnumType); isEnum {
			if ei, exists := g.enums[et.Name]; exists {
				if vi, ok2 := ei.variants[me.Property]; ok2 {
					return g.generateEnumConstructor(ei, vi, n)
				}
			}
			// Generic enum: specialize from the checker's Specializations entry.
			if decl, ok2 := g.genericEnumDecls[et.Name]; ok2 {
				if ft, ok3 := g.check.Specializations[n]; ok3 {
					llvmRet := g.mapTypeToLLVM(ft.Return)
					if ptr, ok4 := llvmRet.(*types.PointerType); ok4 {
						if st, ok5 := ptr.ElemType.(*types.StructType); ok5 {
							for _, ei2 := range g.enums {
								if ei2.typ == st {
									if vi2, ok6 := ei2.variants[me.Property]; ok6 {
										return g.generateEnumConstructor(ei2, vi2, n)
									}
								}
							}
						}
					}
					// Fallback: build substitution from the specialization return type.
					if st2, ok4 := ft.Return.(*checker.SpecializedType); ok4 {
						sub := make(map[string]types.Type)
						for i, gp := range decl.Generics {
							if i < len(st2.Params) {
								sub[gp.Name] = g.mapTypeToLLVM(st2.Params[i])
							}
						}
						ei3, err := g.getOrCreateSpecializedEnum(decl, sub)
						if err == nil {
							if vi3, ok5 := ei3.variants[me.Property]; ok5 {
								return g.generateEnumConstructor(ei3, vi3, n)
							}
						}
					}
				}
				// Last resort: infer from argument types.
				if len(n.Args) > 0 {
					arg0, err := g.generateExpr(n.Args[0])
					if err == nil {
						sub := make(map[string]types.Type)
						if len(decl.Generics) > 0 {
							sub[decl.Generics[0].Name] = arg0.Type()
						}
						ei4, err2 := g.getOrCreateSpecializedEnum(decl, sub)
						if err2 == nil {
							if vi4, ok5 := ei4.variants[me.Property]; ok5 {
								fakeCall := &parser.CallExpr{Args: []parser.Node{n.Args[0]}}
								_ = fakeCall
								return g.generateEnumConstructorWithValues(ei4, vi4, []value.Value{arg0})
							}
						}
					}
				}
			}
		}
		isMethod := false
		switch t := objCheckerType.(type) {
		case *checker.ClassType, *checker.InterfaceType:
			isMethod = true
		case *checker.SpecializedType:
			if _, ok := t.Base.(*checker.ClassType); ok {
				isMethod = true
			}
		case *checker.BasicType:
			isMethod = true
		}
		if isMethod {
			return g.generateMethodCall(me, n)
		}
	}

	// 5. Regular call
	var callee value.Value
	if id, ok := n.Callee.(*parser.Identifier); ok {
		if f := g.findFunc(id.Name); f != nil {
			callee = f
		}
	}
	if callee == nil {
		var err error
		callee, err = g.generateExpr(n.Callee)
		if err != nil {
			return nil, err
		}
	}

	callArgs := n.Args
	if synth, ok := g.check.SynthCallArgs[n]; ok {
		callArgs = append(callArgs, synth...)
	}
	args, err := g.generateCallArgs(callArgs)
	if err != nil {
		return nil, err
	}

	// Direct call when callee is a concrete function pointer; closure call otherwise.
	if ptrType, ok := callee.Type().(*types.PointerType); ok {
		if _, isFuncType := ptrType.ElemType.(*types.FuncType); isFuncType {
			return g.current.NewCall(callee, args...), nil
		}
	}
	return g.callClosureI8Ptr(n, callee, args)
}

func (g *Generator) generatePrint(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) == 0 {
		return nil, fmt.Errorf("print requires at least one argument")
	}
	arg, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	printf := g.findBuiltin("printf")

	var fmtStr string
	var printArgs []value.Value
	if soyuzType := g.checkerTypeForExpr(n.Args[0]); g.isListOrMapType(soyuzType) {
		strVal, err := g.emitCollectionToString(arg, soyuzType)
		if err != nil {
			return nil, err
		}
		fmtStr = "%s\n\x00"
		printArgs = []value.Value{g.strData(strVal)}
	} else {
		switch {
		case arg.Type().Equal(g.soyuzStringPtrType):
			fmtStr = "%s\n\x00"
			printArgs = []value.Value{g.strData(arg)}
		case arg.Type().Equal(types.I8Ptr):
			fmtStr = "%s\n\x00"
			printArgs = []value.Value{arg}
		case arg.Type().Equal(types.I64):
			fmtStr = "%lld\n\x00"
			printArgs = []value.Value{arg}
		case arg.Type().Equal(types.I1):
			fmtStr = "%d\n\x00"
			printArgs = []value.Value{g.current.NewZExt(arg, types.I32)}
		case arg.Type().Equal(types.Double):
			fmtStr = "%f\n\x00"
			printArgs = []value.Value{arg}
		default:
			return nil, fmt.Errorf("unsupported type for print: %s", arg.Type())
		}
	}

	cs := constant.NewCharArrayFromString(fmtStr)
	glob := g.module.NewGlobalDef("", cs)
	glob.Immutable = true
	ptr := g.current.NewGetElementPtr(cs.Type(), glob,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I64, 0))
	g.current.NewCall(printf, append([]value.Value{ptr}, printArgs...)...)
	return nil, nil
}

// generateEnumConstructorWithValues is like generateEnumConstructor but takes pre-evaluated arg values.
func (g *Generator) generateEnumConstructorWithValues(ei enumInfo, vi variantInfo, args []value.Value) (value.Value, error) {
	dtorArg, traceArg := g.enumRCFnArgs(ei.typ.TypeName)
	raw := g.current.NewCall(g.findBuiltin("soyuz_alloc"), constant.NewInt(types.I64, 72), dtorArg, traceArg)
	structPtr := g.current.NewBitCast(raw, types.NewPointer(ei.typ))
	tagPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(constant.NewInt(types.I64, int64(vi.tag)), tagPtr)
	if len(args) > 0 {
		payloadPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		if len(args) > 1 {
			fieldTypes := make([]types.Type, len(args))
			for i, arg := range args {
				fieldTypes[i] = arg.Type()
			}
			payloadType := types.NewStruct(fieldTypes...)
			castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(payloadType))
			for i, val := range args {
				if g.isHeapType(val.Type()) {
					g.emitRetain(val)
				}
				fieldPtr := g.current.NewGetElementPtr(payloadType, castPtr,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
				g.current.NewStore(val, fieldPtr)
			}
			return structPtr, nil
		}
		val := args[0]
		if g.isHeapType(val.Type()) {
			g.emitRetain(val)
		}
		castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(val.Type()))
		g.current.NewStore(val, castPtr)
	}
	return structPtr, nil
}

func (g *Generator) generateEnumConstructor(ei enumInfo, vi variantInfo, n *parser.CallExpr) (value.Value, error) {
	dtorArg, traceArg := g.enumRCFnArgs(ei.typ.TypeName)

	// Enum layout is 72 bytes: 8 (tag i64) + 64 ([64 x i8] payload).
	raw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 72), dtorArg, traceArg)
	structPtr := g.current.NewBitCast(raw, types.NewPointer(ei.typ))

	tagPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(constant.NewInt(types.I64, int64(vi.tag)), tagPtr)

	if len(n.Args) > 0 {
		payloadPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		if len(n.Args) > 1 {
			values := make([]value.Value, len(n.Args))
			fieldTypes := make([]types.Type, len(n.Args))
			for i, arg := range n.Args {
				val, err := g.generateExpr(arg)
				if err != nil {
					return nil, err
				}
				values[i] = val
				fieldTypes[i] = val.Type()
			}
			payloadType := types.NewStruct(fieldTypes...)
			castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(payloadType))
			for i, val := range values {
				if g.isHeapType(val.Type()) {
					g.emitRetain(val)
				}
				fieldPtr := g.current.NewGetElementPtr(payloadType, castPtr,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
				g.current.NewStore(val, fieldPtr)
			}
			return structPtr, nil
		}
		val, err := g.generateExpr(n.Args[0])
		if err != nil {
			return nil, err
		}
		// Retain heap-typed payload: the enum struct now co-owns it.
		if g.isHeapType(val.Type()) {
			g.emitRetain(val)
		}
		castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(val.Type()))
		g.current.NewStore(val, castPtr)
	}
	return structPtr, nil
}

func (g *Generator) generateInterpolatedString(n *parser.InterpolatedString) (value.Value, error) {
	var b strings.Builder
	var args []value.Value

	for _, part := range n.Parts {
		val, err := g.generateExpr(part)
		if err != nil {
			return nil, err
		}
		if soyuzType := g.checkerTypeForExpr(part); g.isListOrMapType(soyuzType) {
			strVal, err := g.emitCollectionToString(val, soyuzType)
			if err != nil {
				return nil, err
			}
			b.WriteString("%s")
			args = append(args, g.strData(strVal))
			continue
		}
		switch {
		case val.Type().Equal(g.soyuzStringPtrType):
			b.WriteString("%s")
			args = append(args, g.strData(val))
		case val.Type().Equal(types.I64):
			b.WriteString("%lld")
			args = append(args, val)
		case val.Type().Equal(types.I32):
			b.WriteString("%c")
			args = append(args, val)
		case val.Type().Equal(types.Double):
			b.WriteString("%f")
			args = append(args, val)
		case val.Type().Equal(types.I1):
			b.WriteString("%d")
			args = append(args, g.current.NewZExt(val, types.I32))
		default:
			b.WriteString("%p")
			args = append(args, val)
		}
	}

	b.WriteByte(0)
	fmtStr := b.String()
	cs := constant.NewCharArrayFromString(fmtStr)
	glob := g.module.NewGlobalDef("", cs)
	glob.Immutable = true
	ptrFmt := g.current.NewGetElementPtr(cs.Type(), glob,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I64, 0))

	malloc := g.findBuiltin("malloc")
	sprintf := g.findBuiltin("sprintf")
	buf := g.current.NewCall(malloc, constant.NewInt(types.I64, 1024))
	g.current.NewCall(sprintf, append([]value.Value{buf, ptrFmt}, args...)...)
	result := g.current.NewCall(g.findFunc("soyuz_str_from_printf_buf"), buf)
	return result, nil
}

// generateMethodCall handles obj.method(args) for both static (class) and dynamic (interface) dispatch.
func (g *Generator) generateMethodCall(me *parser.MemberExpr, n *parser.CallExpr) (value.Value, error) {
	obj, err := g.generateExpr(me.Object)
	if err != nil {
		return nil, err
	}
	// Retain the object for the duration of the method call — but NOT for task
	// handles, which use srt_task_t.refcount and are not RC-managed via soyuz_retain.
	if g.isHeapType(obj.Type()) && !g.isTaskType(g.check.NodeTypes[me.Object]) {
		if _, ok := me.Object.(*parser.Identifier); ok {
			g.emitRetain(obj)
		} else if _, ok := me.Object.(*parser.MemberExpr); ok {
			g.emitRetain(obj)
		}
	}

	// M-22/24/25: Task[T] callback methods evaluate their own args — skip generateCallArgs
	// to avoid generating a leaked first closure from the pre-evaluation step.
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 && ct.Name == "Task" {
			switch me.Property {
			case "tap":
				return g.generateTaskTap(me, n, obj)
			case "always":
				return g.generateTaskAlways(me, n, obj)
			}
		}
	}

	args, err := g.generateCallArgs(n.Args)
	if err != nil {
		return nil, err
	}

	// Special handling for built-in List methods
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 && ct.Name == "List" {
			switch me.Property {
			case "size":
				ptr := g.current.NewGetElementPtr(g.mapTypeToLLVM(st).(*types.PointerType).ElemType, obj,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
				return g.current.NewLoad(types.I64, ptr), nil
			case "append", "add":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				elem := args[0]
				if g.isHeapType(elem.Type()) {
					g.emitRetain(elem) // list owns the element
				}
				var valCast value.Value
				if elem.Type().Equal(types.I64) {
					valCast = g.current.NewIntToPtr(elem, types.I8Ptr)
				} else {
					valCast = g.current.NewBitCast(elem, types.I8Ptr)
				}
				g.current.NewCall(g.findFunc("soyuz_list_append"), objAsI8, valCast)
				return nil, nil
			case "get":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				raw := g.current.NewCall(g.findFunc("soyuz_list_get"), objAsI8, args[0])
				targetType := g.mapTypeToLLVM(st.Params[0])
				if targetType.Equal(types.I64) {
					return g.current.NewPtrToInt(raw, types.I64), nil
				}
				result := g.current.NewBitCast(raw, targetType)
				if g.isHeapType(targetType) {
					g.emitRetain(result) // caller owns the returned element
				}
				return result, nil
			case "isEmpty":
				listTyped := g.current.NewBitCast(obj, types.NewPointer(g.structs["SoyuzList"].typ))
				sizePtr := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
				sizeVal := g.current.NewLoad(types.I64, sizePtr)
				return g.current.NewICmp(enum.IPredEQ, sizeVal, constant.NewInt(types.I64, 0)), nil
			case "map":
				return g.generateListMap(n, obj, st, args)
			case "filter":
				return g.generateListFilter(n, obj, st, args)
			case "reduce":
				return g.generateListReduce(n, obj, st, args)
			case "join":
				return g.generateListJoin(obj, st, args)
			case "set":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				elem := args[1]
				var valCast value.Value
				if elem.Type().Equal(types.I64) {
					valCast = g.current.NewIntToPtr(elem, types.I8Ptr)
				} else {
					valCast = g.current.NewBitCast(elem, types.I8Ptr)
				}
				if g.isHeapType(elem.Type()) {
					g.emitRetain(elem)
					g.current.NewCall(g.findFunc("soyuz_list_set_rc"), objAsI8, args[0], valCast)
				} else {
					g.current.NewCall(g.findFunc("soyuz_list_set"), objAsI8, args[0], valCast)
				}
				return nil, nil
			case "remove":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				raw := g.current.NewCall(g.findFunc("soyuz_list_remove"), objAsI8, args[0])
				targetType := g.mapTypeToLLVM(st.Params[0])
				if targetType.Equal(types.I64) {
					return g.current.NewPtrToInt(raw, types.I64), nil
				}
				return g.current.NewBitCast(raw, targetType), nil
			case "pop":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				raw := g.current.NewCall(g.findFunc("soyuz_list_pop"), objAsI8)
				targetType := g.mapTypeToLLVM(st.Params[0])
				if targetType.Equal(types.I64) {
					return g.current.NewPtrToInt(raw, types.I64), nil
				}
				return g.current.NewBitCast(raw, targetType), nil
			case "prepend":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				elem := args[0]
				if g.isHeapType(elem.Type()) {
					g.emitRetain(elem)
				}
				var valCast value.Value
				if elem.Type().Equal(types.I64) {
					valCast = g.current.NewIntToPtr(elem, types.I8Ptr)
				} else {
					valCast = g.current.NewBitCast(elem, types.I8Ptr)
				}
				g.current.NewCall(g.findFunc("soyuz_list_prepend"), objAsI8, valCast)
				return nil, nil
			case "clear":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				elemLLVMType := g.mapTypeToLLVM(st.Params[0])
				if g.isHeapType(elemLLVMType) {
					g.current.NewCall(g.findFunc("soyuz_list_clear_rc"), objAsI8)
				} else {
					g.current.NewCall(g.findFunc("soyuz_list_clear_primitive"), objAsI8)
				}
				return nil, nil
			case "copy":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				elemLLVMType := g.mapTypeToLLVM(st.Params[0])
				elemIsHeap := int64(0)
				if g.isHeapType(elemLLVMType) {
					elemIsHeap = 1
				}
				raw := g.current.NewCall(g.findFunc("soyuz_list_copy"), objAsI8,
					constant.NewInt(types.I64, elemIsHeap))
				listPtrType := types.NewPointer(g.structs["SoyuzList"].typ)
				return g.current.NewBitCast(raw, listPtrType), nil
			case "concat":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				otherAsI8 := g.current.NewBitCast(args[0], types.I8Ptr)
				elemLLVMType := g.mapTypeToLLVM(st.Params[0])
				elemIsHeap := int64(0)
				if g.isHeapType(elemLLVMType) {
					elemIsHeap = 1
				}
				raw := g.current.NewCall(g.findFunc("soyuz_list_concat"), objAsI8, otherAsI8,
					constant.NewInt(types.I64, elemIsHeap))
				listPtrType := types.NewPointer(g.structs["SoyuzList"].typ)
				return g.current.NewBitCast(raw, listPtrType), nil
			case "iter":
				return g.generateListIter(obj), nil
			}
		}
	}

	// Iterator[T] methods
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 && ct.Name == "Iterator" {
			switch me.Property {
			case "next":
				return g.generateIteratorNext(obj, st)
			case "isEmpty":
				return g.generateIteratorIsEmpty(obj)
			}
		}
	}

	// Special handling for built-in Map methods
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 && ct.Name == "Map" {
			switch me.Property {
			case "size":
				ptr := g.current.NewGetElementPtr(g.mapTypeToLLVM(st).(*types.PointerType).ElemType, obj,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
				return g.current.NewLoad(types.I64, ptr), nil
			case "set":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				if g.isHeapType(args[0].Type()) {
					g.emitRetain(args[0]) // map owns the key
				}
				if g.isHeapType(args[1].Type()) {
					g.emitRetain(args[1]) // map owns the value
				}
				keyCast := g.castToI8Ptr(args[0])
				valCast := g.castToI8Ptr(args[1])
				g.current.NewCall(g.findFunc("soyuz_map_set"), objAsI8, keyCast, valCast)
				return nil, nil
			case "get":
				objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
				keyCast := g.castToI8Ptr(args[0])
				raw := g.current.NewCall(g.findFunc("soyuz_map_get"), objAsI8, keyCast)
				targetType := g.mapTypeToLLVM(st.Params[1])
				result := g.castFromI8Ptr(raw, targetType)
				if g.isHeapType(targetType) {
					g.emitRetain(result) // caller owns the returned value
				}
				return result, nil
			case "keys":
				return g.generateMapKeys(obj, st)
			case "values":
				return g.generateMapValues(obj, st)
			case "iter":
				keys, err := g.generateMapKeys(obj, st)
				if err != nil {
					return nil, err
				}
				return g.generateListIter(keys), nil
			}
		}
	}

	// M7: TaskHandle instance methods: cancelled() and progress(f).
	if ct, ok := g.check.NodeTypes[me.Object].(*checker.ClassType); ok && ct.Name == "TaskHandle" {
		switch me.Property {
		case "cancelled":
			raw := g.current.NewCall(g.findFunc("srt_task_cancelled"), obj)
			return g.current.NewTrunc(raw, types.I1), nil
		case "progress":
			if len(args) >= 1 {
				g.current.NewCall(g.findFunc("srt_task_set_progress"), obj, args[0])
			}
			return nil, nil
		}
	}

	// Task[T] methods: await() and detach()
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 && ct.Name == "Task" {
			switch me.Property {
			case "await":
				raw := g.current.NewCall(g.findFunc("srt_await"), obj)
				// Null the alloca so srt_drop_task_handle is a no-op at scope exit.
				if ident, ok3 := me.Object.(*parser.Identifier); ok3 {
					if alloc, exists := g.vars[ident.Name]; exists {
						g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
					}
				}
				retCheckerType := g.check.NodeTypes[n]
				if retCheckerType == nil {
					if ft, ok3 := g.check.Specializations[n]; ok3 {
						retCheckerType = ft.Return
					}
				}
				if retCheckerType == nil {
					return raw, nil
				}
				retLLVMType := g.mapTypeToLLVM(retCheckerType)
				return g.castFromI8Ptr(raw, retLLVMType), nil
			case "detach":
				g.current.NewCall(g.findFunc("srt_detach"), obj)
				// Null the alloca so srt_drop_task_handle is a no-op at scope exit.
				if ident, ok3 := me.Object.(*parser.Identifier); ok3 {
					if alloc, exists := g.vars[ident.Name]; exists {
						g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
					}
				}
				return nil, nil
			case "cancel":
				// M-10: cancel task + propagate to all non-detached children recursively.
				g.current.NewCall(g.findFunc("srt_cancel"), obj)
				return nil, nil
				// tap/always/then/catch are handled before generateCallArgs above.
			}
		}
	}

	// Primitive / extension method dispatch
	if bt, ok := g.check.NodeTypes[me.Object].(*checker.BasicType); ok {
		if bt.Name == "String" && (me.Property == "byteAt" || me.Property == "unicodeAt") {
			cfn := "soyuz_str_byte_at"
			asChar := false
			if me.Property == "unicodeAt" {
				cfn = "soyuz_str_unicode_at"
				asChar = true
			}
			raw := g.current.NewCall(g.findFunc(cfn), obj, args[0])
			return g.emitIntToOption(raw, asChar)
		}
		if bt.Name == "String" && (me.Property == "indexOf" || me.Property == "lastIndexOf") {
			cfn := "soyuz_str_index_of"
			if me.Property == "lastIndexOf" {
				cfn = "soyuz_str_last_index_of"
			}
			raw := g.current.NewCall(g.findFunc(cfn), obj, args[0])
			return g.emitIntToOption(raw, false)
		}
		if cfn := primitiveMethodCFunc(bt.Name, me.Property); cfn != "" {
			if fn := g.findFunc(cfn); fn != nil {
				callArgs := append([]value.Value{obj}, args...)
				raw := g.current.NewCall(fn, callArgs...)
				if stringMethodReturnsBool(me.Property) {
					return g.current.NewTrunc(raw, types.I1), nil
				}
				return raw, nil
			}
			// fn not declared in this module; fall through to StringExtensions
		}
		if variants, ok := g.extensionMethods[bt.Name][me.Property]; ok {
			fn := classMethodByArity(variants, len(args))
			if fn != nil {
				objAsI8 := g.packExtendSelf(obj)
				allArgs := append([]value.Value{objAsI8}, args...)
				return g.current.NewCall(fn, allArgs...), nil
			}
		}
		if bt.Name == "String" {
			if ci, exists := g.classes["StringExtensions"]; exists {
				if variants, ok2 := ci.methods[me.Property]; ok2 {
					fn := classMethodByArity(variants, len(args))
					if fn != nil {
						objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
						allArgs := append([]value.Value{objAsI8}, args...)
						return g.current.NewCall(fn, allArgs...), nil
					}
				}
			}
		}
		return nil, fmt.Errorf("%s não tem método '%s' no codegen", bt.Name, me.Property)
	}

	// Determine return type from checker specialization.
	var retType types.Type = types.Void
	if ft, ok := g.check.Specializations[n]; ok && ft != nil {
		retType = g.mapTypeToLLVM(ft.Return)
	}

	// Static dispatch: obj is a concrete class struct pointer.
	if ptrType, ok := obj.Type().(*types.PointerType); ok {
		if st, ok2 := ptrType.ElemType.(*types.StructType); ok2 {
			if ci, ok3 := g.classes[st.TypeName]; ok3 {
				if variants, ok4 := ci.methods[me.Property]; ok4 {
					fn := classMethodByArity(variants, len(args))
					if fn != nil {
						objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
						allArgs := append([]value.Value{objAsI8}, args...)
						return g.current.NewCall(fn, allArgs...), nil
					}
				}
			}
			if variants, ok := g.extensionMethods[st.TypeName][me.Property]; ok {
				fn := classMethodByArity(variants, len(args))
				if fn != nil {
					objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
					allArgs := append([]value.Value{objAsI8}, args...)
					return g.current.NewCall(fn, allArgs...), nil
				}
			}
		}
	}

	// Dynamic dispatch: obj is i8* (interface fat pointer).
	if obj.Type().Equal(types.I8Ptr) {
		objCheckerType := g.check.NodeTypes[me.Object]
		it, ok := objCheckerType.(*checker.InterfaceType)
		if !ok {
			return nil, fmt.Errorf("cannot dispatch method %s: object not a known interface", me.Property)
		}
		ifaceDecl, ok := g.interfaceDecls[it.Name]
		if !ok {
			return nil, fmt.Errorf("interface %s not found in codegen", it.Name)
		}
		slotIdx := -1
		for i, m := range ifaceDecl.Methods {
			if m.Name == me.Property {
				slotIdx = i
				break
			}
		}
		if slotIdx < 0 {
			return nil, fmt.Errorf("method %s not found in interface %s", me.Property, it.Name)
		}

		// Unpack fat pointer: SoyuzClosure { obj_ptr: i8*, vtable_ptr: i8* }
		closurePtr := g.current.NewBitCast(obj, types.NewPointer(g.closureType))

		objPtrField := g.current.NewGetElementPtr(g.closureType, closurePtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
		objPtr := g.current.NewLoad(types.I8Ptr, objPtrField)

		vtablePtrField := g.current.NewGetElementPtr(g.closureType, closurePtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		vtableRaw := g.current.NewLoad(types.I8Ptr, vtablePtrField)

		// Bitcast vtable i8* → [N x i8*]*
		nMethods := uint64(len(ifaceDecl.Methods))
		vtableArrType := types.NewArray(nMethods, types.I8Ptr)
		vtablePtr := g.current.NewBitCast(vtableRaw, types.NewPointer(vtableArrType))

		// GEP to the method slot and load the function pointer.
		fnSlotPtr := g.current.NewGetElementPtr(vtableArrType, vtablePtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I64, int64(slotIdx)))
		fnRaw := g.current.NewLoad(types.I8Ptr, fnSlotPtr)

		// Bitcast to concrete function type: (i8* __self, args...) → retType
		paramTypes := []types.Type{types.I8Ptr}
		for _, a := range args {
			paramTypes = append(paramTypes, a.Type())
		}
		fnType := types.NewFunc(retType, paramTypes...)
		fnPtr := g.current.NewBitCast(fnRaw, types.NewPointer(fnType))

		allArgs := append([]value.Value{objPtr}, args...)
		result := g.current.NewCall(fnPtr, allArgs...)
		if retType.Equal(types.Void) {
			return nil, nil
		}
		return result, nil
	}

	return nil, fmt.Errorf("cannot call method %s on type %s", me.Property, obj.Type())
}

func (g *Generator) generateListExpr(n *parser.ListExpr) (value.Value, error) {
	st, ok := g.check.NodeTypes[n].(*checker.SpecializedType)
	if !ok {
		return nil, fmt.Errorf("missing type for list expr in codegen")
	}
	elemSoyuzType := st.Params[0]
	elemType := g.mapTypeToLLVM(elemSoyuzType)

	var dtorName string
	if g.isHeapType(elemType) {
		dtorName = "soyuz_list_dtor_rc"
	} else {
		dtorName = "soyuz_list_dtor_primitive"
	}
	dtor := g.findFunc(dtorName)

	raw := g.current.NewCall(g.findFunc("soyuz_list_new"),
		constant.NewInt(types.I64, int64(len(n.Elements))),
		g.current.NewBitCast(dtor, types.I8Ptr))

	listPtr := g.current.NewBitCast(raw, g.mapTypeToLLVM(st))

	for _, e := range n.Elements {
		val, err := g.generateExpr(e)
		if err != nil {
			return nil, err
		}
		if g.isHeapType(val.Type()) {
			g.emitRetain(val)
		}
		// List data stores i8*
		var valCast value.Value
		if val.Type().Equal(types.I64) {
			valCast = g.current.NewIntToPtr(val, types.I8Ptr)
		} else {
			valCast = g.current.NewBitCast(val, types.I8Ptr)
		}
		g.current.NewCall(g.findFunc("soyuz_list_append"), raw, valCast)
	}

	return listPtr, nil
}

func (g *Generator) generateMapExpr(n *parser.MapExpr) (value.Value, error) {
	st, ok := g.check.NodeTypes[n].(*checker.SpecializedType)
	if !ok {
		return nil, fmt.Errorf("missing type for map expr in codegen")
	}
	keySoyuzType := st.Params[0]

	isStringKey := 0
	if bt, ok := keySoyuzType.(*checker.BasicType); ok && bt.Name == "String" {
		isStringKey = 1
	}

	var dtorName string
	keyHeap := g.isHeapType(g.mapTypeToLLVM(st.Params[0]))
	valHeap := g.isHeapType(g.mapTypeToLLVM(st.Params[1]))
	if keyHeap && valHeap {
		dtorName = "soyuz_map_dtor_rc_both"
	} else if keyHeap {
		dtorName = "soyuz_map_dtor_rc_key"
	} else if valHeap {
		dtorName = "soyuz_map_dtor_rc_val"
	} else {
		dtorName = "soyuz_map_dtor_primitive"
	}
	dtor := g.findFunc(dtorName)

	raw := g.current.NewCall(g.findFunc("soyuz_map_new"),
		constant.NewInt(types.I64, int64(isStringKey)),
		g.current.NewBitCast(dtor, types.I8Ptr))

	mapPtr := g.current.NewBitCast(raw, g.mapTypeToLLVM(st))

	for _, entry := range n.Entries {
		k, err := g.generateExpr(entry.Key)
		if err != nil {
			return nil, err
		}
		v, err := g.generateExpr(entry.Value)
		if err != nil {
			return nil, err
		}

		if g.isHeapType(k.Type()) {
			g.emitRetain(k)
		}
		if g.isHeapType(v.Type()) {
			g.emitRetain(v)
		}

		g.current.NewCall(g.findFunc("soyuz_map_set"), raw, g.castToI8Ptr(k), g.castToI8Ptr(v))
	}

	return mapPtr, nil
}

// generatePipeQuestExpr emits `left |?> f`: short-circuit bind for Result/Option.
func (g *Generator) generatePipeQuestExpr(n *parser.PipeQuestExpr) (value.Value, error) {
	leftVal, err := g.generateExpr(n.Left)
	if err != nil {
		return nil, err
	}

	leftKind := pipeQuestEnumKind(g.check.NodeTypes[n.Left])
	enumName := "Result"
	if leftKind == "Option" {
		enumName = "Option"
	}
	ei := g.enums[enumName]

	optPtr := leftVal
	if _, ok := leftVal.Type().(*types.PointerType); !ok {
		alloc := g.newAlloca(leftVal.Type())
		g.current.NewStore(leftVal, alloc)
		optPtr = alloc
	}

	tagPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := g.current.NewLoad(types.I64, tagPtr)
	isOk := g.current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, 0))

	fn := g.current.Parent
	okBlock := g.newBlock("pq_ok", fn)
	failBlock := g.newBlock("pq_fail", fn)
	mergeBlock := g.newBlock("pq_merge", fn)
	g.current.NewCondBr(isOk, okBlock, failBlock)

	// Success path: unwrap payload and call the piped function.
	g.current = okBlock
	var innerType types.Type = types.I64
	if st, ok := g.check.NodeTypes[n.Left].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		innerType = g.mapTypeToLLVM(st.Params[0])
	}
	payloadPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(innerType))
	payload := g.current.NewLoad(innerType, castPtr)

	callVal, callType, err := g.generatePipeQuestCall(n, payload)
	if err != nil {
		return nil, err
	}
	okResult, err := g.normalizeToResult(callVal, callType)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	okBlockOut := g.current

	// Failure path: propagate Err or convert None.
	g.current = failBlock
	var failResult value.Value
	if leftKind == "Result" {
		failResult = leftVal
	} else {
		failResult, err = g.emitUnexpectedNoneAsResultErr()
		if err != nil {
			return nil, err
		}
	}
	g.current.NewBr(mergeBlock)
	failBlockOut := g.current

	g.current = mergeBlock
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(okResult, okBlockOut),
		ir.NewIncoming(failResult, failBlockOut),
	)
	return phi, nil
}

func pipeQuestEnumKind(t checker.Type) string {
	if st, ok := t.(*checker.SpecializedType); ok {
		if et, ok := st.Base.(*checker.EnumType); ok {
			if et.Name == "Result" || et.Name == "Option" {
				return et.Name
			}
		}
	}
	return "Result"
}

func (g *Generator) generatePipeQuestCall(n *parser.PipeQuestExpr, payload value.Value) (value.Value, checker.Type, error) {
	var callee parser.Node
	var extraArgs []parser.Node
	if rc, ok := n.Right.(*parser.CallExpr); ok {
		callee = rc.Callee
		extraArgs = rc.Args
	} else {
		callee = n.Right
	}

	var calleeVal value.Value
	if id, ok := callee.(*parser.Identifier); ok {
		if f := g.findFunc(id.Name); f != nil {
			calleeVal = f
		}
	}
	if calleeVal == nil {
		var err error
		calleeVal, err = g.generateExpr(callee)
		if err != nil {
			return nil, checker.Unknown, err
		}
	}

	args := []value.Value{payload}
	for _, arg := range extraArgs {
		v, err := g.generateExpr(arg)
		if err != nil {
			return nil, checker.Unknown, err
		}
		args = append(args, v)
	}

	var retType checker.Type = checker.Unknown
	if ft, ok := g.check.NodeTypes[callee].(*checker.FuncType); ok {
		retType = ft.Return
	}
	if sp, ok := g.check.Specializations[n]; ok {
		retType = sp.Return
	}
	retLLVMType := types.Type(types.I64)
	if retType != checker.Unknown {
		retLLVMType = g.mapTypeToLLVM(retType)
	}

	var retVal value.Value
	if ptrType, ok := calleeVal.Type().(*types.PointerType); ok {
		if _, isFuncType := ptrType.ElemType.(*types.FuncType); isFuncType {
			retVal = g.current.NewCall(calleeVal, args...)
			return retVal, retType, nil
		}
	}
	retVal = g.callClosureDirect(calleeVal, retLLVMType, args)
	return retVal, retType, nil
}

func (g *Generator) normalizeToResult(val value.Value, t checker.Type) (value.Value, error) {
	kind := pipeQuestEnumKind(t)
	if kind == "Result" {
		return val, nil
	}
	if kind == "Option" {
		ei := g.enums["Option"]
		optPtr := val
		if _, ok := val.Type().(*types.PointerType); !ok {
			alloc := g.newAlloca(val.Type())
			g.current.NewStore(val, alloc)
			optPtr = alloc
		}
		tagPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
		tag := g.current.NewLoad(types.I64, tagPtr)
		isSome := g.current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, 0))

		fn := g.current.Parent
		someBlock := g.newBlock("pq_opt_some", fn)
		noneBlock := g.newBlock("pq_opt_none", fn)
		mergeBlock := g.newBlock("pq_opt_merge", fn)
		g.current.NewCondBr(isSome, someBlock, noneBlock)

		g.current = someBlock
		var innerType types.Type = types.I64
		if st, ok := t.(*checker.SpecializedType); ok && len(st.Params) > 0 {
			innerType = g.mapTypeToLLVM(st.Params[0])
		}
		payloadPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(innerType))
		inner := g.current.NewLoad(innerType, castPtr)
		okVal, err := g.emitOptionResultAlloc("Result", 0, inner)
		if err != nil {
			return nil, err
		}
		g.current.NewBr(mergeBlock)
		someOut := g.current

		g.current = noneBlock
		errVal, err := g.emitUnexpectedNoneAsResultErr()
		if err != nil {
			return nil, err
		}
		g.current.NewBr(mergeBlock)
		noneOut := g.current

		g.current = mergeBlock
		phi := mergeBlock.NewPhi(
			ir.NewIncoming(okVal, someOut),
			ir.NewIncoming(errVal, noneOut),
		)
		return phi, nil
	}
	return g.emitOptionResultAlloc("Result", 0, val)
}

func (g *Generator) emitUnexpectedNoneAsResultErr() (value.Value, error) {
	if fn := g.findFunc("noneError"); fn != nil {
		msgLit := &parser.StringLiteral{Value: "unexpected None"}
		msgVal, err := g.generateExpr(msgLit)
		if err != nil {
			return nil, err
		}
		errIface, err := g.generateCallValue(fn, []value.Value{msgVal})
		if err != nil {
			return nil, err
		}
		return g.emitOptionResultAlloc("Result", 1, errIface)
	}
	return g.emitOptionResultAlloc("Result", 1, nil)
}

func (g *Generator) generateCallValue(callee value.Value, args []value.Value) (value.Value, error) {
	return g.current.NewCall(callee, args...), nil
}

// generateElvisExpr emits `x ?: default`: if x is Some(v) return v, else return default.
func (g *Generator) generateElvisExpr(n *parser.ElvisExpr) (value.Value, error) {
	optVal, err := g.generateExpr(n.Left)
	if err != nil {
		return nil, err
	}

	// Determine the inner payload LLVM type from the Option[T] on the left.
	var payloadType types.Type = types.I64
	if st, ok := g.check.NodeTypes[n.Left].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		payloadType = g.mapTypeToLLVM(st.Params[0])
	} else if bt, ok := g.check.NodeTypes[n].(*checker.BasicType); ok {
		payloadType = g.mapTypeToLLVM(bt)
	}

	ei := g.enums["Option"]

	// Ensure optVal is stored at a pointer so we can GEP into it.
	var optPtr value.Value
	if _, ok := optVal.Type().(*types.PointerType); ok {
		optPtr = optVal
	} else {
		alloc := g.newAlloca(optVal.Type())
		g.current.NewStore(optVal, alloc)
		optPtr = alloc
	}

	tagPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := g.current.NewLoad(types.I64, tagPtr)
	isSome := g.current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, 0))

	fn := g.current.Parent
	someBlock := g.newBlock("elvis_some", fn)
	noneBlock := g.newBlock("elvis_none", fn)
	mergeBlock := g.newBlock("elvis_merge", fn)
	g.current.NewCondBr(isSome, someBlock, noneBlock)

	// Some branch: extract payload.
	g.current = someBlock
	payloadPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(payloadType))
	someVal := g.current.NewLoad(payloadType, castPtr)
	g.current.NewBr(mergeBlock)
	someBlock = g.current

	// None branch: evaluate default.
	g.current = noneBlock
	defaultVal, err := g.generateExpr(n.Right)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	noneBlock = g.current

	g.current = mergeBlock
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(someVal, someBlock),
		ir.NewIncoming(defaultVal, noneBlock),
	)
	return phi, nil
}

// generateSafeNavExpr emits `obj?.field`: short-circuit member access on Option[T].
func (g *Generator) generateSafeNavExpr(n *parser.SafeNavExpr) (value.Value, error) {
	return g.generateSafeNavAccess(n, func(unwrapped value.Value, innerCheckerType checker.Type) (value.Value, error) {
		tmpName := "__safenav_unwrap"
		tmpAlloc := g.newAlloca(unwrapped.Type())
		g.current.NewStore(unwrapped, tmpAlloc)
		oldVar, hadOld := g.vars[tmpName]
		g.vars[tmpName] = tmpAlloc
		defer func() {
			if hadOld {
				g.vars[tmpName] = oldVar
			} else {
				delete(g.vars, tmpName)
			}
		}()

		g.check.NodeTypes[&parser.Identifier{Name: tmpName}] = innerCheckerType
		me := &parser.MemberExpr{
			Object:   &parser.Identifier{Name: tmpName},
			Property: n.Property,
		}
		return g.generateMemberExpr(me)
	})
}

func (g *Generator) generateSafeNavMethodCall(sn *parser.SafeNavExpr, n *parser.CallExpr) (value.Value, error) {
	return g.generateSafeNavAccess(sn, func(unwrapped value.Value, innerCheckerType checker.Type) (value.Value, error) {
		tmpName := "__safenav_unwrap"
		tmpAlloc := g.newAlloca(unwrapped.Type())
		g.current.NewStore(unwrapped, tmpAlloc)
		oldVar, hadOld := g.vars[tmpName]
		g.vars[tmpName] = tmpAlloc
		defer func() {
			if hadOld {
				g.vars[tmpName] = oldVar
			} else {
				delete(g.vars, tmpName)
			}
		}()

		g.check.NodeTypes[&parser.Identifier{Name: tmpName}] = innerCheckerType
		me := &parser.MemberExpr{
			Object:   &parser.Identifier{Name: tmpName},
			Property: sn.Property,
		}
		return g.generateMethodCall(me, n)
	})
}

// generateSafeNavAccess branches on Option tag; on Some runs accessFn on the unwrapped payload
// and wraps the result in Some; on None returns None without evaluating accessFn.
func (g *Generator) generateSafeNavAccess(n *parser.SafeNavExpr, accessFn func(unwrapped value.Value, innerCheckerType checker.Type) (value.Value, error)) (value.Value, error) {
	optVal, err := g.generateExpr(n.Object)
	if err != nil {
		return nil, err
	}

	ei := g.enums["Option"]
	optPtr := optVal
	if _, ok := optVal.Type().(*types.PointerType); !ok {
		alloc := g.newAlloca(optVal.Type())
		g.current.NewStore(optVal, alloc)
		optPtr = alloc
	}

	tagPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	tag := g.current.NewLoad(types.I64, tagPtr)
	isSome := g.current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, 0))

	fn := g.current.Parent
	someBlock := g.newBlock("safenav_some", fn)
	noneBlock := g.newBlock("safenav_none", fn)
	mergeBlock := g.newBlock("safenav_merge", fn)
	g.current.NewCondBr(isSome, someBlock, noneBlock)

	var innerCheckerType checker.Type = checker.IntType
	if st, ok := g.check.NodeTypes[n.Object].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		innerCheckerType = st.Params[0]
	}
	innerLLVMType := g.mapTypeToLLVM(innerCheckerType)

	// None branch: return None without evaluating member access.
	g.current = noneBlock
	noneVal, err := g.generateNoneLiteral(&parser.NoneLiteral{})
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	noneBlockOut := g.current

	// Some branch: unwrap payload, access member/method, wrap in Some.
	g.current = someBlock
	payloadPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(innerLLVMType))
	payload := g.current.NewLoad(innerLLVMType, castPtr)

	memberVal, err := accessFn(payload, innerCheckerType)
	if err != nil {
		return nil, err
	}
	someVal, err := g.emitOptionResultAlloc("Option", 0, memberVal)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	someBlockOut := g.current

	g.current = mergeBlock
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(someVal, someBlockOut),
		ir.NewIncoming(noneVal, noneBlockOut),
	)
	return phi, nil
}

func (g *Generator) generateSomeExpr(n *parser.SomeExpr) (value.Value, error) {
	val, err := g.generateExpr(n.Value)
	if err != nil {
		return nil, err
	}
	return g.emitOptionResultAlloc("Option", 0, val)
}

func (g *Generator) generateNoneLiteral(n *parser.NoneLiteral) (value.Value, error) {
	return g.emitOptionResultAlloc("Option", 1, nil)
}

func (g *Generator) generateOkExpr(n *parser.OkExpr) (value.Value, error) {
	val, err := g.generateExpr(n.Value)
	if err != nil {
		return nil, err
	}
	return g.emitOptionResultAlloc("Result", 0, val)
}

func (g *Generator) generateErrExpr(n *parser.ErrExpr) (value.Value, error) {
	val, err := g.generateExpr(n.Value)
	if err != nil {
		return nil, err
	}
	// The Err payload must be an Error interface fat pointer {obj_ptr, vtable_ptr}
	// so that dynamic dispatch (e.message()) works when the match extracts it.
	wrapped, err := g.coerceClassToInterface(val, "Error")
	if err != nil {
		return nil, err
	}
	return g.emitOptionResultAlloc("Result", 1, wrapped)
}

func (g *Generator) emitOptionResultAlloc(typeName string, tag int, payload value.Value) (value.Value, error) {
	return g.emitOptionResultAllocInner(typeName, tag, payload, false)
}

func (g *Generator) emitOptionResultAllocNoRetain(typeName string, tag int, payload value.Value) (value.Value, error) {
	return g.emitOptionResultAllocInner(typeName, tag, payload, true)
}

func (g *Generator) emitOptionResultAllocInner(typeName string, tag int, payload value.Value, noRetainPayload bool) (value.Value, error) {
	// Built-in enums use the standard 72-byte layout.
	// We ensure the type exists in the module.
	var enumTyp *types.StructType
	for _, t := range g.module.TypeDefs {
		if st, ok := t.(*types.StructType); ok && st.TypeName == typeName {
			enumTyp = st
			break
		}
	}
	if enumTyp == nil {
		typ := g.module.NewTypeDef(typeName, types.NewStruct(types.I64, types.NewArray(64, types.I8)))
		enumTyp = typ.(*types.StructType)
	}

	// We don't have a specialized dtor for built-in enums here yet,
	// but for M10 test purposes, we can proceed.
	raw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 72), constant.NewNull(types.I8Ptr), constant.NewNull(types.I8Ptr))
	structPtr := g.current.NewBitCast(raw, types.NewPointer(enumTyp))

	tagPtr := g.current.NewGetElementPtr(enumTyp, structPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(constant.NewInt(types.I64, int64(tag)), tagPtr)

	if payload != nil {
		if g.isHeapType(payload.Type()) && !noRetainPayload {
			g.emitRetain(payload)
		}
		payloadPtr := g.current.NewGetElementPtr(enumTyp, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(payload.Type()))
		g.current.NewStore(payload, castPtr)
	}
	return structPtr, nil
}

// listLoopSetup creates blocks and infrastructure for a loop over a List.
// Returns: (listTyped, size, iAlloc, condBlock, bodyBlock, incrBlock, afterBlock).
func (g *Generator) listLoopSetup(obj value.Value) (listTyped value.Value, size value.Value, iAlloc value.Value, cond, body, incr, after *ir.Block) {
	fn := g.current.Parent
	listTyped = g.current.NewBitCast(obj, types.NewPointer(g.structs["SoyuzList"].typ))
	sizePtr := g.current.NewGetElementPtr(g.structs["SoyuzList"].typ, listTyped,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	size = g.current.NewLoad(types.I64, sizePtr)
	iAlloc = g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), iAlloc)
	cond = g.newBlock("hof_cond", fn)
	body = g.newBlock("hof_body", fn)
	incr = g.newBlock("hof_incr", fn)
	after = g.newBlock("hof_after", fn)
	g.current.NewBr(cond)
	return
}

// listLoopGetElem generates a get of the element at index i (expects to be called inside bodyBlock).
func (g *Generator) listLoopGetElem(listTyped value.Value, elemLLVMType types.Type, iAlloc value.Value) value.Value {
	listAsI8 := g.current.NewBitCast(listTyped, types.I8Ptr)
	i := g.current.NewLoad(types.I64, iAlloc)
	raw := g.current.NewCall(g.findFunc("soyuz_list_get"), listAsI8, i)
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
	}
	return elem
}

// listLoopIncr advances the loop counter and jumps back to condBlock.
func (g *Generator) listLoopIncr(iAlloc value.Value, condBlock *ir.Block) {
	cur := g.current.NewLoad(types.I64, iAlloc)
	next := g.current.NewAdd(cur, constant.NewInt(types.I64, 1))
	g.current.NewStore(next, iAlloc)
	g.current.NewBr(condBlock)
}

func (g *Generator) generateListMap(n *parser.CallExpr, obj value.Value, st *checker.SpecializedType, args []value.Value) (value.Value, error) {
	elemCheckerType := st.Params[0]
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)

	// Determine result element type from checker specialization.
	var retCheckerType checker.Type = checker.Unknown
	if ft, ok := g.check.Specializations[n]; ok && ft != nil {
		if rst, ok2 := ft.Return.(*checker.SpecializedType); ok2 && len(rst.Params) > 0 {
			retCheckerType = rst.Params[0]
		}
	}
	retLLVMType := g.mapTypeToLLVM(retCheckerType)

	closureVal := args[0]

	// Create result list.
	dtorName := "soyuz_list_dtor_primitive"
	if g.isHeapType(retLLVMType) {
		dtorName = "soyuz_list_dtor_rc"
	}
	dtor := g.findFunc(dtorName)
	resultRaw := g.current.NewCall(g.findFunc("soyuz_list_new"),
		constant.NewInt(types.I64, 0),
		g.current.NewBitCast(dtor, types.I8Ptr))

	listTyped, size, iAlloc, condBlock, bodyBlock, incrBlock, afterBlock := g.listLoopSetup(obj)

	// Condition block.
	g.current = condBlock
	i := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, i, size)
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	// Body block: get element, call closure, append to result.
	g.current = bodyBlock
	elem := g.listLoopGetElem(listTyped, elemLLVMType, iAlloc)
	mappedVal := g.callClosureDirect(closureVal, retLLVMType, []value.Value{elem})
	if g.isHeapType(retLLVMType) {
		g.emitRetain(mappedVal)
	}
	valCast := g.castToI8Ptr(mappedVal)
	resultAsI8 := g.current.NewBitCast(resultRaw, types.I8Ptr)
	g.current.NewCall(g.findFunc("soyuz_list_append"), resultAsI8, valCast)
	g.current.NewBr(incrBlock)

	// Incr block.
	g.current = incrBlock
	g.listLoopIncr(iAlloc, condBlock)

	g.current = afterBlock

	// Cast result to the correct SpecializedType pointer.
	if ft, ok := g.check.Specializations[n]; ok && ft != nil {
		retListLLVMType := g.mapTypeToLLVM(ft.Return)
		return g.current.NewBitCast(resultRaw, retListLLVMType), nil
	}
	return resultRaw, nil
}

func (g *Generator) generateListFilter(n *parser.CallExpr, obj value.Value, st *checker.SpecializedType, args []value.Value) (value.Value, error) {
	elemCheckerType := st.Params[0]
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)
	closureVal := args[0]
	fn := g.current.Parent

	dtorName := "soyuz_list_dtor_primitive"
	if g.isHeapType(elemLLVMType) {
		dtorName = "soyuz_list_dtor_rc"
	}
	dtor := g.findFunc(dtorName)
	resultRaw := g.current.NewCall(g.findFunc("soyuz_list_new"),
		constant.NewInt(types.I64, 0),
		g.current.NewBitCast(dtor, types.I8Ptr))

	listTyped, size, iAlloc, condBlock, bodyBlock, incrBlock, afterBlock := g.listLoopSetup(obj)

	appendBlock := g.newBlock("hof_filter_append", fn)

	// Condition block.
	g.current = condBlock
	i := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, i, size)
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	// Body block: get element, call predicate.
	g.current = bodyBlock
	elem := g.listLoopGetElem(listTyped, elemLLVMType, iAlloc)
	predResult := g.callClosureDirect(closureVal, types.I1, []value.Value{elem})
	g.current.NewCondBr(predResult, appendBlock, incrBlock)

	// Append block.
	g.current = appendBlock
	if g.isHeapType(elemLLVMType) {
		g.emitRetain(elem)
	}
	valCast := g.castToI8Ptr(elem)
	resultAsI8 := g.current.NewBitCast(resultRaw, types.I8Ptr)
	g.current.NewCall(g.findFunc("soyuz_list_append"), resultAsI8, valCast)
	g.current.NewBr(incrBlock)

	// Incr block.
	g.current = incrBlock
	g.listLoopIncr(iAlloc, condBlock)

	g.current = afterBlock

	retListLLVMType := g.mapTypeToLLVM(st)
	return g.current.NewBitCast(resultRaw, retListLLVMType), nil
}

func (g *Generator) generateListReduce(n *parser.CallExpr, obj value.Value, st *checker.SpecializedType, args []value.Value) (value.Value, error) {
	elemCheckerType := st.Params[0]
	elemLLVMType := g.mapTypeToLLVM(elemCheckerType)
	closureVal := args[0]
	initVal := args[1]
	accType := initVal.Type()

	accAlloc := g.newAlloca(accType)
	g.current.NewStore(initVal, accAlloc)

	listTyped, size, iAlloc, condBlock, bodyBlock, incrBlock, afterBlock := g.listLoopSetup(obj)

	// Condition block.
	g.current = condBlock
	i := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, i, size)
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	// Body block: get element, call fn(acc, elem) -> new acc.
	g.current = bodyBlock
	elem := g.listLoopGetElem(listTyped, elemLLVMType, iAlloc)
	acc := g.current.NewLoad(accType, accAlloc)
	newAcc := g.callClosureDirect(closureVal, accType, []value.Value{acc, elem})
	g.current.NewStore(newAcc, accAlloc)
	g.current.NewBr(incrBlock)

	// Incr block.
	g.current = incrBlock
	g.listLoopIncr(iAlloc, condBlock)

	g.current = afterBlock
	return g.current.NewLoad(accType, accAlloc), nil
}

func (g *Generator) generateListJoin(obj value.Value, st *checker.SpecializedType, args []value.Value) (value.Value, error) {
	// Only valid for List[String]. Concatenates elements with a separator.
	elemLLVMType := g.soyuzStringPtrType
	sep := args[0]
	fn := g.current.Parent

	// acc = soyuz_str_new("", 0)
	emptyCS := constant.NewCharArrayFromString("\x00")
	emptyGlob := g.module.NewGlobalDef("", emptyCS)
	emptyGlob.Immutable = true
	emptyPtr := g.current.NewGetElementPtr(emptyCS.Type(), emptyGlob,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I64, 0))
	accVal := g.current.NewCall(g.findFunc("soyuz_str_new"), emptyPtr, constant.NewInt(types.I64, 0))
	accAlloc := g.newAlloca(g.soyuzStringPtrType)
	g.current.NewStore(accVal, accAlloc)

	listTyped, size, iAlloc, condBlock, bodyBlock, incrBlock, afterBlock := g.listLoopSetup(obj)

	prependSepBlock := g.newBlock("hof_join_sep", fn)
	concatElemBlock := g.newBlock("hof_join_elem", fn)

	// Condition block.
	g.current = condBlock
	i := g.current.NewLoad(types.I64, iAlloc)
	cond := g.current.NewICmp(enum.IPredSLT, i, size)
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	// Body block: get element, check if first.
	g.current = bodyBlock
	elem := g.listLoopGetElem(listTyped, elemLLVMType, iAlloc)
	iForSep := g.current.NewLoad(types.I64, iAlloc)
	isFirst := g.current.NewICmp(enum.IPredEQ, iForSep, constant.NewInt(types.I64, 0))
	g.current.NewCondBr(isFirst, concatElemBlock, prependSepBlock)

	// Add separator before non-first elements.
	g.current = prependSepBlock
	acc1 := g.current.NewLoad(g.soyuzStringPtrType, accAlloc)
	withSep := g.current.NewCall(g.findFunc("soyuz_str_concat"), acc1, sep)
	g.current.NewStore(withSep, accAlloc)
	g.current.NewBr(concatElemBlock)

	// Concat element.
	g.current = concatElemBlock
	acc2 := g.current.NewLoad(g.soyuzStringPtrType, accAlloc)
	withElem := g.current.NewCall(g.findFunc("soyuz_str_concat"), acc2, elem)
	g.current.NewStore(withElem, accAlloc)
	g.current.NewBr(incrBlock)

	// Incr block.
	g.current = incrBlock
	g.listLoopIncr(iAlloc, condBlock)

	g.current = afterBlock
	return g.current.NewLoad(g.soyuzStringPtrType, accAlloc), nil
}

func (g *Generator) generateMapKeys(obj value.Value, st *checker.SpecializedType) (value.Value, error) {
	keyLLVMType := g.mapTypeToLLVM(st.Params[0])
	keyIsHeap := int64(0)
	if g.isHeapType(keyLLVMType) {
		keyIsHeap = 1
	}
	objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
	raw := g.current.NewCall(g.findFunc("soyuz_map_keys"), objAsI8, constant.NewInt(types.I64, keyIsHeap))
	listPtrType := types.NewPointer(g.structs["SoyuzList"].typ)
	return g.current.NewBitCast(raw, listPtrType), nil
}

func (g *Generator) generateMapValues(obj value.Value, st *checker.SpecializedType) (value.Value, error) {
	valLLVMType := g.mapTypeToLLVM(st.Params[1])
	valIsHeap := int64(0)
	if g.isHeapType(valLLVMType) {
		valIsHeap = 1
	}
	objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
	raw := g.current.NewCall(g.findFunc("soyuz_map_values"), objAsI8, constant.NewInt(types.I64, valIsHeap))
	listPtrType := types.NewPointer(g.structs["SoyuzList"].typ)
	return g.current.NewBitCast(raw, listPtrType), nil
}

// emitIntToOption wraps a C int64 sentinel result (-1 = None, ≥0 = Some) into Option[Int|Char].
// When asChar is true the payload is truncated to i32 (Char).
func (g *Generator) emitIntToOption(raw value.Value, asChar bool) (value.Value, error) {
	ei := g.enums["Option"]
	someTag := ei.variants["Some"].tag
	noneTag := ei.variants["None"].tag

	fn := g.current.Parent
	someBlock := g.newBlock("opt_some", fn)
	noneBlock := g.newBlock("opt_none", fn)
	mergeBlock := g.newBlock("opt_merge", fn)

	cmp := g.current.NewICmp(enum.IPredSGE, raw, constant.NewInt(types.I64, 0))
	g.current.NewCondBr(cmp, someBlock, noneBlock)

	g.current = someBlock
	var payload value.Value = raw
	if asChar {
		payload = g.current.NewTrunc(raw, types.I32)
	}
	someOpt, err := g.emitOptionResultAlloc("Option", someTag, payload)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	someBlockOut := g.current

	g.current = noneBlock
	noneOpt, err := g.emitOptionResultAlloc("Option", noneTag, nil)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	noneBlockOut := g.current

	g.current = mergeBlock
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(someOpt, someBlockOut),
		ir.NewIncoming(noneOpt, noneBlockOut),
	)
	return phi, nil
}

func stringMethodReturnsBool(method string) bool {
	switch method {
	case "isEmpty", "contains", "startsWith", "endsWith":
		return true
	default:
		return false
	}
}

func primitiveMethodCFunc(typeName, method string) string {
	switch typeName {
	case "String":
		switch method {
		case "len":
			return "soyuz_str_len"
		case "isEmpty":
			return "soyuz_str_is_empty"
		case "trim":
			return "soyuz_str_trim"
		case "toUpperCase", "toUpper":
			return "soyuz_str_to_upper"
		case "toLowerCase", "toLower":
			return "soyuz_str_to_lower"
		case "contains":
			return "soyuz_str_contains"
		case "startsWith":
			return "soyuz_str_starts_with"
		case "endsWith":
			return "soyuz_str_ends_with"
		case "indexOf":
			return "soyuz_str_index_of"
		case "lastIndexOf":
			return "soyuz_str_last_index_of"
		case "replace":
			return "soyuz_str_replace"
		case "substring":
			return "soyuz_str_substring"
		case "split":
			return "soyuz_str_split"
		}
	case "Int":
		switch method {
		case "toString":
			return "soyuz_int_to_str"
		case "abs":
			return "soyuz_int_abs"
		case "toFloat":
			return "soyuz_int_to_float"
		}
	}
	return ""
}

// generateTaskExpr emits code for `task callExpr(args)` or `task (pipeExpr)`.
// For a direct call, it packs the args into a heap-allocated i64 array, generates a
// wrapper function that unpacks and calls the original function, and calls srt_enqueue.
// For a pipe chain, it captures free variables and generates a wrapper that evaluates
// the full chain inside the worker thread.
func (g *Generator) generateTaskExpr(n *parser.TaskExpr) (value.Value, error) {
	call, ok := n.Inner.(*parser.CallExpr)
	if !ok {
		// M-15: pipe chain or other expression — generate a closure-style wrapper.
		return g.generateTaskPipeExpr(n)
	}

	// Evaluate args in current context.
	args, err := g.generateCallArgs(call.Args)
	if err != nil {
		return nil, err
	}

	// Find the LLVM function to call inside the wrapper.
	var targetFunc *ir.Func
	if id, ok2 := call.Callee.(*parser.Identifier); ok2 {
		targetFunc = g.findFunc(id.Name)
		// Try specialized generic variant.
		if targetFunc == nil {
			if st, ok3 := g.check.Specializations[call]; ok3 {
				mangled := id.Name
				for _, p := range st.Params {
					mangled += "__" + p.String()
				}
				targetFunc = g.specialized[mangled]
			}
		}
	}
	if targetFunc == nil {
		return nil, fmt.Errorf("task: não foi possível resolver função para enfileirar")
	}

	// Pack args into a heap-allocated [N x i64] array.
	// All arg types are normalized to i64 for uniform slot size.
	numArgs := len(args)
	var argsHeap value.Value
	if numArgs > 0 {
		argsHeap = g.current.NewCall(g.findBuiltin("malloc"),
			constant.NewInt(types.I64, int64(numArgs*8)))
		arrType := types.NewArray(uint64(numArgs), types.I64)
		argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
		for i, a := range args {
			slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
			g.current.NewStore(g.castToI64(a), slotPtr)
		}
	} else {
		argsHeap = constant.NewNull(types.I8Ptr)
	}

	// Build the original arg LLVM types (needed by the wrapper).
	origArgTypes := make([]types.Type, len(args))
	for i, a := range args {
		origArgTypes[i] = a.Type()
	}

	// Generate the wrapper function (saves/restores codegen state).
	wrapperFn := g.generateTaskWrapperFunc(targetFunc, origArgTypes, numArgs)

	// Call srt_enqueue in the current context.
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	handle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
	return handle, nil
}

// generateTaskWrapperFunc emits a `void @__task_wrapper_N(i8* raw_args)` function that:
// unpacks i64 slots from the args buffer, calls targetFunc, stores the result via
// srt_set_task_result, and frees the buffer.
func (g *Generator) generateTaskWrapperFunc(
	targetFunc *ir.Func,
	origArgTypes []types.Type,
	numArgs int,
) *ir.Func {
	name := fmt.Sprintf("__task_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++

	wrapperFn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", wrapperFn)

	rawArgs := wrapperFn.Params[0]

	// Unpack args from i64 array.
	var callArgs []value.Value
	if numArgs > 0 {
		arrType := types.NewArray(uint64(numArgs), types.I64)
		argsPtr := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))
		for i, origType := range origArgTypes {
			slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
			slot := g.current.NewLoad(types.I64, slotPtr)
			callArgs = append(callArgs, g.i64ToType(slot, origType))
		}
		// Free the args buffer before calling the function (no further use after unpack).
		g.current.NewCall(g.findBuiltin("free"), rawArgs)
	}

	// Call the original function.
	callInst := g.current.NewCall(targetFunc, callArgs...)

	// Store result via srt_set_task_result.
	retType := targetFunc.Sig.RetType
	var resultI8 value.Value
	if retType == nil || retType.Equal(types.Void) {
		resultI8 = constant.NewNull(types.I8Ptr)
	} else {
		resultI8 = g.castToI8Ptr(callInst)
	}
	g.current.NewCall(g.findFunc("srt_set_task_result"), resultI8)

	g.current.NewRet(nil)

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return wrapperFn
}

// ── M-16 / M-17: ~> and ~?> async pipe ──────────────────────────────────────

// generateAsyncPipeExpr emits code for `a ~> f ~> g` and `a ~> f ~?> g ~> h`.
//
// Architecture: the ENTIRE chain runs inside a single outer task wrapper.
// The initial value is captured from the calling context and packed as an arg.
// Inside the wrapper:
//   - Each intermediate step is spawned as its own task and immediately awaited.
//   - For ~?> steps: the enum tag is inspected; if Err/None (tag ≠ 0), the wrapper
//     stores the error result and returns early (M-17 short-circuit).
//   - After all steps, the final result is stored via srt_set_task_result.
//
// The outer context enqueues the chain wrapper and returns its handle as Task[T].
func (g *Generator) generateAsyncPipeExpr(n *parser.AsyncPipeExpr) (value.Value, error) {
	if len(n.Steps) < 2 {
		return nil, fmt.Errorf("~>: ao menos um step é necessário")
	}

	// Evaluate the initial value in the current context.
	initVal, err := g.generateExpr(n.Steps[0])
	if err != nil {
		return nil, err
	}

	// Pack the initial value as a single-element args buffer.
	initI64 := g.castToI64(initVal)
	argsHeap := g.current.NewCall(g.findBuiltin("malloc"),
		constant.NewInt(types.I64, 8))
	arrType := types.NewArray(uint64(1), types.I64)
	argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
	slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(initI64, slotPtr)

	// Generate the chain wrapper function.
	chainFn, ferr := g.generateAsyncChainWrapper(n.Steps, initVal.Type())
	if ferr != nil {
		return nil, ferr
	}

	// Enqueue the chain wrapper.
	chainPtr := g.current.NewBitCast(chainFn, types.I8Ptr)
	handle := g.current.NewCall(g.findFunc("srt_enqueue"), chainPtr, argsHeap)
	return handle, nil
}

// generateAsyncChainWrapper creates a `void @__async_chain_N(i8* raw_args)` function that:
//  1. Unpacks the initial value from raw_args.
//  2. For each step (in order), spawns a task and awaits the result.
//  3. For ~?> steps, checks the enum tag; if Err/None → stores result + returns early.
//  4. Stores the final result via srt_set_task_result.
func (g *Generator) generateAsyncChainWrapper(steps []parser.Node, initType types.Type) (*ir.Func, error) {
	name := fmt.Sprintf("__async_chain_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++
	fn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", fn)

	// Unpack initial value from args buffer.
	rawArgs := fn.Params[0]
	arrType := types.NewArray(uint64(1), types.I64)
	argsBuf := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))
	slot := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	initI64 := g.current.NewLoad(types.I64, slot)
	current := g.i64ToType(initI64, initType) // value.Value

	// Process each step.
	enqueue := g.findFunc("srt_enqueue")
	await := g.findFunc("srt_await")

	// Track the return type of the previous step so ~?> can inspect the right struct layout.
	prevReturnType := initType

	for i, rawStep := range steps[1:] {
		isQuestStep := false
		step := rawStep
		if qs, ok := rawStep.(*parser.AsyncPipeQuestStep); ok {
			isQuestStep = true
			step = qs.Step
		}

		// Resolve the target function (plain ident or partial call).
		var targetFn *ir.Func

		if rc, ok := step.(*parser.CallExpr); ok {
			if id, ok2 := rc.Callee.(*parser.Identifier); ok2 {
				targetFn = g.findFunc(id.Name)
			}
			// Extra args: evaluate them (they reference outer-scope vars — not available here).
			// For now, only plain function idents are supported.
		} else if id, ok := step.(*parser.Identifier); ok {
			targetFn = g.findFunc(id.Name)
		}
		if targetFn == nil {
			// Restore state before returning error.
			g.current = oldCurrent
			g.vars = oldVars
			g.heapVars = oldHeapVars
			g.scopeStack = oldScopeStack
			g.taskVarStack = oldTaskVarStack
			g.syncGuardStack = oldSyncGuardStack
			g.arcVarStack = oldArcVarStack
			g.blockNames = oldBlockNames
			g.currentReturnType = oldReturnType
			return nil, fmt.Errorf("~>: step %d: não foi possível resolver função '%v'", i+1, step)
		}

		// M-17: ~?> short-circuit — inspect enum tag on the PREVIOUS step's result BEFORE
		// calling this step. If the previous result is Err/None, propagate and return early.
		if isQuestStep {
			if retPtr, ok2 := prevReturnType.(*types.PointerType); ok2 {
				if retST, ok3 := retPtr.ElemType.(*types.StructType); ok3 && len(retST.Fields) >= 2 {
					// current is i8* (from previous srt_await); cast to *Result/*Option.
					typedPtr := g.current.NewBitCast(current, retPtr)
					tagPtr := g.current.NewGetElementPtr(retST, typedPtr,
						constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
					tag := g.current.NewLoad(types.I64, tagPtr)
					isErr := g.current.NewICmp(enum.IPredNE, tag, constant.NewInt(types.I64, 0))

					errBlock := g.newBlock("chain_err", fn)
					contBlock := g.newBlock("chain_ok", fn)
					g.current.NewCondBr(isErr, errBlock, contBlock)

					// Error path: propagate the Result/Option and return early.
					g.current = errBlock
					g.current.NewCall(g.findFunc("srt_set_task_result"), current)
					g.current.NewRet(nil)

					// Ok path: unwrap the payload (field 1) and use it as input to this step.
					g.current = contBlock
					payloadPtr := g.current.NewGetElementPtr(retST, typedPtr,
						constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
					i64Ptr := g.current.NewBitCast(payloadPtr, types.NewPointer(types.I64))
					innerI64 := g.current.NewLoad(types.I64, i64Ptr)
					current = innerI64 // unwrapped value (i64)
				}
			}
		}

		var stepArgs []value.Value
		stepArgs = append(stepArgs, current)

		// Pack stepArgs into a buffer.
		numArgs := len(stepArgs)
		var stepArgsHeap value.Value
		if numArgs > 0 {
			stepArgsHeap = g.current.NewCall(g.findBuiltin("malloc"),
				constant.NewInt(types.I64, int64(numArgs*8)))
			sArrType := types.NewArray(uint64(numArgs), types.I64)
			sArgsPtr := g.current.NewBitCast(stepArgsHeap, types.NewPointer(sArrType))
			for si, a := range stepArgs {
				sp := g.current.NewGetElementPtr(sArrType, sArgsPtr,
					constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(si)))
				g.current.NewStore(g.castToI64(a), sp)
			}
		} else {
			stepArgsHeap = constant.NewNull(types.I8Ptr)
		}

		// origTypes must match the TARGET function's parameter types, not the
		// intermediate value's type (which is i8* from srt_await). The buffer
		// always holds i64 (via castToI64), and the wrapper unpacks using origTypes.
		origTypes := make([]types.Type, len(stepArgs))
		for si := range stepArgs {
			if si < len(targetFn.Params) {
				origTypes[si] = targetFn.Params[si].Type()
			} else {
				origTypes[si] = stepArgs[si].Type()
			}
		}
		wrapperFn := g.generateTaskWrapperFunc(targetFn, origTypes, numArgs)
		wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
		stepHandle := g.current.NewCall(enqueue, wrapperPtr, stepArgsHeap)

		// Await the step result (even for the last step — see below).
		rawResult := g.current.NewCall(await, stepHandle) // i8*

		// Advance tracking for the next iteration's ~?> check.
		prevReturnType = targetFn.Sig.RetType
		current = rawResult
	}

	// Store the final result.
	var finalI8 value.Value
	if current == nil {
		finalI8 = constant.NewNull(types.I8Ptr)
	} else if current.Type().Equal(types.I8Ptr) {
		finalI8 = current
	} else {
		finalI8 = g.castToI8Ptr(current)
	}
	g.current.NewCall(g.findFunc("srt_set_task_result"), finalI8)
	if g.current.Term == nil {
		g.current.NewRet(nil)
	}

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return fn, nil
}

// ── M-15: task (pipe chain) support ─────────────────────────────────────────

// generateTaskPipeExpr emits code for `task (pipeExpr)` or any task whose inner
// expression is not a bare CallExpr (e.g. a PipeExpr chain).
// It identifies free variables used in the pipe chain, packs them into a heap
// buffer as i64 slots, generates a wrapper function that unpacks and evaluates
// the full chain, and enqueues the wrapper via srt_enqueue.
func (g *Generator) generateTaskPipeExpr(n *parser.TaskExpr) (value.Value, error) {
	// 1. Collect free variable names (local vars referenced inside the chain).
	capturedNames := g.collectTaskCaptures(n.Inner)

	// 2. Evaluate each captured variable in the current context.
	capVals := make([]value.Value, 0, len(capturedNames))
	filtered := capturedNames[:0]
	for _, name := range capturedNames {
		alloc, ok := g.vars[name]
		if !ok {
			continue
		}
		ptr, ok2 := alloc.Type().(*types.PointerType)
		if !ok2 {
			continue
		}
		val := g.current.NewLoad(ptr.ElemType, alloc)
		capVals = append(capVals, val)
		filtered = append(filtered, name)
	}
	capturedNames = filtered

	// 3. Pack captured values into a heap-allocated [N x i64] args buffer.
	numCaps := len(capturedNames)
	var argsHeap value.Value
	if numCaps > 0 {
		argsHeap = g.current.NewCall(g.findBuiltin("malloc"),
			constant.NewInt(types.I64, int64(numCaps*8)))
		arrType := types.NewArray(uint64(numCaps), types.I64)
		argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
		for i, v := range capVals {
			slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
			g.current.NewStore(g.castToI64(v), slotPtr)
		}
	} else {
		argsHeap = constant.NewNull(types.I8Ptr)
	}

	// 4. Generate the wrapper function.
	wrapperFn := g.generateTaskPipeWrapperFunc(n.Inner, capturedNames, capVals)

	// 5. Enqueue.
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	handle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
	return handle, nil
}

// collectTaskCaptures walks the AST of a task inner expression and returns all
// local variable names (i.e. names present in g.vars) that are referenced.
// Function names resolved at module level are NOT in g.vars, so they are ignored.
// The returned slice is sorted for deterministic codegen.
func (g *Generator) collectTaskCaptures(node parser.Node) []string {
	seen := make(map[string]bool)
	var walk func(parser.Node)
	walk = func(n parser.Node) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *parser.Identifier:
			if _, inVars := g.vars[v.Name]; inVars {
				seen[v.Name] = true
			}
		case *parser.PipeExpr:
			walk(v.Left)
			// Right is the pipe step — a plain function ident doesn't need capture;
			// if it's a partial call, collect any extra args.
			if call, ok := v.Right.(*parser.CallExpr); ok {
				for _, arg := range call.Args {
					walk(arg)
				}
			}
			// plain ident (function ref) needs no capture
		case *parser.PipeQuestExpr:
			walk(v.Left)
			if call, ok := v.Right.(*parser.CallExpr); ok {
				for _, arg := range call.Args {
					walk(arg)
				}
			}
		case *parser.CallExpr:
			for _, arg := range v.Args {
				walk(arg)
			}
		case *parser.MemberExpr:
			walk(v.Object)
		case *parser.BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *parser.UnaryExpr:
			walk(v.Operand)
		}
	}
	walk(node)
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// generateTaskPipeWrapperFunc emits a `void @__task_wrapper_N(i8* raw_args)` function
// that unpacks captured local variables, evaluates the full pipe chain expression,
// and stores the result via srt_set_task_result.
func (g *Generator) generateTaskPipeWrapperFunc(
	inner parser.Node,
	capturedNames []string,
	capturedVals []value.Value,
) *ir.Func {
	name := fmt.Sprintf("__task_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++
	wrapperFn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", wrapperFn)

	rawArgs := wrapperFn.Params[0]

	// Unpack captured variables from the i64 args buffer.
	if len(capturedNames) > 0 {
		arrType := types.NewArray(uint64(len(capturedNames)), types.I64)
		argsPtr := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))
		for i, capName := range capturedNames {
			slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
			slot := g.current.NewLoad(types.I64, slotPtr)
			restored := g.i64ToType(slot, capturedVals[i].Type())
			alloc := g.newAlloca(restored.Type())
			g.current.NewStore(restored, alloc)
			g.vars[capName] = alloc
		}
		g.current.NewCall(g.findBuiltin("free"), rawArgs)
	}

	// Evaluate the pipe chain expression inside the worker thread.
	result, err := g.generateExpr(inner)

	var resultI8 value.Value
	if err != nil || result == nil {
		resultI8 = constant.NewNull(types.I8Ptr)
	} else {
		resultI8 = g.castToI8Ptr(result)
	}
	g.current.NewCall(g.findFunc("srt_set_task_result"), resultI8)
	if g.current.Term == nil {
		g.current.NewRet(nil)
	}

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return wrapperFn
}

// castToI64 converts any LLVM value to i64 for uniform storage in task arg buffers.
func (g *Generator) castToI64(v value.Value) value.Value {
	t := v.Type()
	if t.Equal(types.I64) {
		return v
	}
	if t.Equal(types.I1) {
		return g.current.NewZExt(v, types.I64)
	}
	if t.Equal(types.I32) {
		return g.current.NewZExt(v, types.I64)
	}
	if t.Equal(types.Double) {
		return g.current.NewBitCast(v, types.I64)
	}
	// Pointer types.
	if _, ok := t.(*types.PointerType); ok {
		return g.current.NewPtrToInt(v, types.I64)
	}
	return g.current.NewPtrToInt(v, types.I64)
}

// i64ToType converts an i64 value back to the original LLVM type after loading from a task arg buffer.
func (g *Generator) i64ToType(v value.Value, target types.Type) value.Value {
	if target.Equal(types.I64) {
		return v
	}
	if target.Equal(types.I1) {
		return g.current.NewTrunc(v, types.I1)
	}
	if target.Equal(types.I32) {
		return g.current.NewTrunc(v, types.I32)
	}
	if target.Equal(types.Double) {
		return g.current.NewBitCast(v, types.Double)
	}
	// Pointer types.
	if _, ok := target.(*types.PointerType); ok {
		return g.current.NewIntToPtr(v, target)
	}
	return g.current.NewIntToPtr(v, types.I8Ptr)
}

// Ensure checker import is used (for FuncType in generateArrowFunc and generateSpecializedFunc).
var _ = (*checker.FuncType)(nil)

// generateTaskAll emits IR for Task.all(t1, t2, ...) and Task.allSettled(t1, t2, ...).
// Awaits each task in order and packs the results into a heap-allocated tuple.
func (g *Generator) generateTaskAll(n *parser.CallExpr) (value.Value, error) {
	ft := g.check.Specializations[n]
	tupleCheckerType, _ := ft.Return.(*checker.TupleType)

	elems := make([]value.Value, len(n.Args))
	elemTypes := make([]types.Type, len(n.Args))

	for i, arg := range n.Args {
		handle, err := g.generateExpr(arg)
		if err != nil {
			return nil, err
		}
		raw := g.current.NewCall(g.findFunc("srt_await"), handle)
		// Null the task var alloca so srt_drop_task_handle is a no-op at scope exit.
		if id, ok2 := arg.(*parser.Identifier); ok2 {
			if alloc, exists := g.vars[id.Name]; exists {
				g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
			}
		}
		var elemCheckerType checker.Type = checker.Unknown
		if tupleCheckerType != nil && i < len(tupleCheckerType.Elements) {
			elemCheckerType = tupleCheckerType.Elements[i]
		}
		llvmT := g.mapTypeToLLVM(elemCheckerType)
		elems[i] = g.castFromI8Ptr(raw, llvmT)
		elemTypes[i] = elems[i].Type()
	}

	// Pack into a heap-allocated struct (same layout as generateTupleExpr).
	st := types.NewStruct(elemTypes...)
	size := constant.NewInt(types.I64, int64(len(elems))*8)
	rawPtr := g.current.NewCall(g.findBuiltin("soyuz_alloc"), size, constant.NewNull(types.I8Ptr), constant.NewNull(types.I8Ptr))
	structPtr := g.current.NewBitCast(rawPtr, types.NewPointer(st))
	for i, v := range elems {
		fieldPtr := g.current.NewGetElementPtr(st, structPtr,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(i)))
		g.current.NewStore(v, fieldPtr)
	}
	return structPtr, nil
}

// generateTaskAny emits IR for Task.any(t1, t2, ...).
// Builds a stack array of handles, calls srt_await_any (which awaits the winner and detaches
// the rest), and returns the result cast to the common inner type.
func (g *Generator) generateTaskAny(n *parser.CallExpr) (value.Value, error) {
	nTasks := len(n.Args)
	if nTasks == 0 {
		return constant.NewNull(types.I8Ptr), nil
	}

	handles := make([]value.Value, nTasks)
	for i, arg := range n.Args {
		h, err := g.generateExpr(arg)
		if err != nil {
			return nil, err
		}
		handles[i] = h
	}

	// Build [N x i8*] array on the stack.
	arrType := types.NewArray(uint64(nTasks), types.I8Ptr)
	arrAlloca := g.newAlloca(arrType)
	for i, h := range handles {
		ptr := g.current.NewGetElementPtr(arrType, arrAlloca,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(i)))
		g.current.NewStore(h, ptr)
	}

	// Pointer to first element (i8**).
	firstPtr := g.current.NewGetElementPtr(arrType, arrAlloca,
		constant.NewInt(types.I32, 0), constant.NewInt(types.I32, 0))
	handlesI8PP := g.current.NewBitCast(firstPtr, types.NewPointer(types.I8Ptr))

	raw := g.current.NewCall(g.findFunc("srt_await_any"), handlesI8PP, constant.NewInt(types.I64, int64(nTasks)))

	// Null all task var allocas (all handles consumed by srt_await_any).
	for _, arg := range n.Args {
		if id, ok2 := arg.(*parser.Identifier); ok2 {
			if alloc, exists := g.vars[id.Name]; exists {
				g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
			}
		}
	}

	ft := g.check.Specializations[n]
	if ft == nil || ft.Return == nil || ft.Return == checker.Unknown {
		return raw, nil
	}
	llvmT := g.mapTypeToLLVM(ft.Return)
	return g.castFromI8Ptr(raw, llvmT), nil
}

// generateTaskFan emits IR for Task.fan(input, f, g, h, ...) — fan-out paralelo.
// Spawns a task for each function with the same input value and packs the resulting
// handles into a heap-allocated struct (same layout as a tuple).
// Typical usage: `entrada |> Task.fan(f, g, h)` (pipe injects entrada as first arg).
func (g *Generator) generateTaskFan(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) < 2 {
		return constant.NewNull(types.I8Ptr), nil
	}

	// First arg is the input value shared across all spawned tasks.
	inputVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	inputI64 := g.castToI64(inputVal)
	origInputType := inputVal.Type()

	nFuncs := len(n.Args) - 1
	handles := make([]value.Value, nFuncs)

	for i, arg := range n.Args[1:] {
		// Each fan-out argument must be a named function identifier.
		id, ok := arg.(*parser.Identifier)
		if !ok {
			return nil, fmt.Errorf("Task.fan: argumento %d deve ser uma função (identifier)", i+1)
		}
		targetFunc := g.findFunc(id.Name)
		if targetFunc == nil {
			return nil, fmt.Errorf("Task.fan: função '%s' não encontrada", id.Name)
		}

		// Allocate a per-task args buffer with a single i64 slot for the input value.
		argsHeap := g.current.NewCall(g.findBuiltin("malloc"),
			constant.NewInt(types.I64, 8))
		arrType := types.NewArray(uint64(1), types.I64)
		argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))
		slotPtr := g.current.NewGetElementPtr(arrType, argsPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
		g.current.NewStore(inputI64, slotPtr)

		// Generate a wrapper function that unpacks the input and calls fn.
		wrapperFn := g.generateTaskWrapperFunc(targetFunc, []types.Type{origInputType}, 1)

		// Enqueue the task; collect its handle.
		wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
		handle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
		handles[i] = handle
	}

	// Pack all handles into a heap-allocated struct (same layout as a tuple).
	handleTypes := make([]types.Type, nFuncs)
	for i := range handles {
		handleTypes[i] = types.I8Ptr
	}
	st := types.NewStruct(handleTypes...)
	size := constant.NewInt(types.I64, int64(nFuncs)*8)
	rawPtr := g.current.NewCall(g.findBuiltin("soyuz_alloc"), size, constant.NewNull(types.I8Ptr), constant.NewNull(types.I8Ptr))
	structPtr := g.current.NewBitCast(rawPtr, types.NewPointer(st))
	for i, h := range handles {
		fieldPtr := g.current.NewGetElementPtr(st, structPtr,
			constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(i)))
		g.current.NewStore(h, fieldPtr)
	}
	return structPtr, nil
}

// ── M-19: Task.pipe — pipeline paralelo com channels ────────────────────────

// generateTaskPipeline emits IR for Task.pipe(input, f, g, h, ...).
// Architecture:
//   - If input is a plain value T: creates a 1-capacity channel, sends the value, closes it.
//   - If input is Channel[T]: uses it directly (stream mode).
//   - Creates N output channels (one per stage).
//   - Spawns one task per stage via generatePipelineStageWrapper; each task loops:
//     recv(in_ch) → call fn → send(out_ch) until in_ch is closed, then closes out_ch.
//   - All tasks are detached — they drain the pipeline autonomously.
//   - Returns Channel[R] (output channel of the last stage).
func (g *Generator) generateTaskPipeline(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) < 2 {
		return constant.NewNull(types.I8Ptr), nil
	}

	// Evaluate first argument.
	firstVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}

	// Check if the first arg is already a Channel[T].
	firstCheckerType := g.check.NodeTypes[n.Args[0]]
	isInputChannel := false
	if st, isST := firstCheckerType.(*checker.SpecializedType); isST {
		if ct, isCT := st.Base.(*checker.ClassType); isCT && ct.Name == "Channel" {
			isInputChannel = true
			_ = ct
		}
	}

	var inCh value.Value
	if isInputChannel {
		inCh = firstVal
	} else {
		// Wrap single value in a 1-capacity channel: send and immediately close.
		inCh = g.current.NewCall(g.findFunc("srt_chan_new"), constant.NewInt(types.I64, 1))
		valI64 := g.castToI64(firstVal)
		g.current.NewCall(g.findFunc("srt_chan_send"), inCh, valI64)
		g.current.NewCall(g.findFunc("srt_chan_close"), inCh)
	}

	nStages := len(n.Args) - 1

	// Create one output channel per stage. channels[0] is the input; channels[i+1] is the output of stage i.
	channels := make([]value.Value, nStages+1)
	channels[0] = inCh
	pipelineCap := constant.NewInt(types.I64, 16)
	for i := 1; i <= nStages; i++ {
		channels[i] = g.current.NewCall(g.findFunc("srt_chan_new"), pipelineCap)
	}

	// Spawn one detached task per stage.
	for i, arg := range n.Args[1:] {
		id, ok := arg.(*parser.Identifier)
		if !ok {
			return nil, fmt.Errorf("Task.pipe: argumento %d deve ser uma função (identifier)", i+1)
		}
		targetFunc := g.findFunc(id.Name)
		if targetFunc == nil {
			return nil, fmt.Errorf("Task.pipe: função '%s' não encontrada", id.Name)
		}

		stageWrapper := g.generatePipelineStageWrapper(targetFunc)

		// Pack (in_ch: i64, out_ch: i64) into a 2-slot i64 heap buffer.
		argsHeap := g.current.NewCall(g.findBuiltin("malloc"), constant.NewInt(types.I64, 16))
		arrType := types.NewArray(uint64(2), types.I64)
		argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))

		inSlot := g.current.NewGetElementPtr(arrType, argsPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
		g.current.NewStore(g.castToI64(channels[i]), inSlot)

		outSlot := g.current.NewGetElementPtr(arrType, argsPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		g.current.NewStore(g.castToI64(channels[i+1]), outSlot)

		wrapperPtr := g.current.NewBitCast(stageWrapper, types.I8Ptr)
		handle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)
		g.current.NewCall(g.findFunc("srt_detach"), handle)
	}

	// Return the final output channel (last one).
	return channels[nStages], nil
}

// generatePipelineStageWrapper emits a void @__pipe_stage_N(i8* raw_args) function.
// The wrapper:
//  1. Unpacks in_ch and out_ch from the i64[2] args buffer.
//  2. Loops: recv(in_ch) → call targetFunc → send(out_ch).
//  3. On channel close: close out_ch, set task result null, return.
func (g *Generator) generatePipelineStageWrapper(targetFunc *ir.Func) *ir.Func {
	name := fmt.Sprintf("__pipe_stage_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++

	wrapperFn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", wrapperFn)

	rawArgs := wrapperFn.Params[0]

	// Unpack in_ch and out_ch from i64[2] buffer.
	arrType := types.NewArray(uint64(2), types.I64)
	argsPtr := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))

	inChSlot := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	inChI64 := g.current.NewLoad(types.I64, inChSlot)
	inCh := g.current.NewIntToPtr(inChI64, types.I8Ptr)

	outChSlot := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	outChI64 := g.current.NewLoad(types.I64, outChSlot)
	outCh := g.current.NewIntToPtr(outChI64, types.I8Ptr)

	g.current.NewCall(g.findBuiltin("free"), rawArgs)

	// Create loop blocks.
	fn := wrapperFn
	loopBlock := g.newBlock("pipe_loop", fn)
	loopBody := g.newBlock("pipe_body", fn)
	loopDone := g.newBlock("pipe_done", fn)

	g.current.NewBr(loopBlock)

	// Loop header: recv from in_ch.
	g.current = loopBlock
	outAlloc := g.newAlloca(types.I64)
	g.current.NewStore(constant.NewInt(types.I64, 0), outAlloc)
	recvOk := g.current.NewCall(g.findFunc("srt_chan_recv"), inCh, outAlloc)
	cond := g.current.NewICmp(enum.IPredNE, recvOk, constant.NewInt(types.I64, 0))
	g.current.NewCondBr(cond, loopBody, loopDone)

	// Loop body: call targetFunc, send result.
	g.current = loopBody
	rawVal := g.current.NewLoad(types.I64, outAlloc)

	// Determine the input LLVM type from the target function's first parameter.
	var inputLLVMType types.Type = types.I64
	if len(targetFunc.Params) > 0 {
		inputLLVMType = targetFunc.Params[0].Type()
	}
	typedVal := g.i64ToType(rawVal, inputLLVMType)

	callResult := g.current.NewCall(targetFunc, typedVal)

	// Convert result to i64 and send to out_ch.
	retLLVMType := targetFunc.Sig.RetType
	var resultI64 value.Value
	if retLLVMType == nil || retLLVMType.Equal(types.Void) {
		resultI64 = constant.NewInt(types.I64, 0)
	} else {
		resultI64 = g.castToI64(callResult)
	}
	g.current.NewCall(g.findFunc("srt_chan_send"), outCh, resultI64)
	g.current.NewBr(loopBlock)

	// Done block: close out_ch, set task result null, return.
	g.current = loopDone
	g.current.NewCall(g.findFunc("srt_chan_close"), outCh)
	g.current.NewCall(g.findFunc("srt_set_task_result"), constant.NewNull(types.I8Ptr))
	g.current.NewRet(nil)

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return wrapperFn
}

// generateTaskHandleCurrent emits IR for TaskHandle.current().
// Calls srt_task_handle_current() and wraps the result in Option[TaskHandle]:
// Some(handle) if non-null, None otherwise.
func (g *Generator) generateTaskHandleCurrent() (value.Value, error) {
	raw := g.current.NewCall(g.findFunc("srt_task_handle_current"))

	isNull := g.current.NewICmp(enum.IPredEQ, raw, constant.NewNull(types.I8Ptr))
	fn := g.current.Parent
	someBlock := g.newBlock("task_handle_some", fn)
	noneBlock := g.newBlock("task_handle_none", fn)
	mergeBlock := g.newBlock("task_handle_merge", fn)
	g.current.NewCondBr(isNull, noneBlock, someBlock)

	g.current = someBlock
	someVal, err := g.emitOptionResultAllocNoRetain("Option", 0, raw)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	someEnd := g.current

	g.current = noneBlock
	noneVal, err := g.emitOptionResultAllocNoRetain("Option", 1, nil)
	if err != nil {
		return nil, err
	}
	g.current.NewBr(mergeBlock)
	noneEnd := g.current

	g.current = mergeBlock
	phi := g.current.NewPhi(
		ir.NewIncoming(someVal, someEnd),
		ir.NewIncoming(noneVal, noneEnd),
	)
	return phi, nil
}

// generateTaskTap implements Task[T].tap(fn: T -> Unit) -> Task[T].
//
// The tap desugars into a wrapper task that:
//  1. Awaits the source task handle.
//  2. Calls the callback closure with the result (side-effect only).
//  3. Re-stores the same result via srt_set_task_result.
//
// The wrapper receives [srcHandle (i8*), closure (i8*)] as a 2-slot i64 buffer.
// Returns a new Task[T] handle — the source handle alloca is nulled out.
func (g *Generator) generateTaskTap(me *parser.MemberExpr, n *parser.CallExpr, srcHandle value.Value) (value.Value, error) {
	if len(n.Args) == 0 {
		return nil, fmt.Errorf(".tap: esperado argumento callback")
	}

	// Evaluate the callback (lambda or named fn) in the current context.
	callbackVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}

	// Determine inner LLVM type T for result unpacking inside the wrapper.
	innerLLVMType := types.Type(types.I64)
	if st, ok := g.check.NodeTypes[me.Object].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		innerLLVMType = g.mapTypeToLLVM(st.Params[0])
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

	// Generate the tap wrapper function and enqueue it.
	wrapperFn := g.generateTapWrapperFunc(innerLLVMType)
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	newHandle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)

	// Null the source alloca — ownership transferred to the wrapper.
	if ident, ok := me.Object.(*parser.Identifier); ok {
		if alloc, exists := g.vars[ident.Name]; exists {
			g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
		}
	}

	return newHandle, nil
}

// generateTapWrapperFunc emits `void @__tap_wrapper_N(i8* raw_args)`.
//
// The wrapper:
//  1. Unpacks srcHandle (slot 0) and closure (slot 1) from the args buffer.
//  2. Calls srt_await(srcHandle) to get the result (i8*).
//  3. Casts the result to innerType and calls the closure (side-effect).
//  4. Re-stores the original i8* result via srt_set_task_result.
func (g *Generator) generateTapWrapperFunc(innerType types.Type) *ir.Func {
	name := fmt.Sprintf("__tap_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++

	// Ensure closure type is initialized (needed by callClosureDirect).
	g.getOrCreateClosureDtor()

	fn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", fn)

	// Unpack args buffer.
	rawArgs := fn.Params[0]
	arrType := types.NewArray(uint64(2), types.I64)
	argsBuf := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))

	// Slot 0: source task handle (i8*)
	slot0 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handleI64 := g.current.NewLoad(types.I64, slot0)
	tapSrcHandle := g.current.NewIntToPtr(handleI64, types.I8Ptr)

	// Slot 1: callback closure (i8*)
	slot1 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	closureI64 := g.current.NewLoad(types.I64, slot1)
	closurePtr := g.current.NewIntToPtr(closureI64, types.I8Ptr)

	// Await the source task.
	result := g.current.NewCall(g.findFunc("srt_await"), tapSrcHandle) // i8*

	// Cast result from i8* to innerType for the callback argument.
	var argVal value.Value
	if innerType.Equal(types.I8Ptr) {
		argVal = result
	} else if _, isPtr := innerType.(*types.PointerType); isPtr {
		argVal = g.current.NewBitCast(result, innerType)
	} else {
		// Integer / float: ptrtoint then truncate/bitcast as needed.
		argVal = g.current.NewPtrToInt(result, types.I64)
		if !innerType.Equal(types.I64) {
			argVal = g.current.NewTrunc(argVal, innerType)
		}
	}

	// Call the closure for side-effect (return value discarded).
	g.callClosureDirect(closurePtr, types.Void, []value.Value{argVal})

	// Re-store the original result so the new task carries the same value.
	g.current.NewCall(g.findFunc("srt_set_task_result"), result)

	g.current.NewRet(nil)

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return fn
}

// generateTaskListen implements Task.listen(t: Task[T], ch: Channel[T]) -> Unit.
//
// Spawns a fire-and-forget listener wrapper that:
//  1. Awaits the source task handle.
//  2. Sends the result to the channel via srt_chan_send.
//
// The listener handle is immediately detached. The source handle alloca is
// nulled out so its taskVarStack destructor is a no-op.
func (g *Generator) generateTaskListen(n *parser.CallExpr) (value.Value, error) {
	if len(n.Args) < 2 {
		return nil, fmt.Errorf("Task.listen: esperado 2 argumentos")
	}

	// Evaluate task handle (arg0) and channel (arg1).
	taskHandle, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
	}
	chanVal, err := g.generateExpr(n.Args[1])
	if err != nil {
		return nil, err
	}

	// Pack [taskHandle, chanPtr] as a 2-slot i64 buffer.
	argsHeap := g.current.NewCall(g.findBuiltin("malloc"), constant.NewInt(types.I64, 16))
	arrType := types.NewArray(uint64(2), types.I64)
	argsPtr := g.current.NewBitCast(argsHeap, types.NewPointer(arrType))

	slot0 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(g.castToI64(taskHandle), slot0)

	slot1 := g.current.NewGetElementPtr(arrType, argsPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	g.current.NewStore(g.castToI64(chanVal), slot1)

	// Generate the listener wrapper and enqueue it.
	wrapperFn := g.generateListenWrapperFunc()
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	listenerHandle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)

	// Immediately detach the listener — it is fire-and-forget.
	g.current.NewCall(g.findFunc("srt_detach"), listenerHandle)

	// Null the source task alloca so the taskVarStack destructor is a no-op.
	if id, ok := n.Args[0].(*parser.Identifier); ok {
		if alloc, exists := g.vars[id.Name]; exists {
			g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
		}
	}

	return nil, nil
}

// generateListenWrapperFunc emits `void @__listen_wrapper_N(i8* raw_args)`.
//
// The wrapper:
//  1. Unpacks srcHandle (slot 0) and chanPtr (slot 1) from the args buffer.
//  2. Calls srt_await(srcHandle) to get the result (i8*).
//  3. Converts the result to i64 via ptrtoint and calls srt_chan_send(chan, raw).
func (g *Generator) generateListenWrapperFunc() *ir.Func {
	name := fmt.Sprintf("__listen_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++

	fn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", fn)

	// Unpack args buffer.
	rawArgs := fn.Params[0]
	arrType := types.NewArray(uint64(2), types.I64)
	argsBuf := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))

	// Slot 0: source task handle (i8*)
	slot0 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handleI64 := g.current.NewLoad(types.I64, slot0)
	listenSrcHandle := g.current.NewIntToPtr(handleI64, types.I8Ptr)

	// Slot 1: channel pointer (i8*)
	slot1 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	chanI64 := g.current.NewLoad(types.I64, slot1)
	chanPtr := g.current.NewIntToPtr(chanI64, types.I8Ptr)

	// Await the source task.
	result := g.current.NewCall(g.findFunc("srt_await"), listenSrcHandle) // i8*

	// Convert result (i8*) to i64 for srt_chan_send — channel stores raw i64.
	raw := g.current.NewPtrToInt(result, types.I64)
	g.current.NewCall(g.findFunc("srt_chan_send"), chanPtr, raw)

	g.current.NewRet(nil)

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return fn
}

// generateTaskAlways implements Task[T].always(fn: Unit -> Unit) -> Task[T].
//
// Unlike .tap(fn) which only fires on completion, .always fires on BOTH SRT_DONE
// and SRT_CANCELLED — because M-12's wake_waiters fires in both states, any task
// doing srt_await on the source is woken up regardless of the outcome.
//
// The callback receives no meaningful args (Unit). The original result is
// re-stored unchanged so any downstream .await() retrieves the correct value.
func (g *Generator) generateTaskAlways(me *parser.MemberExpr, n *parser.CallExpr, srcHandle value.Value) (value.Value, error) {
	if len(n.Args) == 0 {
		return nil, fmt.Errorf(".always: esperado argumento callback")
	}

	// Evaluate the callback in the current context (lambda or named fn).
	callbackVal, err := g.generateExpr(n.Args[0])
	if err != nil {
		return nil, err
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

	// Generate the always wrapper and enqueue it.
	wrapperFn := g.generateAlwaysWrapperFunc()
	wrapperPtr := g.current.NewBitCast(wrapperFn, types.I8Ptr)
	newHandle := g.current.NewCall(g.findFunc("srt_enqueue"), wrapperPtr, argsHeap)

	// Null the source alloca — ownership transferred to the wrapper.
	if ident, ok := me.Object.(*parser.Identifier); ok {
		if alloc, exists := g.vars[ident.Name]; exists {
			g.current.NewStore(constant.NewNull(types.I8Ptr), alloc)
		}
	}

	return newHandle, nil
}

// generateAlwaysWrapperFunc emits `void @__always_wrapper_N(i8* raw_args)`.
//
// The wrapper:
//  1. Unpacks srcHandle (slot 0) and closure (slot 1) from the args buffer.
//  2. Calls srt_await(srcHandle) — returns on SRT_DONE OR SRT_CANCELLED.
//  3. Calls the closure with no additional args (fn: Unit → only implicit env ptr).
//  4. Re-stores original result (or null for cancelled) via srt_set_task_result.
func (g *Generator) generateAlwaysWrapperFunc() *ir.Func {
	name := fmt.Sprintf("__always_wrapper_%d", g.taskWrapperCounter)
	g.taskWrapperCounter++

	// Ensure closure type is initialized (needed by callClosureDirect).
	g.getOrCreateClosureDtor()

	fn := g.module.NewFunc(name, types.Void, ir.NewParam("raw_args", types.I8Ptr))

	// Save generator state.
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
	g.current = g.newBlock("entry", fn)

	// Unpack args buffer.
	rawArgs := fn.Params[0]
	arrType := types.NewArray(uint64(2), types.I64)
	argsBuf := g.current.NewBitCast(rawArgs, types.NewPointer(arrType))

	// Slot 0: source task handle (i8*)
	slot0 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	handleI64 := g.current.NewLoad(types.I64, slot0)
	alwaysSrcHandle := g.current.NewIntToPtr(handleI64, types.I8Ptr)

	// Slot 1: callback closure (i8*)
	slot1 := g.current.NewGetElementPtr(arrType, argsBuf,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
	closureI64 := g.current.NewLoad(types.I64, slot1)
	closurePtr := g.current.NewIntToPtr(closureI64, types.I8Ptr)

	// Await source — returns on both SRT_DONE and SRT_CANCELLED (M-12 guarantee).
	result := g.current.NewCall(g.findFunc("srt_await"), alwaysSrcHandle) // i8*

	// Call closure with no additional args (Unit → only implicit env pointer).
	g.callClosureDirect(closurePtr, types.Void, []value.Value{})

	// Re-store original result so the chain continues with the correct value.
	g.current.NewCall(g.findFunc("srt_set_task_result"), result)

	g.current.NewRet(nil)

	// Restore generator state.
	g.current = oldCurrent
	g.vars = oldVars
	g.heapVars = oldHeapVars
	g.scopeStack = oldScopeStack
	g.taskVarStack = oldTaskVarStack
	g.syncGuardStack = oldSyncGuardStack
	g.arcVarStack = oldArcVarStack
	g.blockNames = oldBlockNames
	g.currentReturnType = oldReturnType

	return fn
}
