package parser

import "soyuz/internal/lexer"

func (p *Parser) parsePattern() Pattern {
	pos := p.peek().Position

	switch p.peek().Type {
	case lexer.UNDERSCORE:
		p.advance()
		return &WildcardPattern{pos: pos}

	case lexer.SELF:
		p.advance()
		return &BindingPattern{pos: pos, Name: "self"}

	case lexer.INT_LITERAL:
		tok := p.advance()
		lit := &IntLiteral{pos: tok.Position, Value: tok.Lexeme}
		if p.checkAny(lexer.RANGE, lexer.RANGE_INCL) {
			inclusive := p.advance().Type == lexer.RANGE_INCL
			to := p.parseExpression(0)
			return &RangePattern{pos: pos, From: lit, To: to, Inclusive: inclusive}
		}
		return &LiteralPattern{pos: pos, Value: lit}

	case lexer.MINUS:
		p.advance()
		switch p.peek().Type {
		case lexer.INT_LITERAL:
			tok := p.advance()
			return &LiteralPattern{pos: pos, Value: &IntLiteral{pos: pos, Value: "-" + tok.Lexeme}}
		case lexer.FLOAT_LITERAL:
			tok := p.advance()
			return &LiteralPattern{pos: pos, Value: &FloatLiteral{pos: pos, Value: "-" + tok.Lexeme}}
		default:
			p.errorf(pos, "esperado número após - em padrão")
			return &WildcardPattern{pos: pos}
		}

	case lexer.FLOAT_LITERAL:
		tok := p.advance()
		return &LiteralPattern{pos: pos, Value: &FloatLiteral{pos: tok.Position, Value: tok.Lexeme}}

	case lexer.STRING_LITERAL:
		tok := p.advance()
		return &LiteralPattern{pos: pos, Value: &StringLiteral{pos: tok.Position, Value: tok.Lexeme}}

	case lexer.CHAR_LITERAL:
		tok := p.advance()
		runes := []rune(tok.Lexeme)
		var r rune
		if len(runes) > 0 {
			r = runes[0]
		}

		return &LiteralPattern{
			pos: pos,
			Value: &CharLiteral{
				pos:   tok.Position,
				Value: r,
			},
		}

	case lexer.TRUE:
		p.advance()
		return &LiteralPattern{pos: pos, Value: &BoolLiteral{pos: pos, Value: true}}

	case lexer.FALSE:
		p.advance()
		return &LiteralPattern{pos: pos, Value: &BoolLiteral{pos: pos, Value: false}}

	case lexer.NONE:
		p.advance()
		return &ConstructorPattern{pos: pos, Name: "None"}

	case lexer.OK, lexer.ERR, lexer.SOME:
		name := p.advance().Lexeme
		args := p.parseConstructorPatternArgs()
		return &ConstructorPattern{pos: pos, Name: name, Args: args}

	case lexer.IDENT:
		name := p.advance().Lexeme
		if isUppercase(name) {
			return p.parseNamedPattern(pos, name)
		}
		return &BindingPattern{pos: pos, Name: name}

	case lexer.LPAREN:
		p.advance()
		var elements []Pattern
		for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
			elements = append(elements, p.parsePattern())
			if !p.check(lexer.RPAREN) {
				p.consume(lexer.COMMA)
			}
		}
		p.expect(lexer.RPAREN)
		return &TuplePattern{pos: pos, Elements: elements}

	default:
		if p.isNameLikeKeyword(p.peek().Type) {
			name := p.advance().Lexeme
			return p.parseNamedPattern(pos, name)
		}
		p.errorf(pos, "padrão inválido: %s (%q)", p.peek().Type, p.peek().Lexeme)
		p.advance()
		return &WildcardPattern{pos: pos}
	}
}

func (p *Parser) parseNamedPattern(pos lexer.Position, name string) Pattern {
	if p.check(lexer.LPAREN) {
		args := p.parseConstructorPatternArgs()
		return &ConstructorPattern{pos: pos, Name: name, Args: args}
	}
	if p.check(lexer.LBRACE) {
		return p.parseRecordPatternBody(pos, name)
	}
	return &ConstructorPattern{pos: pos, Name: name}
}

func (p *Parser) parseConstructorPatternArgs() []Pattern {
	if !p.consume(lexer.LPAREN) {
		return nil
	}
	var args []Pattern
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		before := p.pos
		args = append(args, p.parsePattern())
		if !p.check(lexer.RPAREN) {
			p.consume(lexer.COMMA)
		}
		if !p.bumpOrBail(before) {
			break
		}
	}
	p.expect(lexer.RPAREN)
	return args
}

func (p *Parser) parseRecordPatternBody(pos lexer.Position, name string) *RecordPattern {
	p.expect(lexer.LBRACE)
	var fields []RecordPatternField
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		before := p.pos
		if !p.check(lexer.IDENT) {
			p.errorf(p.peek().Position, "esperado identificador de campo em padrão record")
			break
		}
		fname := p.advance().Lexeme
		var fpat Pattern
		if p.consume(lexer.COLON) {
			fpat = p.parsePattern()
		}
		fields = append(fields, RecordPatternField{Name: fname, Pattern: fpat})
		if p.check(lexer.RBRACE) {
			break
		}
		p.consume(lexer.COMMA)
		if !p.bumpOrBail(before) {
			break
		}
	}
	p.expect(lexer.RBRACE)
	return &RecordPattern{pos: pos, Name: name, Fields: fields}
}
