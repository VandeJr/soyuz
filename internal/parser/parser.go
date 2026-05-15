package parser

import (
	"fmt"
	"slices"
	"soyuz/internal/lexer"
)

// ParseError represents a syntax error with source position.
type ParseError struct {
	Position lexer.Position
	Message  string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%s: %s", e.Position, e.Message)
}

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
		node := p.parseTopLevel()
		if node != nil {
			prog.Body = append(prog.Body, node)
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

func (p *Parser) skipSemicolons() {
	for p.check(lexer.SEMICOLON) {
		p.advance()
	}
}

func (p *Parser) errorf(pos lexer.Position, format string, args ...any) {
	p.errors = append(p.errors, ParseError{
		Position: pos,
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
		case lexer.FN, lexer.VAL, lexer.VAR, lexer.CONST,
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
