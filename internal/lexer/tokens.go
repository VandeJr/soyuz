package lexer

import "fmt"

type TokenType int

type Position struct {
	Line   int
	Column int
}

func (p Position) String() string {
	return fmt.Sprintf("L%d:C%d", p.Line, p.Column)
}

type Token struct {
	Type     TokenType
	Lexeme   string
	Position Position
}

func (t Token) String() string {
	return fmt.Sprintf("Token{%s, %q, %s}", t.Type, t.Lexeme, t.Position)
}

const (
	// Special
	ILLEGAL TokenType = iota
	EOF
	SEMICOLON // virtual — inserted by lexer on newline

	// Literals
	IDENT
	INT_LITERAL
	FLOAT_LITERAL
	STRING_LITERAL
	CHAR_LITERAL // 'a'
	STRING_PART  // parte de string interpolada: "olá "
	INTERP_START // $(
	INTERP_END   // ) fechando interpolação

	// Arithmetic
	PLUS     // +
	MINUS    // -
	ASTERISK // *
	SLASH    // /
	PERCENT  // %

	// Comparison
	EQUALS     // ==
	NOT_EQUALS // !=
	LT         // <
	GT         // >
	LTE        // <=
	GTE        // >=

	// Logic
	AND  // &&
	OR   // ||
	BANG // !

	// Assignment
	ASSIGN // =

	// Symbols
	COLON       // :
	DOT         // .
	COMMA       // ,
	QUESTION    // ?
	AT          // @
	HASH        // #
	UNDERSCORE  // _
	AMPERSAND   // &
	PIPE_SINGLE // |

	// Paired
	LPAREN   // (
	RPAREN   // )
	LBRACE   // {
	RBRACE   // }
	LBRACKET // [
	RBRACKET // ]

	// Compound symbols
	ARROW      // ->
	FAT_ARROW  // =>
	PIPE       // |>
	PIPE_QUEST // |?>
	RANGE      // ..
	RANGE_INCL // ..=
	SAFE_NAV   // ?.
	ELVIS      // ?:
	SHL        // <<
	SHR        // >>
	CARET      // ^
	TILDE      // ~

	// Keywords — declarations
	VAL
	VAR
	FN
	EXTERN
	RETURN
	EXTEND

	// Keywords — types
	RECORD
	CLASS
	INTERFACE
	ENUM

	// Keywords — control flow
	IF
	ELSE
	WHEN
	MATCH
	FOR
	WHILE
	LOOP
	BREAK
	CONTINUE
	IN

	// Keywords — modules
	IMPORT
	PUB

	// Keywords — OOP
	SELF

	// Keywords — built-in types
	INT_TYPE
	FLOAT_TYPE
	BOOL_TYPE
	STRING_TYPE
	CHAR_TYPE
	UNIT_TYPE

	// Keywords — values
	TRUE
	FALSE
	NONE

	// Keywords — Result / Option constructors
	OK
	ERR
	SOME
)

func (t TokenType) String() string {
	names := map[TokenType]string{
		ILLEGAL:        "ILLEGAL",
		EOF:            "EOF",
		SEMICOLON:      "SEMICOLON",
		IDENT:          "IDENT",
		INT_LITERAL:    "INT_LITERAL",
		FLOAT_LITERAL:  "FLOAT_LITERAL",
		STRING_LITERAL: "STRING_LITERAL",
		CHAR_LITERAL:   "CHAR_LITERAL",
		STRING_PART:    "STRING_PART",
		INTERP_START:   "INTERP_START",
		INTERP_END:     "INTERP_END",
		PLUS:           "PLUS",
		MINUS:          "MINUS",
		ASTERISK:       "ASTERISK",
		SLASH:          "SLASH",
		PERCENT:        "PERCENT",
		EQUALS:         "EQUALS",
		NOT_EQUALS:     "NOT_EQUALS",
		LT:             "LT",
		GT:             "GT",
		LTE:            "LTE",
		GTE:            "GTE",
		AND:            "AND",
		OR:             "OR",
		BANG:           "BANG",
		ASSIGN:         "ASSIGN",
		COLON:          "COLON",
		DOT:            "DOT",
		COMMA:          "COMMA",
		QUESTION:       "QUESTION",
		AT:             "AT",
		HASH:           "HASH",
		UNDERSCORE:     "UNDERSCORE",
		AMPERSAND:      "AMPERSAND",
		PIPE_SINGLE:    "PIPE_SINGLE",
		LPAREN:         "LPAREN",
		RPAREN:         "RPAREN",
		LBRACE:         "LBRACE",
		RBRACE:         "RBRACE",
		LBRACKET:       "LBRACKET",
		RBRACKET:       "RBRACKET",
		ARROW:          "ARROW",
		FAT_ARROW:      "FAT_ARROW",
		PIPE:           "PIPE",
		PIPE_QUEST:     "PIPE_QUEST",
		RANGE:          "RANGE",
		RANGE_INCL:     "RANGE_INCL",
		SAFE_NAV:       "SAFE_NAV",
		ELVIS:          "ELVIS",
		SHL:            "SHL",
		SHR:            "SHR",
		CARET:          "CARET",
		TILDE:          "TILDE",
		VAL:            "VAL",
		VAR:            "VAR",
		FN:             "FN",
		EXTERN:         "EXTERN",
		RETURN:         "RETURN",
		EXTEND:         "EXTEND",
		RECORD:         "RECORD",
		CLASS:          "CLASS",
		INTERFACE:      "INTERFACE",
		ENUM:           "ENUM",
		IF:             "IF",
		ELSE:           "ELSE",
		WHEN:           "WHEN",
		MATCH:          "MATCH",
		FOR:            "FOR",
		WHILE:          "WHILE",
		LOOP:           "LOOP",
		BREAK:          "BREAK",
		CONTINUE:       "CONTINUE",
		IN:             "IN",
		IMPORT:         "IMPORT",
		PUB:            "PUB",
		SELF:           "SELF",
		INT_TYPE:       "INT_TYPE",
		FLOAT_TYPE:     "FLOAT_TYPE",
		BOOL_TYPE:      "BOOL_TYPE",
		STRING_TYPE:    "STRING_TYPE",
		CHAR_TYPE:      "CHAR_TYPE",
		UNIT_TYPE:      "UNIT_TYPE",
		TRUE:           "TRUE",
		FALSE:          "FALSE",
		NONE:           "NONE",
		OK:             "OK",
		ERR:            "ERR",
		SOME:           "SOME",
	}
	if s, ok := names[t]; ok {
		return s
	}
	return "UNKNOWN"
}

func CanInsertSemicolon(t TokenType) bool {
	switch t {
	case IDENT,
		INT_LITERAL, FLOAT_LITERAL, STRING_LITERAL, CHAR_LITERAL, INTERP_END,
		TRUE, FALSE, NONE, OK, ERR, SOME, SELF,
		RPAREN, RBRACE, RBRACKET,
		RETURN, BREAK, CONTINUE,
		INT_TYPE, FLOAT_TYPE, BOOL_TYPE, STRING_TYPE, CHAR_TYPE, UNIT_TYPE:
		return true
	}
	return false
}
