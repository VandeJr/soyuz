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

	case *parser.StringLiteral:
		s := n.Value + "\x00"
		cs := constant.NewCharArrayFromString(s)
		glob := g.module.NewGlobalDef("", cs)
		glob.Immutable = true
		return g.current.NewGetElementPtr(cs.Type(), glob,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I64, 0)), nil

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

	// 4. Method call: obj.method(args)
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		objCheckerType := g.check.NodeTypes[me.Object]
		isMethod := false
		switch t := objCheckerType.(type) {
		case *checker.ClassType, *checker.InterfaceType:
			isMethod = true
		case *checker.SpecializedType:
			if _, ok := t.Base.(*checker.ClassType); ok {
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

	args, err := g.generateCallArgs(n.Args)
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
	return g.current.NewCall(printf, append([]value.Value{ptr}, printArgs...)...), nil
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
		case val.Type().Equal(types.I8Ptr):
			b.WriteString("%s")
		case val.Type().Equal(types.I64):
			b.WriteString("%lld")
		case val.Type().Equal(types.Double):
			b.WriteString("%f")
		case val.Type().Equal(types.I1):
			b.WriteString("%d")
			val = g.current.NewZExt(val, types.I32)
		default:
			b.WriteString("%p")
		}
		args = append(args, val)
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
	return buf, nil
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
			}
		}
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
				if fn, ok4 := ci.methods[me.Property]; ok4 {
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

// Ensure checker import is used (for FuncType in generateArrowFunc and generateSpecializedFunc).
var _ = (*checker.FuncType)(nil)
