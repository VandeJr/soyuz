package parser

import (
	"strings"

	"soyuz/internal/lexer"
)

func (p *Parser) parseTopLevel() Node {
	pub := false
	if p.check(lexer.PUB) {
		p.advance()
		pub = true
	}

	switch p.peek().Type {
	case lexer.WEAK, lexer.VAL, lexer.VAR:
		vd := p.parseVarDecl(pub)
		if vd.Pattern == nil && vd.Name != "" {
			if _, ok := vd.Init.(*ArrowFunc); ok {
				p.errorf(vd.Pos(), "funções nomeadas devem usar 'fn %s(...)', não 'val %s = fn(...)'", vd.Name, vd.Name)
			}
		}
		return vd
	case lexer.FN:
		return p.parseFuncDecl(pub)
	case lexer.EXTERN:
		return p.parseExternDecl(pub)
	case lexer.RECORD:
		return p.parseRecordDecl(pub)
	case lexer.CLASS:
		return p.parseClassDecl(pub)
	case lexer.INTERFACE:
		return p.parseInterfaceDecl(pub)
	case lexer.ENUM:
		return p.parseEnumDecl(pub)
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

	var whenGuard Node
	if p.consume(lexer.WHEN) {
		// Stop before '=' (ASSIGN has bindingPower=4).
		whenGuard = p.parseExpression(bindingPower(lexer.ASSIGN))
	}

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
		WhenGuard:  whenGuard,
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
	var defaultExpr Node
	if p.consume(lexer.ASSIGN) {
		defaultExpr = p.parseExpression(0)
	}
	return FuncParam{Pos: pos, Pattern: pat, Type: typeExpr, Default: defaultExpr}
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
	name := p.expectName().Lexeme
	var fields []EnumField

	if p.consume(lexer.LPAREN) {
		for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
			var fieldName string
			if p.checkName() && p.peekN(1).Type == lexer.COLON {
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
	} else if p.consume(lexer.LBRACE) {
		for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
			var fieldName string
			if p.checkName() && p.peekN(1).Type == lexer.COLON {
				fieldName = p.advance().Lexeme
				p.advance() // colon
			}
			fieldType := p.parseTypeExpr()
			fields = append(fields, EnumField{Name: fieldName, Type: fieldType})
			if !p.check(lexer.RBRACE) {
				p.consume(lexer.COMMA)
			}
		}
		p.expect(lexer.RBRACE)
	}
	return EnumVariant{Pos: pos, Name: name, Fields: fields}
}

func (p *Parser) parseExternDecl(pub bool) *ExternDecl {
	pos := p.expect(lexer.EXTERN).Position
	p.expect(lexer.FN)
	nameTok := p.expect(lexer.IDENT)
	params := p.parseFuncParams()
	var returnType TypeExpr
	if p.consume(lexer.ARROW) {
		returnType = p.parseTypeExpr()
	}
	p.consume(lexer.SEMICOLON)
	return &ExternDecl{pos: pos, Pub: pub, Name: nameTok.Lexeme, Params: params, ReturnType: returnType}
}

func (p *Parser) parseImportBlock() []Node {
	pos := p.expect(lexer.IMPORT).Position

	// Bloco: import ( ... )
	if p.check(lexer.LPAREN) {
		return p.parseImportParenBlock(pos)
	}

	// Inline: import path.{ names }  ou  import path
	segments, isStdlib, ok := p.parseImportPathSegments()
	if !ok {
		p.synchronize()
		return nil
	}

	var names []ImportName
	p.consume(lexer.DOT) // math.{ names } ou @soyuz.fs.{ readFile }
	if p.check(lexer.LBRACE) {
		names = p.parseImportNames()
	}

	p.consume(lexer.SEMICOLON)
	return []Node{p.makeImportDeclFromSegments(pos, segments, isStdlib, names)}
}

func (p *Parser) parseImportParenBlock(pos lexer.Position) []Node {
	p.expect(lexer.LPAREN)

	var decls []Node
	for !p.check(lexer.RPAREN) && !p.check(lexer.EOF) {
		if spec := p.parseImportSpec(pos); spec != nil {
			decls = append(decls, spec)
		} else {
			p.advance() // evita loop infinito em erro de sintaxe
		}
		p.consume(lexer.SEMICOLON)
		p.skipSemicolons()
	}
	if p.check(lexer.RPAREN) {
		p.advance()
	} else {
		p.errorf(p.peek().Position, "esperado ')' no bloco import")
	}
	p.consume(lexer.SEMICOLON)
	return decls
}

func (p *Parser) parseImportPathSegments() ([]string, bool, bool) {
	var segments []string
	isStdlib := false

	if p.check(lexer.AT) {
		p.advance()
		tok := p.peek()
		if tok.Type != lexer.IDENT || tok.Lexeme != "soyuz" {
			p.errorf(tok.Position, "esperado 'soyuz' após '@' em import stdlib")
			return nil, false, false
		}
		p.advance()
		isStdlib = true
	} else if p.check(lexer.IDENT) {
		segments = append(segments, p.advance().Lexeme)
	} else {
		p.errorf(p.peek().Position, "esperado caminho de módulo após import")
		return nil, false, false
	}

	for {
		if !p.check(lexer.DOT) && !p.check(lexer.SLASH) {
			break
		}
		// math.{ names } — o ponto antes de { encerra o caminho, não é segmento
		if p.check(lexer.DOT) && p.peekN(1).Type == lexer.LBRACE {
			break
		}
		p.advance()
		if !p.check(lexer.IDENT) {
			p.errorf(p.peek().Position, "esperado segmento de módulo após '.' ou '/'")
			return nil, false, false
		}
		segments = append(segments, p.advance().Lexeme)
	}

	return segments, isStdlib, true
}

func (p *Parser) parseImportNames() []ImportName {
	p.expect(lexer.LBRACE)
	var names []ImportName
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		tok := p.peek()
		if tok.Type != lexer.IDENT {
			p.errorf(tok.Position, "esperado identificador na lista de import")
			p.advance()
			continue
		}
		names = append(names, ImportName{Name: p.advance().Lexeme})
		p.consume(lexer.COMMA)
	}
	p.expect(lexer.RBRACE)
	return names
}

func (p *Parser) parseImportSpec(blockPos lexer.Position) *ImportDecl {
	if p.check(lexer.LBRACE) {
		names := p.parseImportNames()
		if !p.check(lexer.IDENT) || p.peek().Lexeme != "from" {
			p.errorf(p.peek().Position, "esperado 'from' após nomes importados")
			return nil
		}
		p.advance()
		if !p.check(lexer.STRING_LITERAL) {
			p.errorf(p.peek().Position, "esperado string literal após 'from'")
			return nil
		}
		path := p.advance().Lexeme
		return p.makeImportDecl(blockPos, path, names)
	}

	if p.check(lexer.STRING_LITERAL) {
		path := p.advance().Lexeme
		return p.makeImportDecl(blockPos, path, nil)
	}

	p.errorf(p.peek().Position, "esperado {names} from \"path\" ou \"path\" no import")
	return nil
}

func (p *Parser) makeImportDecl(pos lexer.Position, path string, names []ImportName) *ImportDecl {
	isStdlib := strings.HasPrefix(path, "@soyuz/")
	return p.makeImportDeclFromSegments(pos, pathSegmentsFromPath(path), isStdlib, names)
}

func (p *Parser) makeImportDeclFromSegments(pos lexer.Position, segments []string, isStdlib bool, names []ImportName) *ImportDecl {
	path := strings.Join(segments, "/")
	if isStdlib {
		path = "@soyuz/" + path
	}
	namespace := ""
	if len(segments) > 0 {
		namespace = segments[len(segments)-1]
	}
	return &ImportDecl{
		pos:       pos,
		Path:      path,
		Names:     names,
		Namespace: namespace,
		IsStdlib:  isStdlib,
	}
}

func pathSegmentsFromPath(path string) []string {
	p := strings.TrimPrefix(path, "@soyuz/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
