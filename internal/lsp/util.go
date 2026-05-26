package lsp

import (
	"soyuz/internal/lexer"
	"soyuz/internal/parser"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

func fromLSPPosition(p protocol.Position) lexer.Position {
	return lexer.Position{
		Line:   int(p.Line) + 1,
		Column: int(p.Character) + 1,
	}
}

func toLSPPosition(p lexer.Position) protocol.Position {
	line := p.Line - 1
	col := p.Column - 1
	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}
	return protocol.Position{
		Line:      protocol.UInteger(line),
		Character: protocol.UInteger(col),
	}
}

func toLSPRange(pos lexer.Position) protocol.Range {
	start := toLSPPosition(pos)
	return protocol.Range{
		Start: start,
		End:   protocol.Position{Line: start.Line, Character: start.Character + 20},
	}
}

func toLSPLocation(uri string, pos lexer.Position) protocol.Location {
	return protocol.Location{URI: uri, Range: toLSPRange(pos)}
}

// posLE reports whether a <= b.
func posLE(a, b lexer.Position) bool {
	if a.Line != b.Line {
		return a.Line <= b.Line
	}
	return a.Column <= b.Column
}

// posGT reports whether a > b.
func posGT(a, b lexer.Position) bool {
	if a.Line != b.Line {
		return a.Line > b.Line
	}
	return a.Column > b.Column
}

// findNodeAt returns the deepest AST node whose start position is ≤ the
// cursor (closest-preceding heuristic — AST nodes store only their start).
func findNodeAt(prog *parser.Program, pos lexer.Position) parser.Node {
	var best parser.Node
	var bestPos lexer.Position
	walkAST(prog, func(n parser.Node) {
		np := n.Pos()
		if posLE(np, pos) && posGT(np, bestPos) {
			best = n
			bestPos = np
		}
	})
	return best
}

// walkAST visits every reachable node in the AST via DFS.
func walkAST(prog *parser.Program, fn func(parser.Node)) {
	for _, node := range prog.Body {
		walkNode(node, fn)
	}
}

func walkNode(node parser.Node, fn func(parser.Node)) {
	if node == nil {
		return
	}
	fn(node)
	switch n := node.(type) {
	case *parser.FuncDecl:
		if n.WhenGuard != nil {
			walkNode(n.WhenGuard, fn)
		}
		for _, param := range n.Params {
			if param.Default != nil {
				walkNode(param.Default, fn)
			}
		}
		walkNode(n.Body, fn)
	case *parser.ExternDecl:
		// leaf node — no children to walk
	case *parser.ExtendDecl:
		for _, m := range n.Methods {
			walkNode(m, fn)
		}
	case *parser.ImportDecl:
		// leaf node — no children to walk
	case *parser.VarDecl:
		walkNode(n.Init, fn)
	case *parser.ClassDecl:
		for _, m := range n.Body {
			walkNode(m, fn)
		}
	case *parser.BlockStmt:
		for _, s := range n.Statements {
			walkNode(s, fn)
		}
	case *parser.ExprStmt:
		walkNode(n.Expr, fn)
	case *parser.ReturnStmt:
		if n.Value != nil {
			walkNode(n.Value, fn)
		}
	case *parser.BreakStmt:
		if n.Value != nil {
			walkNode(n.Value, fn)
		}
	case *parser.IfStmt:
		walkNode(n.Condition, fn)
		walkNode(n.Consequent, fn)
		if n.Alternate != nil {
			walkNode(n.Alternate, fn)
		}
	case *parser.ForStmt:
		walkNode(n.Iterable, fn)
		walkNode(n.Body, fn)
	case *parser.WhileStmt:
		walkNode(n.Condition, fn)
		walkNode(n.Body, fn)
	case *parser.LoopStmt:
		walkNode(n.Body, fn)
	case *parser.BinaryExpr:
		walkNode(n.Left, fn)
		walkNode(n.Right, fn)
	case *parser.UnaryExpr:
		walkNode(n.Operand, fn)
	case *parser.AssignExpr:
		walkNode(n.Left, fn)
		walkNode(n.Right, fn)
	case *parser.CallExpr:
		walkNode(n.Callee, fn)
		for _, arg := range n.Args {
			walkNode(arg, fn)
		}
	case *parser.MemberExpr:
		walkNode(n.Object, fn)
	case *parser.SafeNavExpr:
		walkNode(n.Object, fn)
	case *parser.ElvisExpr:
		walkNode(n.Left, fn)
		walkNode(n.Right, fn)
	case *parser.PipeExpr:
		walkNode(n.Left, fn)
		walkNode(n.Right, fn)
	case *parser.PipeQuestExpr:
		walkNode(n.Left, fn)
		walkNode(n.Right, fn)
	case *parser.IndexExpr:
		walkNode(n.Object, fn)
		walkNode(n.Index, fn)
	case *parser.RangeExpr:
		walkNode(n.From, fn)
		walkNode(n.To, fn)
	case *parser.TupleExpr:
		for _, e := range n.Elements {
			walkNode(e, fn)
		}
	case *parser.ListExpr:
		for _, e := range n.Elements {
			walkNode(e, fn)
		}
	case *parser.MapExpr:
		for _, e := range n.Entries {
			walkNode(e.Key, fn)
			walkNode(e.Value, fn)
		}
	case *parser.MatchExpr:
		walkNode(n.Subject, fn)
		for _, arm := range n.Arms {
			if arm.Guard != nil {
				walkNode(arm.Guard, fn)
			}
			walkNode(arm.Body, fn)
		}
	case *parser.ArrowFunc:
		walkNode(n.Body, fn)
	case *parser.OkExpr:
		walkNode(n.Value, fn)
	case *parser.ErrExpr:
		walkNode(n.Value, fn)
	case *parser.SomeExpr:
		walkNode(n.Value, fn)
	case *parser.RecordLiteral:
		for _, f := range n.Fields {
			walkNode(f.Value, fn)
		}
	case *parser.InterpolatedString:
		for _, part := range n.Parts {
			walkNode(part, fn)
		}
	}
}
