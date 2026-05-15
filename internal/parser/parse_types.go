package parser

import "soyuz/internal/lexer"

func (p *Parser) parseTypeExpr() TypeExpr {
	pos := p.peek().Position
	var t TypeExpr

	switch p.peek().Type {
	case lexer.INT_TYPE:
		p.advance()
		t = &NamedType{pos: pos, Name: "Int"}
	case lexer.FLOAT_TYPE:
		p.advance()
		t = &NamedType{pos: pos, Name: "Float"}
	case lexer.BOOL_TYPE:
		p.advance()
		t = &NamedType{pos: pos, Name: "Bool"}
	case lexer.STRING_TYPE:
		p.advance()
		t = &NamedType{pos: pos, Name: "String"}
	case lexer.UNIT_TYPE:
		p.advance()
		t = &NamedType{pos: pos, Name: "Unit"}

	case lexer.IDENT:
		name := p.advance().Lexeme
		if p.check(lexer.LBRACKET) {
			p.advance()
			var params []TypeExpr
			for !p.check(lexer.RBRACKET) && !p.check(lexer.EOF) {
				params = append(params, p.parseTypeExpr())
				if !p.check(lexer.RBRACKET) {
					p.consume(lexer.COMMA)
				}
			}
			p.expect(lexer.RBRACKET)
			t = &GenericType{pos: pos, Name: name, Params: params}
		} else {
			t = &NamedType{pos: pos, Name: name}
		}

	case lexer.LPAREN:
		p.advance()
		var types []TypeExpr
		for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
			types = append(types, p.parseTypeExpr())
			if !p.check(lexer.RPAREN) {
				p.consume(lexer.COMMA)
			}
		}
		p.expect(lexer.RPAREN)
		if p.consume(lexer.ARROW) {
			ret := p.parseTypeExpr()
			t = &FuncType{pos: pos, ParamTypes: types, ReturnType: ret}
		} else {
			t = &TupleType{pos: pos, Elements: types}
		}

	default:
		p.errorf(pos, "tipo inválido: %s (%q)", p.peek().Type, p.peek().Lexeme)
		p.advance() // evita loop infinito: sempre avança em caso de erro
		t = &NamedType{pos: pos, Name: "Unknown"}
	}

	// Optional suffix T?
	if p.check(lexer.QUESTION) {
		p.advance()
		t = &OptionalType{pos: pos, Inner: t}
	}
	return t
}
