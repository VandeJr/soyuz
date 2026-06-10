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
	case lexer.VAL, lexer.VAR:
		vd := p.parseVarDecl(pub)
		if vd.Pattern == nil && vd.Name != "" {
			if _, ok := vd.Init.(*ArrowFunc); ok {
				p.errorf(vd.Pos(), "funções nomeadas devem usar 'fn %s(...)', não 'val %s = fn(...)'", vd.Name, vd.Name)
			}
		}
		return vd
	case lexer.FN:
		return p.parseFuncDecl(pub)
	case lexer.TEST:
		return p.parseTestFuncDecl()
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
	case lexer.EXTEND:
		return p.parseExtendDecl()
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
	kind := VarKind(p.advance().Lexeme)

	var name string
	var namePos lexer.Position
	var pattern Pattern
	if p.check(lexer.LPAREN) {
		pattern = p.parsePattern() // tuple destructuring: val (x, y) = expr
	} else if p.check(lexer.UNDERSCORE) {
		// val _ = expr — blank identifier (silencia must-use warnings, mas runtime ainda panica no drop)
		tok := p.advance()
		name = "_"
		namePos = tok.Position
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

	return &VarDecl{pos: pos, NamePos: namePos, Pub: pub, Kind: kind, Name: name, Pattern: pattern, Type: typeExpr, Init: init}
}

func (p *Parser) parseTestFuncDecl() *FuncDecl {
	p.expect(lexer.TEST)
	ignore := false
	// 'ignore' is not a keyword — detect it as the identifier "ignore" in this context.
	if p.check(lexer.IDENT) && p.peek().Lexeme == "ignore" {
		p.advance()
		ignore = true
	}
	if !p.check(lexer.FN) {
		p.errorf(p.peek().Position, "esperado 'fn' após 'test'")
		p.synchronize()
		return nil
	}
	fd := p.parseFuncDecl(false)
	fd.IsTest = true
	fd.IsIgnore = ignore
	return fd
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
		fname := p.expect(lexer.IDENT).Lexeme
		p.expect(lexer.COLON)
		ftype := p.parseTypeExpr()
		fields = append(fields, RecordField{Pos: fpos, Name: fname, Type: ftype})
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
		switch p.peek().Type {
		case lexer.FN:
			body = append(body, p.parseFuncDecl(memberPub))
		case lexer.VAL, lexer.VAR:
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
	pub := p.consume(lexer.PUB)
	pos := p.expect(lexer.FN).Position
	name := p.expect(lexer.IDENT).Lexeme
	params := p.parseFuncParams()
	var returnType TypeExpr
	if p.consume(lexer.ARROW) {
		returnType = p.parseTypeExpr()
	}
	p.consume(lexer.SEMICOLON)
	return InterfaceMethod{Pos: pos, Pub: pub, Name: name, Params: params, ReturnType: returnType}
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
		before := p.pos
		variants = append(variants, p.parseEnumVariant())
		if p.pos == before {
			p.synchronize()
			continue
		}
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

func (p *Parser) parseExtendDecl() *ExtendDecl {
	pos := p.expect(lexer.EXTEND).Position
	typeName := p.parseExtendTypeName()
	p.expect(lexer.LBRACE)
	p.skipSemicolons()

	var methods []*FuncDecl
	for !p.check(lexer.RBRACE) && !p.check(lexer.EOF) {
		before := p.pos
		memberPub := p.consume(lexer.PUB)
		if !p.check(lexer.FN) {
			p.errorf(p.peek().Position, "esperado fn em bloco extend")
			p.recoverDecl()
			if !p.bumpOrBail(before) {
				break
			}
			continue
		}
		fd := p.parseFuncDecl(memberPub)
		methods = append(methods, fd)
		p.skipSemicolons()
		if !p.bumpOrBail(before) {
			break
		}
	}
	p.expect(lexer.RBRACE)
	p.consume(lexer.SEMICOLON)
	return &ExtendDecl{pos: pos, TypeName: typeName, Methods: methods}
}

func (p *Parser) parseExtendTypeName() string {
	switch p.peek().Type {
	case lexer.STRING_TYPE:
		p.advance()
		return "String"
	case lexer.INT_TYPE:
		p.advance()
		return "Int"
	case lexer.FLOAT_TYPE:
		p.advance()
		return "Float"
	case lexer.BOOL_TYPE:
		p.advance()
		return "Bool"
	case lexer.CHAR_TYPE:
		p.advance()
		return "Char"
	case lexer.IDENT:
		return p.advance().Lexeme
	default:
		p.errorf(p.peek().Position, "esperado nome de tipo após extend")
		return ""
	}
}

func (p *Parser) parseImportBlock() []Node {
	pos := p.expect(lexer.IMPORT).Position

	// Bloco: import ( ... )
	if p.check(lexer.LPAREN) {
		return p.parseImportParenBlock(pos)
	}

	// Inline: import path.{ names }  ou  import path
	var path string
	var ok bool
	if p.check(lexer.AT) || p.check(lexer.DOT) || p.check(lexer.STRING_LITERAL) {
		path, ok = p.parseImportPath()
	} else {
		var segments []string
		var isStdlib bool
		segments, isStdlib, ok = p.parseImportPathSegments()
		if ok {
			decl := p.makeImportDeclFromSegments(pos, segments, isStdlib, nil)
			if p.consume(lexer.DOT) && p.check(lexer.LBRACE) {
				decl.Names = p.parseImportNames()
			}
			p.consume(lexer.SEMICOLON)
			return []Node{decl}
		}
	}
	if !ok {
		p.synchronize()
		return nil
	}

	var names []ImportName
	if p.consume(lexer.DOT) && p.check(lexer.LBRACE) {
		names = p.parseImportNames()
	}

	p.consume(lexer.SEMICOLON)
	return []Node{p.makeImportDeclFromPath(pos, path, names)}
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
		if p.check(lexer.SLASH) {
			p.advance()
			segs, ok := p.parsePathSegmentsAfterSlash()
			if !ok {
				return nil, false, false
			}
			return segs, false, true // project root @/ — PathKind set later
		}
		if p.check(lexer.IDENT) {
			ident := p.advance().Lexeme
			if ident == "soyuz" {
				isStdlib = true
				if p.check(lexer.SLASH) || p.check(lexer.DOT) {
					segs, ok := p.parsePathSegmentsAfterDotOrSlash()
					return segs, isStdlib, ok
				}
				return nil, isStdlib, true
			}
			segments = append(segments, ident)
			segs, ok := p.parsePathSegmentsAfterDotOrSlash()
			return append(segments, segs...), false, ok
		}
		p.errorf(p.peek().Position, "esperado caminho após '@'")
		return nil, false, false
	}

	if p.check(lexer.DOT) {
		return p.parseRelativePathSegments()
	}

	if p.check(lexer.IDENT) {
		segments = append(segments, p.advance().Lexeme)
	} else {
		p.errorf(p.peek().Position, "esperado caminho de módulo após import")
		return nil, false, false
	}

	for {
		if !p.check(lexer.DOT) && !p.check(lexer.SLASH) {
			break
		}
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

func (p *Parser) parseRelativePathSegments() ([]string, bool, bool) {
	var parts []string
	for p.check(lexer.DOT) {
		p.advance()
		if p.check(lexer.DOT) && p.peekN(1).Type == lexer.DOT {
			p.advance()
			parts = append(parts, "..")
		} else {
			parts = append(parts, ".")
		}
		if p.check(lexer.SLASH) {
			p.advance()
		} else if p.check(lexer.IDENT) {
			// ./foo without slash
		} else {
			break
		}
		if p.check(lexer.IDENT) {
			parts = append(parts, p.advance().Lexeme)
		}
		for p.check(lexer.SLASH) {
			p.advance()
			if p.check(lexer.IDENT) {
				parts = append(parts, p.advance().Lexeme)
			}
		}
		if p.check(lexer.DOT) && p.peekN(1).Type != lexer.LBRACE {
			continue
		}
		break
	}
	if len(parts) == 0 {
		p.errorf(p.peek().Position, "esperado caminho relativo ./ ou ../")
		return nil, false, false
	}
	return parts, false, true
}

func (p *Parser) parsePathSegmentsAfterSlash() ([]string, bool) {
	var segs []string
	for p.check(lexer.IDENT) {
		segs = append(segs, p.advance().Lexeme)
		if !p.check(lexer.SLASH) {
			break
		}
		p.advance()
	}
	return segs, true
}

func (p *Parser) parsePathSegmentsAfterDotOrSlash() ([]string, bool) {
	var segs []string
	for {
		if !p.check(lexer.DOT) && !p.check(lexer.SLASH) {
			break
		}
		if p.check(lexer.DOT) && p.peekN(1).Type == lexer.LBRACE {
			break
		}
		p.advance()
		if !p.check(lexer.IDENT) {
			p.errorf(p.peek().Position, "esperado segmento após '.' ou '/'")
			return nil, false
		}
		segs = append(segs, p.advance().Lexeme)
	}
	return segs, true
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
		path, ok := p.parseImportPath()
		if !ok {
			return nil
		}
		return p.makeImportDeclFromPath(blockPos, path, names)
	}

	if p.check(lexer.STRING_LITERAL) {
		path := p.advance().Lexeme
		return p.makeImportDeclFromPath(blockPos, path, nil)
	}

	if p.check(lexer.AT) || p.check(lexer.DOT) {
		path, ok := p.parseImportPath()
		if !ok {
			return nil
		}
		return p.makeImportDeclFromPath(blockPos, path, nil)
	}

	p.errorf(p.peek().Position, "esperado {names} from path ou path no import")
	return nil
}

func (p *Parser) parseImportPath() (string, bool) {
	if p.check(lexer.STRING_LITERAL) {
		return p.advance().Lexeme, true
	}
	if p.check(lexer.AT) {
		return p.parseAtImportPath()
	}
	if p.check(lexer.DOT) {
		return p.parseDotImportPath()
	}
	if p.check(lexer.IDENT) {
		return p.parseLegacyIdentPath()
	}
	p.errorf(p.peek().Position, "esperado caminho de import após 'from'")
	return "", false
}

func (p *Parser) parseAtImportPath() (string, bool) {
	p.advance()
	if p.check(lexer.SLASH) {
		p.advance()
		segs, ok := p.parsePathSegmentsAfterSlash()
		if !ok {
			return "", false
		}
		return "@/" + strings.Join(segs, "/"), true
	}
	if !p.check(lexer.IDENT) {
		p.errorf(p.peek().Position, "esperado identificador após '@'")
		return "", false
	}
	first := p.advance().Lexeme
	if first == "soyuz" {
		segs, ok := p.parsePathSegmentsAfterDotOrSlash()
		if !ok && len(segs) == 0 {
			return "@soyuz", true
		}
		if len(segs) > 0 {
			return "@soyuz/" + strings.Join(segs, "/"), true
		}
		return "@soyuz", true
	}
	segs := []string{first}
	more, ok := p.parsePathSegmentsAfterDotOrSlash()
	if !ok {
		return "", false
	}
	segs = append(segs, more...)
	return "@" + strings.Join(segs, "/"), true
}

func (p *Parser) parseDotImportPath() (string, bool) {
	segs, _, ok := p.parseRelativePathSegments()
	if !ok {
		return "", false
	}
	return strings.Join(segs, "/"), true
}

func (p *Parser) parseLegacyIdentPath() (string, bool) {
	var segs []string
	segs = append(segs, p.advance().Lexeme)
	more, ok := p.parsePathSegmentsAfterDotOrSlash()
	if !ok {
		return "", false
	}
	segs = append(segs, more...)
	return strings.Join(segs, "/"), true
}

func (p *Parser) makeImportDeclFromPath(pos lexer.Position, path string, names []ImportName) *ImportDecl {
	if strings.HasPrefix(path, `"`) || strings.HasPrefix(path, `'`) {
		path = strings.Trim(path, `"'`)
	}
	kind, alias := classifyImportPath(path)
	segments := importPathSegments(path, kind, alias)
	return p.buildImportDecl(pos, path, kind, alias, segments, names)
}

func (p *Parser) makeImportDecl(pos lexer.Position, path string, names []ImportName) *ImportDecl {
	return p.makeImportDeclFromPath(pos, path, names)
}

func (p *Parser) makeImportDeclFromSegments(pos lexer.Position, segments []string, isStdlib bool, names []ImportName) *ImportDecl {
	var path string
	var kind ImportPathKind
	var alias string
	if isStdlib {
		path = "@soyuz/" + strings.Join(segments, "/")
		kind = ImportPathStdlib
	} else if len(segments) > 0 && (segments[0] == "." || segments[0] == "..") {
		path = strings.Join(segments, "/")
		kind = ImportPathRelative
	} else {
		path = strings.Join(segments, "/")
		kind = ImportPathLegacy
	}
	return p.buildImportDecl(pos, path, kind, alias, segments, names)
}

func classifyImportPath(path string) (ImportPathKind, string) {
	if strings.HasPrefix(path, "@soyuz/") || path == "@soyuz" {
		return ImportPathStdlib, ""
	}
	if strings.HasPrefix(path, "@/") {
		return ImportPathProjectRoot, ""
	}
	if strings.HasPrefix(path, "@") {
		rest := path[1:]
		if idx := strings.Index(rest, "/"); idx >= 0 {
			return ImportPathPackageAlias, rest[:idx]
		}
		return ImportPathPackageAlias, rest
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || path == "." || path == ".." {
		return ImportPathRelative, ""
	}
	if strings.HasPrefix(path, "@soyuz") {
		return ImportPathStdlib, ""
	}
	return ImportPathLegacy, ""
}

func importPathSegments(path string, kind ImportPathKind, alias string) []string {
	switch kind {
	case ImportPathStdlib:
		p := strings.TrimPrefix(path, "@soyuz/")
		if p == "" {
			return nil
		}
		return strings.Split(p, "/")
	case ImportPathProjectRoot:
		p := strings.TrimPrefix(path, "@/")
		if p == "" {
			return nil
		}
		return strings.Split(p, "/")
	case ImportPathPackageAlias:
		rest := path[1:]
		if alias != "" {
			rest = strings.TrimPrefix(rest, alias+"/")
		}
		if rest == "" {
			return nil
		}
		return strings.Split(rest, "/")
	case ImportPathRelative:
		return strings.Split(path, "/")
	default:
		if strings.HasPrefix(path, "@soyuz/") {
			return strings.Split(strings.TrimPrefix(path, "@soyuz/"), "/")
		}
		return strings.Split(path, "/")
	}
}

func (p *Parser) buildImportDecl(pos lexer.Position, path string, kind ImportPathKind, alias string, segments []string, names []ImportName) *ImportDecl {
	namespace := ""
	if len(segments) > 0 {
		namespace = segments[len(segments)-1]
	}
	return &ImportDecl{
		pos:          pos,
		Path:         path,
		Names:        names,
		Namespace:    namespace,
		PathKind:     kind,
		PackageAlias: alias,
		IsStdlib:     kind == ImportPathStdlib,
	}
}
