package parser

import (
	"fmt"
	"slices"
	"soyuz/internal/lexer"
)

// ParseError represents a syntax error with source position.
type ParseError struct {
	Position lexer.Position
	End      lexer.Position
	Message  string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Position, e.Message)
}

const maxParseErrors = 256

// Parser is a recursive descent / Pratt parser for Soyuz (.sy).
type Parser struct {
	tokens []lexer.Token
	pos    int
	errors []ParseError
}

// New creates a Parser from a token slice (as produced by lexer.Tokenize).
func New(tokens []lexer.Token) *Parser {
	return &Parser{tokens: tokens}
}

// Errors returns all collected parse errors.
func (p *Parser) Errors() []ParseError { return p.errors }

// HasErrors returns true if any parse errors were collected.
func (p *Parser) HasErrors() bool { return len(p.errors) > 0 }

// Parse parses the entire program and returns the AST root.
func (p *Parser) Parse() *Program {
	prog := &Program{pos: p.peek().Position}
	p.skipSemicolons()
	for !p.check(lexer.EOF) {
		if p.check(lexer.IMPORT) {
			decls := p.parseImportBlock()
			for _, d := range decls {
				if d != nil {
					prog.Body = append(prog.Body, d)
				}
			}
		} else {
			node := p.parseTopLevel()
			if node != nil {
				prog.Body = append(prog.Body, node)
			}
		}
		p.skipSemicolons()
	}
	return prog
}

// ── Navigation helpers ───────────────────────────────────────────────────────

func (p *Parser) peek() lexer.Token {
	return p.tokens[p.pos]
}

func (p *Parser) peekN(n int) lexer.Token {
	idx := p.pos + n
	if idx >= len(p.tokens) {
		return lexer.Token{Type: lexer.EOF}
	}
	return p.tokens[idx]
}

func (p *Parser) advance() lexer.Token {
	tok := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return tok
}

func (p *Parser) check(t lexer.TokenType) bool {
	return p.peek().Type == t
}

func (p *Parser) checkAny(types ...lexer.TokenType) bool {
	return slices.Contains(types, p.peek().Type)
}

func (p *Parser) expect(t lexer.TokenType) lexer.Token {
	tok := p.peek()
	if tok.Type == t {
		return p.advance()
	}
	p.errorf(tok.Position, "esperado %s, encontrado %s (%q)", t, tok.Type, tok.Lexeme)
	return tok
}

func (p *Parser) consume(t lexer.TokenType) bool {
	if p.check(t) {
		p.advance()
		return true
	}
	return false
}

// expectName accepts IDENT or any keyword token that can be used as a name
// (e.g. Ok, Err, Some, None as enum variant names).
func (p *Parser) expectName() lexer.Token {
	tok := p.peek()
	if tok.Type == lexer.IDENT || p.isNameLikeKeyword(tok.Type) {
		return p.advance()
	}
	p.errorf(tok.Position, "esperado identificador, encontrado %s (%q)", tok.Type, tok.Lexeme)
	return tok
}

// isNameLikeKeyword returns true for keyword tokens that are commonly used as names
// (type constructors, variant names, field names, etc.).
func (p *Parser) isNameLikeKeyword(t lexer.TokenType) bool {
	switch t {
	case lexer.OK, lexer.ERR, lexer.SOME, lexer.NONE,
		lexer.TRUE, lexer.FALSE,
		lexer.INT_TYPE, lexer.FLOAT_TYPE, lexer.BOOL_TYPE,
		lexer.STRING_TYPE, lexer.CHAR_TYPE, lexer.UNIT_TYPE,
		lexer.VAL, lexer.VAR, lexer.FN,
		lexer.PUB, lexer.SELF, lexer.IN,
		// declaration / control keywords used as enum variant names (e.g. AST Node)
		lexer.RECORD, lexer.CLASS, lexer.INTERFACE, lexer.ENUM,
		lexer.IMPORT, lexer.EXTEND, lexer.EXTERN,
		lexer.RETURN, lexer.BREAK, lexer.CONTINUE,
		lexer.IF, lexer.ELSE, lexer.WHEN, lexer.MATCH,
		lexer.FOR, lexer.WHILE, lexer.LOOP,
		lexer.TASK, lexer.SELECT, lexer.TEST:
		return true
	}
	return false
}

// checkName returns true if the current token is an IDENT or a keyword usable as a name.
func (p *Parser) checkName() bool {
	return p.check(lexer.IDENT) || p.isNameLikeKeyword(p.peek().Type)
}

func (p *Parser) skipSemicolons() {
	for p.check(lexer.SEMICOLON) {
		p.advance()
	}
}

// recoverDecl skips to the next likely declaration boundary and always advances
// at least one token so error-recovery loops cannot spin on the same token.
func (p *Parser) recoverDecl() {
	before := p.pos
	p.synchronize()
	if p.pos == before && !p.check(lexer.EOF) {
		p.advance()
	}
}

// bumpOrBail reports whether the parser advanced since before; on stagnation it
// records one recovery error, synchronizes, and returns false.
func (p *Parser) bumpOrBail(before int) bool {
	if p.pos != before {
		return true
	}
	p.errorf(p.peek().Position, "erro de recuperação do parser")
	p.recoverDecl()
	return false
}

func (p *Parser) errorf(pos lexer.Position, format string, args ...any) {
	if len(p.errors) >= maxParseErrors {
		return
	}
	end := pos
	if p.pos < len(p.tokens) {
		tok := p.tokens[p.pos]
		if tok.Position.Line == pos.Line && tok.Position.Column == pos.Column {
			width := len(tok.Lexeme)
			if width < 1 {
				width = 1
			}
			end = lexer.Position{Line: pos.Line, Column: pos.Column + width}
		}
	}
	p.errors = append(p.errors, ParseError{
		Position: pos,
		End:      end,
		Message:  fmt.Sprintf(format, args...),
	})
}

// synchronize skips to the next statement boundary for error recovery.
func (p *Parser) synchronize() {
	for !p.check(lexer.EOF) {
		if p.check(lexer.SEMICOLON) {
			p.advance()
			return
		}
		switch p.peek().Type {
		case lexer.FN, lexer.VAL, lexer.VAR,
			lexer.CLASS, lexer.RECORD, lexer.INTERFACE, lexer.ENUM,
			lexer.IF, lexer.FOR, lexer.WHILE, lexer.LOOP, lexer.RETURN,
			lexer.RBRACE:
			return
		}
		p.advance()
	}
}

// isUppercase reports whether s starts with an ASCII uppercase letter.
func isUppercase(s string) bool {
	if len(s) == 0 {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}
