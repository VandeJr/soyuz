package parser

import "soyuz/internal/lexer"

func (p *Parser) parseStatement() Node {
	switch p.peek().Type {
	case lexer.VAL, lexer.VAR:
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
	case lexer.SELECT:
		return p.parseSelectExpr()
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

func (p *Parser) parseFor() Node {
	pos := p.advance().Position // consume FOR

	// Detect `for task binding in iterable { body }`
	if p.check(lexer.TASK) {
		p.advance() // consume TASK
		binding := p.expect(lexer.IDENT).Lexeme
		p.expect(lexer.IN)
		iterable := p.parseExpression(0)
		body := p.parseBlock()
		return &ForTaskStmt{pos: pos, Binding: binding, Iterable: iterable, Body: body}
	}

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

// parseSelectExpr parses:
//   select {
//       msg = ch.recv() => body
//       default         => body
//   }
func (p *Parser) parseSelectExpr() *SelectExpr {
	pos := p.expect(lexer.SELECT).Position
	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var arms []SelectArm
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		arm := p.parseSelectArm()
		arms = append(arms, arm)
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)
	return &SelectExpr{pos: pos, Arms: arms}
}

// parseSelectArm handles one arm inside a select block:
//   binding = ch.recv() => body   (recv arm with binding)
//   ch.recv() => body              (recv arm without binding)
//   default   => body              (default arm)
func (p *Parser) parseSelectArm() SelectArm {
	pos := p.peek().Position

	// default arm: identifier "default" followed by =>
	if p.check(lexer.IDENT) && p.peek().Lexeme == "default" {
		p.advance()
		p.expect(lexer.FAT_ARROW)
		body := p.parseArmBody()
		return SelectArm{Pos: pos, IsDefault: true, Body: body}
	}

	// Recv arm with binding: IDENT ASSIGN ch.recv() => body
	if p.check(lexer.IDENT) && p.peekN(1).Type == lexer.ASSIGN {
		binding := p.advance().Lexeme
		p.advance() // consume =
		chanExpr := p.parseExpression(0)
		p.expect(lexer.FAT_ARROW)
		body := p.parseArmBody()
		return SelectArm{Pos: pos, Binding: binding, Chan: chanExpr, Body: body}
	}

	// Recv arm without binding: ch.recv() => body
	chanExpr := p.parseExpression(0)
	p.expect(lexer.FAT_ARROW)
	body := p.parseArmBody()
	return SelectArm{Pos: pos, Chan: chanExpr, Body: body}
}

// parseArmBody parses either a block { ... } or a single expression.
func (p *Parser) parseArmBody() Node {
	if p.check(lexer.LBRACE) {
		return p.parseBlock()
	}
	expr := p.parseExpression(0)
	p.consume(lexer.SEMICOLON)
	return expr
}
