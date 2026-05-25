package codegen

import (
	"fmt"
	"strconv"
	"strings"
	"soyuz/internal/checker"
	"soyuz/internal/parser"

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
		// Fall back to global function
		if f := g.findFunc(n.Name); f != nil {
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

	case *parser.PipeQuestExpr:
		return g.generatePipeQuestExpr(n)

	case *parser.ElvisExpr:
		return g.generateElvisExpr(n)

	case *parser.AssignExpr:
		// When assigning Some(x) to a weak field, the Option payload must NOT be retained,
		// otherwise the weak reference would still increment the reference count of x.
		var val value.Value
		var err error
		if me, isMember := n.Left.(*parser.MemberExpr); isMember && g.isWeakMember(me) {
			if se, isSome := n.Right.(*parser.SomeExpr); isSome {
				inner, innerErr := g.generateExpr(se.Value)
				if innerErr != nil {
					return nil, innerErr
				}
				val, err = g.emitOptionResultAllocNoRetain("Option", 0, inner)
			}
		}
		if val == nil {
			val, err = g.generateExpr(n.Right)
		}
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
				// Release old value
				ptrType := alloc.Type().(*types.PointerType)
				old := g.current.NewLoad(ptrType.ElemType, alloc)
				g.emitRelease(old)
				// Retain new value
				g.emitRetain(val)
			}

			g.current.NewStore(val, alloc)
		case *parser.MemberExpr:
			ptr, err := g.generateMemberPtr(l)
			if err != nil {
				return nil, err
			}

			// Handle RC for member assignment; weak fields do not own the value.
			isWeak := g.isWeakMember(l)
			if g.isHeapType(val.Type()) && !isWeak {
				// Release old value
				old := g.current.NewLoad(val.Type(), ptr)
				g.emitRelease(old)
				// Retain new value
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
		g.current.NewBr(g.loops[len(g.loops)-1].after)
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
		}
		g.releaseAllScopes()

		retType := g.current.Parent.Sig.RetType
		if retType.Equal(types.Void) {
			g.current.NewRet(nil)
		} else {
			if val == nil {
				g.current.NewRet(g.defaultReturnValue(retType))
			} else {
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
	return val, nil
}

// wrapInInterfaceFatPtr packs a class pointer and a vtable into a SoyuzClosure fat pointer,
// returning the result as i8* (the runtime representation of an interface value).
func (g *Generator) wrapInInterfaceFatPtr(objPtr value.Value, vtable *ir.Global) (value.Value, error) {
	closureRaw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 16), constant.NewNull(types.I8Ptr))
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
	rawPtr := g.current.NewCall(g.findBuiltin("soyuz_alloc"), size, constant.NewNull(types.I8Ptr))
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
			if t.Name == "String" {
				isMethod = true
			}
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
	var dtorArg value.Value
	if dtor, ok := g.destructors[ei.typ.TypeName]; ok {
		dtorArg = g.current.NewBitCast(dtor, types.I8Ptr)
	} else {
		dtorArg = constant.NewNull(types.I8Ptr)
	}
	raw := g.current.NewCall(g.findBuiltin("soyuz_alloc"), constant.NewInt(types.I64, 72), dtorArg)
	structPtr := g.current.NewBitCast(raw, types.NewPointer(ei.typ))
	tagPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(constant.NewInt(types.I64, int64(vi.tag)), tagPtr)
	if len(args) > 0 {
		val := args[0]
		if g.isHeapType(val.Type()) {
			g.emitRetain(val)
		}
		payloadPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(val.Type()))
		g.current.NewStore(val, castPtr)
	}
	return structPtr, nil
}

func (g *Generator) generateEnumConstructor(ei enumInfo, vi variantInfo, n *parser.CallExpr) (value.Value, error) {
	// Look up the enum destructor by type name.
	var dtorArg value.Value
	if dtor, ok := g.destructors[ei.typ.TypeName]; ok {
		dtorArg = g.current.NewBitCast(dtor, types.I8Ptr)
	} else {
		dtorArg = constant.NewNull(types.I8Ptr)
	}

	// Enum layout is 72 bytes: 8 (tag i64) + 64 ([64 x i8] payload).
	raw := g.current.NewCall(g.findBuiltin("soyuz_alloc"),
		constant.NewInt(types.I64, 72), dtorArg)
	structPtr := g.current.NewBitCast(raw, types.NewPointer(ei.typ))

	tagPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
	g.current.NewStore(constant.NewInt(types.I64, int64(vi.tag)), tagPtr)

	if len(n.Args) > 0 {
		val, err := g.generateExpr(n.Args[0])
		if err != nil {
			return nil, err
		}
		// Retain heap-typed payload: the enum struct now co-owns it.
		if g.isHeapType(val.Type()) {
			g.emitRetain(val)
		}
		payloadPtr := g.current.NewGetElementPtr(ei.typ, structPtr,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
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
	if g.isHeapType(obj.Type()) {
		if _, ok := me.Object.(*parser.Identifier); ok {
			g.emitRetain(obj)
		} else if _, ok := me.Object.(*parser.MemberExpr); ok {
			g.emitRetain(obj)
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
			case "append":
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
			}
		}
	}

	// String extension method dispatch: "hello".len() → StringExtensions_len("hello")
	if bt, ok := g.check.NodeTypes[me.Object].(*checker.BasicType); ok && bt.Name == "String" {
		if ci, exists := g.classes["StringExtensions"]; exists {
			if variants, ok2 := ci.methods[me.Property]; ok2 {
				fn := classMethodByArity(variants, len(args))
				if fn != nil {
					// obj is %SoyuzString* — __self param is i8*, so bitcast
					objAsI8 := g.current.NewBitCast(obj, types.I8Ptr)
					allArgs := append([]value.Value{objAsI8}, args...)
					return g.current.NewCall(fn, allArgs...), nil
				}
			}
		}
		return nil, fmt.Errorf("String não tem método '%s' no codegen", me.Property)
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

	calleeVal, err := g.generateExpr(callee)
	if err != nil {
		return nil, checker.Unknown, err
	}

	args := []value.Value{payload}
	for _, arg := range extraArgs {
		v, err := g.generateExpr(arg)
		if err != nil {
			return nil, checker.Unknown, err
		}
		args = append(args, v)
	}

	retVal := g.current.NewCall(calleeVal, args...)
	var retType checker.Type = checker.Unknown
	if ft, ok := g.check.NodeTypes[callee].(*checker.FuncType); ok {
		retType = ft.Return
	}
	if sp, ok := g.check.Specializations[n]; ok {
		retType = sp.Return
	}
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

	// Determine the inner payload LLVM type from the checker.
	var payloadType types.Type = types.I64
	if st, ok := g.check.NodeTypes[n].(*checker.SpecializedType); ok && len(st.Params) > 0 {
		payloadType = g.mapTypeToLLVM(st.Params[0])
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
	// If the value is a concrete class that implements Error, wrap it now.
	if ptrType, ok := val.Type().(*types.PointerType); ok {
		if st, ok2 := ptrType.ElemType.(*types.StructType); ok2 {
			if ci, ok3 := g.classes[st.TypeName]; ok3 {
				if vtable, ok4 := ci.vtables["Error"]; ok4 {
					val, err = g.wrapInInterfaceFatPtr(val, vtable)
					if err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return g.emitOptionResultAlloc("Result", 1, val)
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
		constant.NewInt(types.I64, 72), constant.NewNull(types.I8Ptr))
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

// Ensure checker import is used (for FuncType in generateArrowFunc and generateSpecializedFunc).
var _ = (*checker.FuncType)(nil)
