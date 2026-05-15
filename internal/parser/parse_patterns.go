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
			if p.check(lexer.LPAREN) {
				args := p.parseConstructorPatternArgs()
				return &ConstructorPattern{pos: pos, Name: name, Args: args}
			}
			if p.check(lexer.LBRACE) {
				return p.parseRecordPatternBody(pos, name)
			}
			return &ConstructorPattern{pos: pos, Name: name}
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
		p.errorf(pos, "padrão inválido: %s (%q)", p.peek().Type, p.peek().Lexeme)
		p.advance()
		return &WildcardPattern{pos: pos}
	}
}

func (p *Parser) parseConstructorPatternArgs() []Pattern {
	if !p.consume(lexer.LPAREN) {
		return nil
	}
	var args []Pattern
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		args = append(args, p.parsePattern())
		if !p.check(lexer.RPAREN) {
			p.consume(lexer.COMMA)
		}
	}
	p.expect(lexer.RPAREN)
	return args
}

func (p *Parser) parseRecordPatternBody(pos lexer.Position, name string) *RecordPattern {
	p.expect(lexer.LBRACE)
	var fields []RecordPatternField
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		fname := p.expect(lexer.IDENT).Lexeme
		var fpat Pattern
		if p.consume(lexer.COLON) {
			fpat = p.parsePattern()
		}
		fields = append(fields, RecordPatternField{Name: fname, Pattern: fpat})
		if !p.check(lexer.RBRACE) {
			p.consume(lexer.COMMA)
		}
	}
	p.expect(lexer.RBRACE)
	return &RecordPattern{pos: pos, Name: name, Fields: fields}
}
