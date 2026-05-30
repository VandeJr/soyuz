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

// generateBlock processes a block statement with a new RC ownership scope.
// Heap vars declared inside are released when the block exits normally.
func (g *Generator) generateBlock(n *parser.BlockStmt) (value.Value, error) {
	g.pushScope()
	var lastVal value.Value
	for i, stmt := range n.Statements {
		if g.current.Term != nil {
			break
		}
		// For the last ExprStmt, evaluate the inner expression directly — bypassing
		// the ExprStmt release. The block's return value is not "unused"; it will be
		// retained by prepareReturn in the caller (function generator, match arm, etc.).
		// Mid-block ExprStmts are still evaluated via generateExpr (with release).
		isLastExprStmt := i == len(n.Statements)-1
		var val value.Value
		var err error
		if isLastExprStmt {
			if es, ok := stmt.(*parser.ExprStmt); ok {
				val, err = g.generateExpr(es.Expr)
			} else {
				val, err = g.generateExpr(stmt)
			}
		} else {
			val, err = g.generateExpr(stmt)
		}
		if err != nil {
			g.popScopeAndRelease()
			return nil, err
		}
		lastVal = val
	}
	g.popScopeAndRelease()
	return lastVal, nil
}

func (g *Generator) generateIfStmt(n *parser.IfStmt) (value.Value, error) {
	cond, err := g.generateExpr(n.Condition)
	if err != nil {
		return nil, err
	}
	fn := g.current.Parent
	thenBlock := g.newBlock("then", fn)
	elseBlock := g.newBlock("else", fn)
	mergeBlock := g.newBlock("if_merge", fn)
	g.current.NewCondBr(cond, thenBlock, elseBlock)

	g.current = thenBlock
	if _, err = g.generateExpr(n.Consequent); err != nil {
		return nil, err
	}
	if g.current.Term == nil {
		g.current.NewBr(mergeBlock)
	}

	g.current = elseBlock
	if n.Alternate != nil {
		if _, err = g.generateExpr(n.Alternate); err != nil {
			return nil, err
		}
	}
	if g.current.Term == nil {
		g.current.NewBr(mergeBlock)
	}

	g.current = mergeBlock
	return nil, nil
}

func (g *Generator) generateWhileStmt(n *parser.WhileStmt) (value.Value, error) {
	fn := g.current.Parent
	condBlock := g.newBlock("while_cond", fn)
	bodyBlock := g.newBlock("while_body", fn)
	afterBlock := g.newBlock("while_after", fn)

	g.current.NewBr(condBlock)
	g.current = condBlock
	cond, err := g.generateExpr(n.Condition)
	if err != nil {
		return nil, err
	}
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	g.loops = append(g.loops, loopCtx{cond: condBlock, after: afterBlock})
	g.current = bodyBlock
	if _, err = g.generateExpr(n.Body); err != nil {
		return nil, err
	}
	if g.current.Term == nil {
		g.current.NewBr(condBlock)
	}
	g.loops = g.loops[:len(g.loops)-1]

	g.current = afterBlock
	return nil, nil
}

func (g *Generator) generateLoopStmt(n *parser.LoopStmt) (value.Value, error) {
	fn := g.current.Parent
	bodyBlock := g.newBlock("loop_body", fn)
	afterBlock := g.newBlock("loop_after", fn)

	var resultAlloca value.Value
	var resultLLVM types.Type
	if loopTy := g.check.NodeTypes[n]; loopTy != nil && loopTy.String() != "Unit" {
		resultLLVM = g.mapTypeToLLVM(loopTy)
		resultAlloca = g.newAlloca(resultLLVM)
		if def := g.defaultReturnValue(resultLLVM); def != nil {
			g.current.NewStore(def, resultAlloca)
		}
	}

	g.current.NewBr(bodyBlock)
	g.loops = append(g.loops, loopCtx{
		cond:         bodyBlock,
		after:        afterBlock,
		resultAlloca: resultAlloca,
		resultLLVM:   resultLLVM,
	})
	g.current = bodyBlock
	if _, err := g.generateExpr(n.Body); err != nil {
		return nil, err
	}
	if g.current.Term == nil {
		g.current.NewBr(bodyBlock)
	}
	g.loops = g.loops[:len(g.loops)-1]

	g.current = afterBlock
	if resultAlloca != nil {
		val := g.current.NewLoad(resultLLVM, resultAlloca)
		return g.prepareReturn(val), nil
	}
	return nil, nil
}

// generateForStmt implements `for binding in iterable { body }`.
// Supports range, List[T], Map[K,V], and Iterator[T].
func (g *Generator) generateForStmt(n *parser.ForStmt) (value.Value, error) {
	if rangeExpr, ok := n.Iterable.(*parser.RangeExpr); ok {
		return g.generateForRange(n.Binding, rangeExpr, n.Body)
	}
	if st, ok := g.check.NodeTypes[n.Iterable].(*checker.SpecializedType); ok {
		if ct, ok2 := st.Base.(*checker.ClassType); ok2 {
			switch ct.Name {
			case "Map":
				return g.generateForMap(n)
			case "Iterator":
				return g.generateForIterator(n)
			}
		}
	}
	return g.generateForList(n)
}

func (g *Generator) generateForRange(binding string, rangeExpr *parser.RangeExpr, body *parser.BlockStmt) (value.Value, error) {
	start, err := g.generateExpr(rangeExpr.From)
	if err != nil {
		return nil, err
	}
	end, err := g.generateExpr(rangeExpr.To)
	if err != nil {
		return nil, err
	}

	fn := g.current.Parent
	alloc := g.newAlloca(types.I64)
	g.current.NewStore(start, alloc)
	g.vars[binding] = alloc

	condBlock := g.newBlock("for_cond", fn)
	bodyBlock := g.newBlock("for_body", fn)
	incrBlock := g.newBlock("for_incr", fn)
	afterBlock := g.newBlock("for_after", fn)

	g.current.NewBr(condBlock)

	g.current = condBlock
	counter := g.current.NewLoad(types.I64, alloc)
	var cond value.Value
	if rangeExpr.Inclusive {
		cond = g.current.NewICmp(enum.IPredSLE, counter, end)
	} else {
		cond = g.current.NewICmp(enum.IPredSLT, counter, end)
	}
	g.current.NewCondBr(cond, bodyBlock, afterBlock)

	g.loops = append(g.loops, loopCtx{cond: incrBlock, after: afterBlock})
	g.current = bodyBlock
	if _, err = g.generateExpr(body); err != nil {
		return nil, err
	}
	if g.current.Term == nil {
		g.current.NewBr(incrBlock)
	}
	g.loops = g.loops[:len(g.loops)-1]

	g.current = incrBlock
	cur := g.current.NewLoad(types.I64, alloc)
	next := g.current.NewAdd(cur, constant.NewInt(types.I64, 1))
	g.current.NewStore(next, alloc)
	g.current.NewBr(condBlock)

	g.current = afterBlock
	return nil, nil
}

func (g *Generator) generateForList(n *parser.ForStmt) (value.Value, error) {
	listVal, err := g.generateExpr(n.Iterable)
	if err != nil {
		return nil, err
	}

	st, ok := g.check.NodeTypes[n.Iterable].(*checker.SpecializedType)
	if !ok {
		return nil, fmt.Errorf("for-in: tipo do iterável não é List[T]")
	}
	return g.generateForListValue(n.Binding, listVal, st.Params[0], n.Body)
}

func (g *Generator) generateLogicalExpr(n *parser.BinaryExpr) (value.Value, error) {
	left, err := g.generateExpr(n.Left)
	if err != nil {
		return nil, err
	}
	leftBlock := g.current
	fn := g.current.Parent
	rightBlock := g.newBlock("logic_right", fn)
	mergeBlock := g.newBlock("logic_merge", fn)

	if n.Operator == "&&" {
		leftBlock.NewCondBr(left, rightBlock, mergeBlock)
	} else {
		leftBlock.NewCondBr(left, mergeBlock, rightBlock)
	}

	g.current = rightBlock
	right, err := g.generateExpr(n.Right)
	if err != nil {
		return nil, err
	}
	rightBlockFinal := g.current
	if rightBlockFinal.Term == nil {
		rightBlockFinal.NewBr(mergeBlock)
	}

	g.current = mergeBlock
	phi := g.current.NewPhi(ir.NewIncoming(left, leftBlock), ir.NewIncoming(right, rightBlockFinal))
	return phi, nil
}
