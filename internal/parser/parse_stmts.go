package parser

import "soyuz/internal/lexer"

func (p *Parser) parseStatement() Node {
	switch p.peek().Type {
	case lexer.VAL, lexer.VAR, lexer.CONST:
		return p.parseVarDecl(false)
	case lexer.FN:
		return p.parseFuncDecl(false)
	case lexer.RECORD:
		return p.parseRecordDecl(false)
	case lexer.ENUM:
		return p.parseEnumDecl(false)
	case lexer.RETURN:
		return p.parseReturn()
	case lexer.BREAK:
		return p.parseBreak()
	case lexer.CONTINUE:
		pos := p.peek().Position
		p.advance()
		p.consume(lexer.SEMICOLON)
		return &ContinueStmt{pos: pos}
	case lexer.IF:
		return p.parseIf()
	case lexer.FOR:
		return p.parseFor()
	case lexer.WHILE:
		return p.parseWhile()
	case lexer.LOOP:
		return p.parseLoop()
	case lexer.LBRACE:
		return p.parseBlock()
	default:
		expr := p.parseExpression(0)
		p.consume(lexer.SEMICOLON)
		return &ExprStmt{pos: expr.Pos(), Expr: expr}
	}
}

func (p *Parser) parseBlock() *BlockStmt {
	pos := p.expect(lexer.LBRACE).Position
	p.skipSemicolons()

	var stmts []Node
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		if stmt := p.parseStatement(); stmt != nil {
			stmts = append(stmts, stmt)
		}
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)

	return &BlockStmt{pos: pos, Statements: stmts}
}

func (p *Parser) parseReturn() *ReturnStmt {
	pos := p.advance().Position
	var value Node
	if !p.checkAny(lexer.SEMICOLON, lexer.RBRACE, lexer.EOF) {
		value = p.parseExpression(0)
	}
	p.consume(lexer.SEMICOLON)
	return &ReturnStmt{pos: pos, Value: value}
}

func (p *Parser) parseBreak() *BreakStmt {
	pos := p.advance().Position
	var value Node
	if !p.checkAny(lexer.SEMICOLON, lexer.RBRACE, lexer.EOF) {
		value = p.parseExpression(0)
	}
	p.consume(lexer.SEMICOLON)
	return &BreakStmt{pos: pos, Value: value}
}

func (p *Parser) parseIf() *IfStmt {
	pos := p.advance().Position
	condition := p.parseExpression(0)
	consequent := p.parseBlock()

	node := &IfStmt{pos: pos, Condition: condition, Consequent: consequent}
	if p.consume(lexer.ELSE) {
		if p.check(lexer.IF) {
			node.Alternate = p.parseIf()
		} else {
			node.Alternate = p.parseBlock()
		}
	}
	return node
}

func (p *Parser) parseFor() *ForStmt {
	pos := p.advance().Position
	binding := p.expect(lexer.IDENT).Lexeme
	p.expect(lexer.IN)
	iterable := p.parseExpression(0)
	body := p.parseBlock()
	return &ForStmt{pos: pos, Binding: binding, Iterable: iterable, Body: body}
}

func (p *Parser) parseWhile() *WhileStmt {
	pos := p.advance().Position
	condition := p.parseExpression(0)
	body := p.parseBlock()
	return &WhileStmt{pos: pos, Condition: condition, Body: body}
}

func (p *Parser) parseLoop() *LoopStmt {
	pos := p.advance().Position
	body := p.parseBlock()
	return &LoopStmt{pos: pos, Body: body}
}
