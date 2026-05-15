package parser

import "soyuz/internal/lexer"

func (p *Parser) parseTopLevel() Node {
	pub := false
	if p.check(lexer.PUB) {
		p.advance()
		pub = true
	}

	switch p.peek().Type {
	case lexer.WEAK, lexer.VAL, lexer.VAR, lexer.CONST:
		return p.parseVarDecl(pub)
	case lexer.FN:
		return p.parseFuncDecl(pub)
	case lexer.RECORD:
		return p.parseRecordDecl(pub)
	case lexer.CLASS:
		return p.parseClassDecl(pub)
	case lexer.INTERFACE:
		return p.parseInterfaceDecl(pub)
	case lexer.ENUM:
		return p.parseEnumDecl(pub)
	case lexer.IMPORT:
		if pub {
			p.errorf(p.peek().Position, "import não pode ser pub")
		}
		return p.parseImportDecl()
	default:
		if pub {
			p.errorf(p.peek().Position, "esperado declaração após pub")
		}
		expr := p.parseExpression(0)
		p.consume(lexer.SEMICOLON)
		return &ExprStmt{pos: expr.Pos(), Expr: expr}
	}
}

func (p *Parser) parseVarDecl(pub bool) *VarDecl {
	pos := p.peek().Position
	weak := p.consume(lexer.WEAK)
	kind := VarKind(p.advance().Lexeme)

	var name string
	var namePos lexer.Position
	var pattern Pattern
	if p.check(lexer.LPAREN) {
		pattern = p.parsePattern() // tuple destructuring: val (x, y) = expr
	} else {
		tok := p.expect(lexer.IDENT)
		name = tok.Lexeme
		namePos = tok.Position
	}

	var typeExpr TypeExpr
	if p.consume(lexer.COLON) {
		typeExpr = p.parseTypeExpr()
	}

	var init Node
	if p.consume(lexer.ASSIGN) {
		init = p.parseExpression(0)
	}
	p.consume(lexer.SEMICOLON)

	return &VarDecl{pos: pos, NamePos: namePos, Pub: pub, Weak: weak, Kind: kind, Name: name, Pattern: pattern, Type: typeExpr, Init: init}
}

func (p *Parser) parseFuncDecl(pub bool) *FuncDecl {
	pos := p.expect(lexer.FN).Position
	nameTok := p.expect(lexer.IDENT)
	name := nameTok.Lexeme
	namePos := nameTok.Position

	var generics []GenericParam
	if p.check(lexer.LBRACKET) {
		generics = p.parseGenericParams()
	}

	params := p.parseFuncParams()

	var returnType TypeExpr
	if p.consume(lexer.ARROW) {
		returnType = p.parseTypeExpr()
	}

	var body Node
	isExpr := false

	switch {
	case p.check(lexer.ASSIGN) || p.check(lexer.FAT_ARROW):
		p.advance()
		body = p.parseExpression(0)
		p.consume(lexer.SEMICOLON)
		isExpr = true
	case p.check(lexer.LBRACE):
		body = p.parseBlock()
	default:
		p.errorf(p.peek().Position, "esperado => ou { no corpo da função")
		p.synchronize()
	}

	return &FuncDecl{
		pos:        pos,
		NamePos:    namePos,
		Pub:        pub,
		Name:       name,
		Generics:   generics,
		Params:     params,
		ReturnType: returnType,
		Body:       body,
		IsExprBody: isExpr,
	}
}

func (p *Parser) parseGenericParams() []GenericParam {
	p.expect(lexer.LBRACKET)
	var params []GenericParam
	for !p.check(lexer.RBRACKET) && !p.check(lexer.EOF) {
		name := p.expect(lexer.IDENT).Lexeme
		var constraints []TypeExpr
		if p.consume(lexer.COLON) {
			constraints = append(constraints, p.parseTypeExpr())
			for p.consume(lexer.PLUS) {
				constraints = append(constraints, p.parseTypeExpr())
			}
		}
		params = append(params, GenericParam{Name: name, Constraints: constraints})
		if !p.check(lexer.RBRACKET) {
			p.consume(lexer.COMMA)
		}
	}
	p.expect(lexer.RBRACKET)
	return params
}

func (p *Parser) parseFuncParams() []FuncParam {
	p.expect(lexer.LPAREN)
	var params []FuncParam
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		params = append(params, p.parseFuncParam())
		if !p.check(lexer.RPAREN) {
			p.consume(lexer.COMMA)
		}
	}
	p.expect(lexer.RPAREN)
	return params
}

func (p *Parser) parseFuncParam() FuncParam {
	pos := p.peek().Position
	pat := p.parsePattern()
	var typeExpr TypeExpr
	if p.consume(lexer.COLON) {
		typeExpr = p.parseTypeExpr()
	}
	return FuncParam{Pos: pos, Pattern: pat, Type: typeExpr}
}

func (p *Parser) parseRecordDecl(pub bool) *RecordDecl {
	pos := p.expect(lexer.RECORD).Position
	name := p.expect(lexer.IDENT).Lexeme

	var generics []GenericParam
	if p.check(lexer.LBRACKET) {
		generics = p.parseGenericParams()
	}

	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var fields []RecordField
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		fpos := p.peek().Position
		fweak := p.consume(lexer.WEAK)
		fname := p.expect(lexer.IDENT).Lexeme
		p.expect(lexer.COLON)
		ftype := p.parseTypeExpr()
		fields = append(fields, RecordField{Pos: fpos, Weak: fweak, Name: fname, Type: ftype})
		p.consume(lexer.COMMA)
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)

	return &RecordDecl{pos: pos, Pub: pub, Name: name, Generics: generics, Fields: fields}
}

func (p *Parser) parseClassDecl(pub bool) *ClassDecl {
	pos := p.expect(lexer.CLASS).Position
	name := p.expect(lexer.IDENT).Lexeme

	var generics []GenericParam
	if p.check(lexer.LBRACKET) {
		generics = p.parseGenericParams()
	}

	var interfaces []TypeExpr
	if p.consume(lexer.COLON) {
		interfaces = append(interfaces, p.parseTypeExpr())
		for p.consume(lexer.COMMA) {
			interfaces = append(interfaces, p.parseTypeExpr())
		}
	}

	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var body []Node
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		memberPub := p.consume(lexer.PUB)
		memberWeak := p.check(lexer.WEAK) // Don't consume yet, parseVarDecl will
		switch p.peek().Type {
		case lexer.FN:
			if memberWeak {
				p.errorf(p.peek().Position, "fn não pode ser weak")
				p.advance() // consume weak to continue
			}
			body = append(body, p.parseFuncDecl(memberPub))
		case lexer.WEAK, lexer.VAL, lexer.VAR:
			body = append(body, p.parseVarDecl(memberPub))
		default:
			p.errorf(p.peek().Position, "esperado membro de class (fn, val, var), encontrado %s", p.peek().Type)
			p.synchronize()
		}
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)

	return &ClassDecl{pos: pos, Pub: pub, Name: name, Generics: generics, Interfaces: interfaces, Body: body}
}

func (p *Parser) parseInterfaceDecl(pub bool) *InterfaceDecl {
	pos := p.expect(lexer.INTERFACE).Position
	name := p.expect(lexer.IDENT).Lexeme

	var generics []GenericParam
	if p.check(lexer.LBRACKET) {
		generics = p.parseGenericParams()
	}

	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var methods []InterfaceMethod
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		methods = append(methods, p.parseInterfaceMethod())
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)

	return &InterfaceDecl{pos: pos, Pub: pub, Name: name, Generics: generics, Methods: methods}
}

func (p *Parser) parseInterfaceMethod() InterfaceMethod {
	pos := p.expect(lexer.FN).Position
	name := p.expect(lexer.IDENT).Lexeme
	params := p.parseFuncParams()
	var returnType TypeExpr
	if p.consume(lexer.ARROW) {
		returnType = p.parseTypeExpr()
	}
	p.consume(lexer.SEMICOLON)
	return InterfaceMethod{Pos: pos, Name: name, Params: params, ReturnType: returnType}
}

func (p *Parser) parseEnumDecl(pub bool) *EnumDecl {
	pos := p.expect(lexer.ENUM).Position
	name := p.expect(lexer.IDENT).Lexeme

	var generics []GenericParam
	if p.check(lexer.LBRACKET) {
		generics = p.parseGenericParams()
	}

	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var variants []EnumVariant
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		variants = append(variants, p.parseEnumVariant())
		p.consume(lexer.COMMA)
		p.skipSemicolons()
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)

	return &EnumDecl{pos: pos, Pub: pub, Name: name, Generics: generics, Variants: variants}
}

func (p *Parser) parseEnumVariant() EnumVariant {
	pos := p.peek().Position
	name := p.expect(lexer.IDENT).Lexeme
	var fields []EnumField

	if p.consume(lexer.LPAREN) {
		for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
			var fieldName string
			if p.check(lexer.IDENT) && p.peekN(1).Type == lexer.COLON {
				fieldName = p.advance().Lexeme
				p.advance() // colon
			}
			fieldType := p.parseTypeExpr()
			fields = append(fields, EnumField{Name: fieldName, Type: fieldType})
			if !p.check(lexer.RPAREN) {
				p.consume(lexer.COMMA)
			}
		}
		p.expect(lexer.RPAREN)
	}
	return EnumVariant{Pos: pos, Name: name, Fields: fields}
}

func (p *Parser) parseImportDecl() *ImportDecl {
	pos := p.expect(lexer.IMPORT).Position

	var path []string
	path = append(path, p.expect(lexer.IDENT).Lexeme)

	var names []ImportName
	wildcard := false

	for p.consume(lexer.DOT) {
		if p.check(lexer.LBRACE) {
			p.advance()
			for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
				n := p.expect(lexer.IDENT).Lexeme
				names = append(names, ImportName{Name: n})
				p.consume(lexer.COMMA)
			}
			p.expect(lexer.RBRACE)
			break
		}
		if p.check(lexer.ASTERISK) {
			wildcard = true
			p.advance()
			break
		}
		path = append(path, p.expect(lexer.IDENT).Lexeme)
	}
	p.consume(lexer.SEMICOLON)

	return &ImportDecl{pos: pos, Path: path, Names: names, Wildcard: wildcard}
}
