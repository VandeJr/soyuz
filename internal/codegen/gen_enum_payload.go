package codegen

import (
	"soyuz/internal/checker"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// enumVariantPayloadLLVMType returns the LLVM type stored in an enum variant's payload area.
func enumVariantPayloadLLVMType(vi variantInfo) types.Type {
	if len(vi.fields) == 0 {
		return types.I64
	}
	if len(vi.fields) == 1 {
		return vi.fields[0]
	}
	return types.NewStruct(vi.fields...)
}

// loadEnumPayloadValue loads a value from an enum's payload bytes using the expected LLVM type.
func (g *Generator) loadEnumPayloadValue(payloadPtr value.Value, llvmType types.Type) value.Value {
	castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(llvmType))
	return g.current.NewLoad(llvmType, castPtr)
}

// tryStructDeepEqual emits field-wise comparison for heap records (pointer identity + field values).
func (g *Generator) tryStructDeepEqual(leftNode, rightNode parser.Node, left, right value.Value) (value.Value, bool, error) {
	leftType := g.check.NodeTypes[leftNode]
	rightType := g.check.NodeTypes[rightNode]
	lr, ok1 := leftType.(*checker.RecordType)
	rr, ok2 := rightType.(*checker.RecordType)
	if !ok1 || !ok2 || lr.Name != rr.Name {
		return nil, false, nil
	}
	si, ok := g.structs[lr.Name]
	if !ok {
		return nil, false, nil
	}
	lp, ok1 := left.Type().(*types.PointerType)
	rp, ok2 := right.Type().(*types.PointerType)
	if !ok1 || !ok2 {
		return nil, false, nil
	}
	if !lp.ElemType.Equal(si.typ) || !rp.ElemType.Equal(si.typ) {
		return nil, false, nil
	}
	ptrEq := g.current.NewICmp(enum.IPredEQ, left, right)
	mergeBlock := g.newBlock("rec_eq_merge", g.current.Parent)
	shortBlock := g.newBlock("rec_eq_short", g.current.Parent)
	deepBlock := g.newBlock("rec_eq_deep", g.current.Parent)
	g.current.NewCondBr(ptrEq, shortBlock, deepBlock)

	g.current = shortBlock
	g.current.NewBr(mergeBlock)
	shortOut := g.current

	g.current = deepBlock
	var fieldEqs []value.Value
	for i := range si.typ.Fields {
		lPtr := g.current.NewGetElementPtr(si.typ, left,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
		rPtr := g.current.NewGetElementPtr(si.typ, right,
			constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(i)))
		lv := g.current.NewLoad(si.typ.Fields[i], lPtr)
		rv := g.current.NewLoad(si.typ.Fields[i], rPtr)
		var feq value.Value
		if si.typ.Fields[i].Equal(types.Double) {
			feq = g.current.NewFCmp(enum.FPredOEQ, lv, rv)
		} else if si.typ.Fields[i].Equal(types.I1) {
			feq = g.current.NewICmp(enum.IPredEQ, lv, rv)
		} else if _, ok := si.typ.Fields[i].(*types.PointerType); ok {
			feq = g.current.NewICmp(enum.IPredEQ, lv, rv)
		} else {
			feq = g.current.NewICmp(enum.IPredEQ, lv, rv)
		}
		fieldEqs = append(fieldEqs, feq)
	}
	deepResult := fieldEqs[0]
	for _, feq := range fieldEqs[1:] {
		deepResult = g.current.NewAnd(deepResult, feq)
	}
	g.current.NewBr(mergeBlock)
	deepOut := g.current

	g.current = mergeBlock
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(constant.True, shortOut),
		ir.NewIncoming(deepResult, deepOut),
	)
	return g.boolI1(phi), true, nil
}

// optionPrimitiveInnerType reports the inner type when t is Option[T] and T is printable as primitive.
func (g *Generator) optionPrimitiveInnerType(t checker.Type) (checker.Type, bool) {
	st, ok := t.(*checker.SpecializedType)
	if !ok {
		return nil, false
	}
	et, ok := st.Base.(*checker.EnumType)
	if !ok || et.Name != "Option" || len(st.Params) == 0 {
		return nil, false
	}
	switch st.Params[0] {
	case checker.IntType, checker.FloatType, checker.BoolType, checker.StringType, checker.CharType:
		return st.Params[0], true
	default:
		return nil, false
	}
}

// emitOptionPrimitiveForPrint unwraps Option[T] for printf when T is a primitive (defaults None to zero).
func (g *Generator) emitOptionPrimitiveForPrint(optVal value.Value, inner checker.Type) (value.Value, error) {
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
	someBlock := g.newBlock("opt_print_some", fn)
	noneBlock := g.newBlock("opt_print_none", fn)
	mergeBlock := g.newBlock("opt_print_merge", fn)
	g.current.NewCondBr(isSome, someBlock, noneBlock)

	innerLLVM := g.mapTypeToLLVM(inner)
	payloadPtr := g.current.NewGetElementPtr(ei.typ, optPtr,
		constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))

	g.current = someBlock
	someVal := g.loadEnumPayloadValue(payloadPtr, innerLLVM)
	g.current.NewBr(mergeBlock)
	someOut := g.current

	g.current = noneBlock
	var zero value.Value
	switch inner {
	case checker.FloatType:
		zero = constant.NewFloat(types.Double, 0)
	case checker.BoolType:
		zero = constant.False
	case checker.StringType:
		zero = constant.NewNull(types.I8Ptr)
	case checker.CharType:
		zero = constant.NewInt(types.I32, 0)
	default:
		zero = constant.NewInt(types.I64, 0)
	}
	g.current.NewBr(mergeBlock)
	noneOut := g.current

	g.current = mergeBlock
	return mergeBlock.NewPhi(
		ir.NewIncoming(someVal, someOut),
		ir.NewIncoming(zero, noneOut),
	), nil
}

// coerceCallResult maps extern C Bool (i32) and similar to Soyuz i1 when the checker type is Bool.
func (g *Generator) coerceCallResult(val value.Value, t checker.Type) value.Value {
	if t != checker.BoolType {
		return val
	}
	return g.boolI1(val)
}

// boolI1 normalizes a value to i1 for Bool contexts (if conditions, Bool variables).
func (g *Generator) boolI1(v value.Value) value.Value {
	if v.Type().Equal(types.I1) {
		return v
	}
	if v.Type().Equal(types.I64) {
		return g.current.NewICmp(enum.IPredNE, v, constant.NewInt(types.I64, 0))
	}
	if v.Type().Equal(types.I32) {
		return g.current.NewICmp(enum.IPredNE, v, constant.NewInt(types.I32, 0))
	}
	return g.current.NewICmp(enum.IPredNE, v, constant.NewInt(types.I32, 0))
}
