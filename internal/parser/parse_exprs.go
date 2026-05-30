package parser

import "soyuz/internal/lexer"

// bindingPower returns the left binding power of an infix/postfix token.
// Precedence (lowest → highest, following C conventions for bitwise):
//
//	PIPE(|>) < ASSIGN < ELVIS < OR(||) < PIPE_SINGLE(|) < CARET(^) < AND(&&) < AMPERSAND(&)
//	< EQUALS/NE < LT/GT/… < RANGE < SHL/SHR < PLUS/MINUS < MUL/DIV/MOD < DOT < CALL/INDEX
func bindingPower(t lexer.TokenType) int {
	switch t {
	case lexer.PIPE, lexer.PIPE_QUEST,
		lexer.ASYNC_PIPE, lexer.ASYNC_PIPE_QUEST:
		return 2
	case lexer.ASSIGN:
		return 4
	case lexer.ELVIS:
		return 6
	case lexer.OR:
		return 8
	case lexer.PIPE_SINGLE:
		return 10
	case lexer.CARET:
		return 12
	case lexer.AND:
		return 14
	case lexer.AMPERSAND:
		return 16
	case lexer.EQUALS, lexer.NOT_EQUALS:
		return 18
	case lexer.LT, lexer.GT, lexer.LTE, lexer.GTE:
		return 20
	case lexer.RANGE, lexer.RANGE_INCL:
		return 22
	case lexer.SHL, lexer.SHR:
		return 24
	case lexer.PLUS, lexer.MINUS:
		return 26
	case lexer.ASTERISK, lexer.SLASH, lexer.PERCENT:
		return 28
	case lexer.DOT, lexer.SAFE_NAV:
		return 30
	case lexer.LPAREN, lexer.LBRACKET:
		return 32
	default:
		return 0
	}
}

func (p *Parser) parseExpression(minBP int) Node {
	left := p.parsePrefix()
	for bindingPower(p.peek().Type) > minBP {
		left = p.parseInfix(left)
	}
	return left
}

func (p *Parser) parsePrefix() Node {
	pos := p.peek().Position

	switch p.peek().Type {
	case lexer.INT_LITERAL:
		tok := p.advance()
		return &IntLiteral{pos: tok.Position, Value: tok.Lexeme}

	case lexer.FLOAT_LITERAL:
		tok := p.advance()
		return &FloatLiteral{pos: tok.Position, Value: tok.Lexeme}

	case lexer.STRING_LITERAL:
		tok := p.advance()
		return &StringLiteral{pos: tok.Position, Value: tok.Lexeme}

	case lexer.CHAR_LITERAL:
		tok := p.advance()
		r := []rune(tok.Lexeme)
		var ch rune
		if len(r) > 0 {
			ch = r[0]
		}
		return &CharLiteral{pos: tok.Position, Value: ch}

	case lexer.STRING_PART, lexer.INTERP_START:
		return p.parseInterpolatedString()

	case lexer.TRUE:
		p.advance()
		return &BoolLiteral{pos: pos, Value: true}

	case lexer.FALSE:
		p.advance()
		return &BoolLiteral{pos: pos, Value: false}

	case lexer.NONE:
		p.advance()
		return &NoneLiteral{pos: pos}

	case lexer.SELF:
		p.advance()
		return &SelfExpr{pos: pos}

	case lexer.OK:
		p.advance()
		p.expect(lexer.LPAREN)
		val := p.parseExpression(0)
		p.expect(lexer.RPAREN)
		return &OkExpr{pos: pos, Value: val}

	case lexer.ERR:
		p.advance()
		p.expect(lexer.LPAREN)
		val := p.parseExpression(0)
		p.expect(lexer.RPAREN)
		return &ErrExpr{pos: pos, Value: val}

	case lexer.SOME:
		p.advance()
		p.expect(lexer.LPAREN)
		val := p.parseExpression(0)
		p.expect(lexer.RPAREN)
		return &SomeExpr{pos: pos, Value: val}

	case lexer.MINUS:
		p.advance()
		operand := p.parseExpression(28)
		return &UnaryExpr{pos: pos, Operator: "-", Operand: operand}

	case lexer.BANG:
		p.advance()
		operand := p.parseExpression(28)
		return &UnaryExpr{pos: pos, Operator: "!", Operand: operand}

	case lexer.TILDE:
		p.advance()
		operand := p.parseExpression(28)
		return &UnaryExpr{pos: pos, Operator: "~", Operand: operand}

	case lexer.LPAREN:
		return p.parseParenOrTuple()

	case lexer.LBRACKET:
		return p.parseListOrMap()

	case lexer.MATCH:
		return p.parseMatchExpr()

	case lexer.IF:
		return p.parseIf()

	case lexer.FOR:
		return p.parseFor()

	case lexer.WHILE:
		return p.parseWhile()

	case lexer.LOOP:
		return p.parseLoop()

	case lexer.TASK:
		p.advance()
		if p.peek().Type == lexer.FN {
			p.errorf(pos, "task não aceita lambda — extraia uma função nomeada")
		}
		inner := p.parseExpression(0)
		return &TaskExpr{pos: pos, Inner: inner}

	case lexer.SELECT:
		return p.parseSelectExpr()

	case lexer.FN:
		if p.peekN(1).Type == lexer.IDENT {
			p.errorf(pos, "declaração de função não é uma expressão — use fn(...) => expr para funções anônimas")
		}
		return p.parseAnonymousFunc()

	case lexer.IDENT:
		return p.parseIdentOrRecordLiteral()

	case lexer.UNDERSCORE:
		tok := p.advance()
		return &Identifier{pos: tok.Position, Name: "_"}

	default:
		p.errorf(pos, "token inesperado na expressão: %s (%q)", p.peek().Type, p.peek().Lexeme)
		tok := p.advance()
		return &Identifier{pos: tok.Position, Name: tok.Lexeme}
	}
}

func (p *Parser) parseInfix(left Node) Node {
	tok := p.peek()
	bp := bindingPower(tok.Type)

	switch tok.Type {
	case lexer.PLUS, lexer.MINUS, lexer.ASTERISK, lexer.SLASH, lexer.PERCENT,
		lexer.EQUALS, lexer.NOT_EQUALS,
		lexer.LT, lexer.GT, lexer.LTE, lexer.GTE,
		lexer.AND, lexer.OR,
		lexer.PIPE_SINGLE, lexer.AMPERSAND, lexer.CARET,
		lexer.SHL, lexer.SHR:
		p.advance()
		right := p.parseExpression(bp)
		return &BinaryExpr{pos: tok.Position, Operator: tok.Lexeme, Left: left, Right: right}

	case lexer.ASSIGN:
		p.advance()
		right := p.parseExpression(bp - 1)
		return &AssignExpr{pos: tok.Position, Left: left, Right: right}

	case lexer.PIPE:
		p.advance()
		right := p.parseExpression(bp)
		return &PipeExpr{pos: tok.Position, Left: left, Right: right}

	case lexer.PIPE_QUEST:
		p.advance()
		right := p.parseExpression(bp)
		return &PipeQuestExpr{pos: tok.Position, Left: left, Right: right}

	case lexer.ASYNC_PIPE:
		p.advance()
		right := p.parseExpression(bp)
		// Flatten into a single AsyncPipeExpr with all steps.
		if ap, ok := left.(*AsyncPipeExpr); ok {
			ap.Steps = append(ap.Steps, right)
			return ap
		}
		return &AsyncPipeExpr{pos: tok.Position, Steps: []Node{left, right}}

	case lexer.ASYNC_PIPE_QUEST:
		// M-17: store as a tagged step. Reuse AsyncPipeExpr steps but wrap the
		// step in a AsyncPipeQuestStep marker to distinguish error-propagating steps.
		// For now, parse identically to ASYNC_PIPE and let the checker/codegen
		// detect the step marker via the token type stored in the node.
		p.advance()
		right := p.parseExpression(bp)
		if ap, ok := left.(*AsyncPipeExpr); ok {
			ap.Steps = append(ap.Steps, &AsyncPipeQuestStep{pos: tok.Position, Step: right})
			return ap
		}
		return &AsyncPipeExpr{pos: tok.Position, Steps: []Node{left, &AsyncPipeQuestStep{pos: tok.Position, Step: right}}}

	case lexer.ELVIS:
		p.advance()
		right := p.parseExpression(bp)
		return &ElvisExpr{pos: tok.Position, Left: left, Right: right}

	case lexer.RANGE:
		p.advance()
		to := p.parseExpression(bp)
		return &RangeExpr{pos: tok.Position, From: left, To: to, Inclusive: false}

	case lexer.RANGE_INCL:
		p.advance()
		to := p.parseExpression(bp)
		return &RangeExpr{pos: tok.Position, From: left, To: to, Inclusive: true}

	case lexer.DOT:
		p.advance()
		if p.check(lexer.INT_LITERAL) {
			prop := p.advance().Lexeme
			return &MemberExpr{pos: tok.Position, Object: left, Property: prop}
		}
		prop := p.expectName().Lexeme
		return &MemberExpr{pos: tok.Position, Object: left, Property: prop}

	case lexer.SAFE_NAV:
		p.advance()
		prop := p.expectName().Lexeme
		return &SafeNavExpr{pos: tok.Position, Object: left, Property: prop}

	case lexer.LPAREN:
		p.advance()
		args := p.parseCallArgs()
		p.expect(lexer.RPAREN)
		return &CallExpr{pos: tok.Position, Callee: left, Args: args}

	case lexer.LBRACKET:
		p.advance()
		idx := p.parseExpression(0)
		p.expect(lexer.RBRACKET)
		return &IndexExpr{pos: tok.Position, Object: left, Index: idx}
	}
	return left
}

func (p *Parser) parseCallArgs() []Node {
	var args []Node
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		args = append(args, p.parseExpression(0))
		if !p.check(lexer.RPAREN) {
			p.consume(lexer.COMMA)
		}
	}
	return args
}

func (p *Parser) parseParenOrTuple() Node {
	pos := p.expect(lexer.LPAREN).Position
	if p.check(lexer.RPAREN) {
		p.advance()
		return &TupleExpr{pos: pos}
	}
	first := p.parseExpression(0)
	if p.consume(lexer.COMMA) {
		elements := []Node{first}
		for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
			elements = append(elements, p.parseExpression(0))
			if !p.check(lexer.RPAREN) {
				p.consume(lexer.COMMA)
			}
		}
		p.expect(lexer.RPAREN)
		return &TupleExpr{pos: pos, Elements: elements}
	}
	p.expect(lexer.RPAREN)
	return first
}

func (p *Parser) parseListOrMap() Node {
	pos := p.expect(lexer.LBRACKET).Position
	if p.check(lexer.RBRACKET) {
		p.advance()
		return &ListExpr{pos: pos}
	}
	first := p.parseExpression(0)
	if p.consume(lexer.COLON) {
		firstVal := p.parseExpression(0)
		entries := []MapEntry{{Key: first, Value: firstVal}}
		for p.consume(lexer.COMMA) {
			if p.check(lexer.RBRACKET) {
				break
			}
			key := p.parseExpression(0)
			p.expect(lexer.COLON)
			val := p.parseExpression(0)
			entries = append(entries, MapEntry{Key: key, Value: val})
		}
		p.expect(lexer.RBRACKET)
		return &MapExpr{pos: pos, Entries: entries}
	}
	elements := []Node{first}
	for p.consume(lexer.COMMA) {
		if p.check(lexer.RBRACKET) {
			break
		}
		elements = append(elements, p.parseExpression(0))
	}
	p.expect(lexer.RBRACKET)
	return &ListExpr{pos: pos, Elements: elements}
}

func (p *Parser) parseIdentOrRecordLiteral() Node {
	tok := p.advance()
	if isUppercase(tok.Lexeme) && p.check(lexer.LBRACE) {
		return p.parseRecordLiteralBody(tok.Position, tok.Lexeme)
	}
	return &Identifier{pos: tok.Position, Name: tok.Lexeme}
}

func (p *Parser) parseRecordLiteralBody(pos lexer.Position, name string) *RecordLiteral {
	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var fields []RecordLiteralField
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		fpos := p.peek().Position
		fname := p.expect(lexer.IDENT).Lexeme
		p.expect(lexer.COLON)
		fval := p.parseExpression(0)
		fields = append(fields, RecordLiteralField{Pos: fpos, Name: fname, Value: fval})
		p.consume(lexer.COMMA)
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	return &RecordLiteral{pos: pos, Name: name, Fields: fields}
}

func (p *Parser) parseMatchExpr() *MatchExpr {
	pos := p.advance().Position
	subject := p.parseExpression(0)
	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var arms []MatchArm
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		arms = append(arms, p.parseMatchArm())
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	return &MatchExpr{pos: pos, Subject: subject, Arms: arms}
}

func (p *Parser) parseMatchArm() MatchArm {
	pos := p.peek().Position
	pat := p.parsePattern()

	var guard Node
	if p.check(lexer.WHEN) || p.check(lexer.IF) {
		p.advance()
		guard = p.parseExpression(0)
	}
	p.expect(lexer.FAT_ARROW)

	var body Node
	body = p.parseArmBody()
	if _, isBlock := body.(*BlockStmt); !isBlock {
		// Accept either a comma or a semicolon (from auto-insertion at newline) as arm separator.
		if !p.consume(lexer.COMMA) {
			p.consume(lexer.SEMICOLON)
		}
	}
	return MatchArm{Pos: pos, Pattern: pat, Guard: guard, Body: body}
}

func (p *Parser) parseAnonymousFunc() *ArrowFunc {
	pos := p.expect(lexer.FN).Position
	params := p.parseFuncParams()

	var returnType TypeExpr
	if p.consume(lexer.ARROW) {
		returnType = p.parseTypeExpr()
	}
	p.expect(lexer.FAT_ARROW)

	var body Node
	if p.check(lexer.LBRACE) {
		body = p.parseBlock()
	} else {
		body = p.parseExpression(0)
	}
	return &ArrowFunc{pos: pos, Params: params, ReturnType: returnType, Body: body}
}

func (p *Parser) parseInterpolatedString() Node {
	pos := p.peek().Position
	interp := &InterpolatedString{pos: pos}

	for {
		switch p.peek().Type {
		case lexer.STRING_PART:
			tok := p.advance()
			interp.Parts = append(interp.Parts, &StringLiteral{pos: tok.Position, Value: tok.Lexeme})
		case lexer.INTERP_START:
			p.advance()
			expr := p.parseExpression(0)
			interp.Parts = append(interp.Parts, expr)
			p.expect(lexer.INTERP_END)
		case lexer.STRING_LITERAL:
			tok := p.advance()
			if tok.Lexeme != "" {
				interp.Parts = append(interp.Parts, &StringLiteral{pos: tok.Position, Value: tok.Lexeme})
			}
			if len(interp.Parts) == 1 {
				if sl, ok := interp.Parts[0].(*StringLiteral); ok {
					return sl
				}
			}
			return interp
		default:
			return interp
		}
	}
}
