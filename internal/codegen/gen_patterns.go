package codegen

import (
	"fmt"
	"maps"
	"soyuz/internal/parser"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	"github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

func (g *Generator) generateMatchExpr(n *parser.MatchExpr) (value.Value, error) {
	subject, err := g.generateExpr(n.Subject)
	if err != nil {
		return nil, err
	}

	fn := g.current.Parent
	mergeBlock := g.newBlock("match_merge", fn)

	var phiIncomings []*ir.Incoming

	for i, arm := range n.Arms {
		variantNext := g.newBlock(fmt.Sprintf("match_arm_%d_next", i), fn)
		variantBody := g.newBlock(fmt.Sprintf("match_arm_%d_body", i), fn)

		oldVars := maps.Clone(g.vars)

		matchOk := g.newBlock(fmt.Sprintf("match_arm_%d_pattern_ok", i), fn)
		if err := g.matchPattern(subject, arm.Pattern, matchOk, variantNext); err != nil {
			return nil, err
		}

		g.current = matchOk
		if arm.Guard != nil {
			guardCond, err := g.generateExpr(arm.Guard)
			if err != nil {
				return nil, err
			}
			g.current.NewCondBr(guardCond, variantBody, variantNext)
		} else if g.current.Term == nil {
			g.current.NewBr(variantBody)
		}

		g.current = variantBody
		armVal, err := g.generateExpr(arm.Body)
		if err != nil {
			return nil, err
		}

		armFinalBlock := g.current
		if armFinalBlock.Term == nil {
			armFinalBlock.NewBr(mergeBlock)
		}

		if armVal != nil && !armVal.Type().Equal(types.Void) {
			phiIncomings = append(phiIncomings, ir.NewIncoming(armVal, armFinalBlock))
		}

		g.vars = oldVars
		g.current = variantNext
	}

	// Default: no arm matched — fall through to merge.
	if g.current.Term == nil {
		g.current.NewBr(mergeBlock)
		if len(phiIncomings) > 0 {
			phiIncomings = append(phiIncomings, ir.NewIncoming(
				g.defaultReturnValue(phiIncomings[0].X.Type()), g.current))
		}
	}

	g.current = mergeBlock
	if len(phiIncomings) > 0 {
		return mergeBlock.NewPhi(phiIncomings...), nil
	}
	return nil, nil
}

func (g *Generator) matchPattern(val value.Value, pat parser.Pattern, thenBlock *ir.Block, nextBlock *ir.Block) error {
	switch p := pat.(type) {
	case *parser.WildcardPattern:
		g.current.NewBr(thenBlock)

	case *parser.BindingPattern:
		alloc := g.newAlloca(val.Type())
		g.current.NewStore(val, alloc)
		g.vars[p.Name] = alloc
		g.current.NewBr(thenBlock)

	case *parser.LiteralPattern:
		lit, err := g.generateExpr(p.Value)
		if err != nil {
			return err
		}
		var cond value.Value
		if val.Type().Equal(types.Double) {
			cond = g.current.NewFCmp(enum.FPredOEQ, val, lit)
		} else {
			cond = g.current.NewICmp(enum.IPredEQ, val, lit)
		}
		g.current.NewCondBr(cond, thenBlock, nextBlock)

	case *parser.RangePattern:
		from, err := g.generateExpr(p.From)
		if err != nil {
			return err
		}
		to, err := g.generateExpr(p.To)
		if err != nil {
			return err
		}

		fn := g.current.Parent
		thenGE := g.newBlock("range_ge", fn)
		ge := g.current.NewICmp(enum.IPredSGE, val, from)
		g.current.NewCondBr(ge, thenGE, nextBlock)

		g.current = thenGE
		var le value.Value
		if p.Inclusive {
			le = g.current.NewICmp(enum.IPredSLE, val, to)
		} else {
			le = g.current.NewICmp(enum.IPredSLT, val, to)
		}
		g.current.NewCondBr(le, thenBlock, nextBlock)

	case *parser.ConstructorPattern:
		ei, ok := g.enums[p.Name]
		if !ok {
			for _, e := range g.enums {
				if _, found := e.variants[p.Name]; found {
					ei = e
					break
				}
			}
		}

		vi, ok := ei.variants[p.Name]
		if !ok {
			return fmt.Errorf("unknown enum variant in pattern: %s", p.Name)
		}

		var tag value.Value
		var payloadPtr value.Value
		if _, ok := val.Type().(*types.PointerType); ok {
			tagPtr := g.current.NewGetElementPtr(ei.typ, val,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
			tag = g.current.NewLoad(types.I64, tagPtr)
			payloadPtr = g.current.NewGetElementPtr(ei.typ, val,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		} else {
			alloc := g.newAlloca(val.Type())
			g.current.NewStore(val, alloc)
			tagPtr := g.current.NewGetElementPtr(ei.typ, alloc,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 0))
			tag = g.current.NewLoad(types.I64, tagPtr)
			payloadPtr = g.current.NewGetElementPtr(ei.typ, alloc,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, 1))
		}

		cond := g.current.NewICmp(enum.IPredEQ, tag, constant.NewInt(types.I64, int64(vi.tag)))
		matchOk := g.newBlock(fmt.Sprintf("match_%s_ok", p.Name), g.current.Parent)
		g.current.NewCondBr(cond, matchOk, nextBlock)
		g.current = matchOk

		if len(p.Args) > 0 {
			var fieldType types.Type = types.I64
			if len(vi.fields) > 0 {
				fieldType = vi.fields[0]
			}
			castPtr := g.current.NewBitCast(payloadPtr, types.NewPointer(fieldType))
			innerVal := g.current.NewLoad(fieldType, castPtr)
			return g.matchPattern(innerVal, p.Args[0], thenBlock, nextBlock)
		}
		g.current.NewBr(thenBlock)

	case *parser.RecordPattern:
		ptrType, ok := val.Type().(*types.PointerType)
		structVal := val
		if !ok {
			alloc := g.newAlloca(val.Type())
			g.current.NewStore(val, alloc)
			structVal = alloc
			ptrType = types.NewPointer(val.Type())
		}
		st := ptrType.ElemType.(*types.StructType)
		si := g.structs[st.TypeName]

		for _, f := range p.Fields {
			idx := si.fieldIndices[f.Name]
			ptr := g.current.NewGetElementPtr(st, structVal,
				constant.NewInt(types.I64, 0), constant.NewInt(types.I32, int64(idx)))
			fieldVal := g.current.NewLoad(st.Fields[idx], ptr)

			if f.Pattern != nil {
				fieldNext := g.newBlock("field_match_ok", g.current.Parent)
				if err := g.matchPattern(fieldVal, f.Pattern, fieldNext, nextBlock); err != nil {
					return err
				}
				g.current = fieldNext
			} else {
				alloc := g.newAlloca(fieldVal.Type())
				g.current.NewStore(fieldVal, alloc)
				g.vars[f.Name] = alloc
			}
		}
		g.current.NewBr(thenBlock)

	case *parser.TuplePattern:
		ptrType, ok := val.Type().(*types.PointerType)
		if !ok {
			return fmt.Errorf("tuple pattern: expected struct pointer, got %v", val.Type())
		}
		st, ok := ptrType.ElemType.(*types.StructType)
		if !ok {
			return fmt.Errorf("tuple pattern: pointer elem is not a struct, got %v", ptrType.ElemType)
		}
		if len(p.Elements) == 0 {
			g.current.NewBr(thenBlock)
			return nil
		}
		for i, elem := range p.Elements {
			ptr := g.current.NewGetElementPtr(st, val,
				constant.NewInt(types.I32, 0), constant.NewInt(types.I32, int64(i)))
			fieldVal := g.current.NewLoad(st.Fields[i], ptr)
			var elemThen *ir.Block
			if i == len(p.Elements)-1 {
				elemThen = thenBlock
			} else {
				elemThen = g.newBlock(fmt.Sprintf("tuple_elem_%d_ok", i), g.current.Parent)
			}
			if err := g.matchPattern(fieldVal, elem, elemThen, nextBlock); err != nil {
				return err
			}
			g.current = elemThen
		}

	default:
		return fmt.Errorf("unsupported pattern in codegen: %T", pat)
	}
	return nil
}
