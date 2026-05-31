package lexer

import (
	"unicode/utf8"
)

// Lexer para a linguagem Soyuz (.sy)
type Lexer struct {
	input        []rune // source decodificado como runes (UTF-8 correto)
	position     int    // índice do char atual
	readPosition int    // índice do próximo char
	ch           rune   // char atual

	line   int
	column int

	// Estado de interpolação de string
	// Quando > 0, estamos dentro de $( ... )
	interpDepth int
	parenDepth  int

	// Último token emitido — usado para decidir se insere SEMICOLON virtual
	lastToken TokenType

	// Fila de tokens pendentes — usada na interpolação de strings
	// onde um único readString pode gerar múltiplos tokens
	pending []Token
}

func NewLexer(input string) *Lexer {
	l := &Lexer{
		input:  []rune(input),
		line:   1,
		column: 0,
	}
	l.readChar()
	return l
}

// --- Navegação ---

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++

	if l.ch == '\n' {
		// Não incrementa linha aqui — incrementamos APÓS emitir o SEMICOLON
		// para que o token tenha a linha correta.
	} else {
		l.column++
	}
}

func (l *Lexer) peekChar() rune {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) peekCharN(n int) rune {
	pos := l.readPosition + n - 1
	if pos >= len(l.input) {
		return 0
	}
	return l.input[pos]
}

func (l *Lexer) currentPos() Position {
	return Position{Line: l.line, Column: l.column}
}

// --- Whitespace e comentários ---

// skipWhitespace pula espaços e tabs, mas NÃO pula \n
// pois \n pode gerar SEMICOLON virtual.
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
		l.readChar()
	}
}

func (l *Lexer) skipLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

func (l *Lexer) skipBlockComment() {
	// Consome até */
	for l.ch != 0 {
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar() // *
			l.readChar() // /
			return
		}
		if l.ch == '\n' {
			l.line++
			l.column = 0
		}
		l.readChar()
	}
}

// --- Leitores ---

func (l *Lexer) readIdentifier() string {
	start := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return string(l.input[start:l.position])
}

func (l *Lexer) readNumber() (string, TokenType) {
	start := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	// Float se tiver . seguido de dígito
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar() // consome o .
		for isDigit(l.ch) {
			l.readChar()
		}
		return string(l.input[start:l.position]), FLOAT_LITERAL
	}
	return string(l.input[start:l.position]), INT_LITERAL
}

// readChar2 lê um literal de char delimitado por aspas simples: 'a', '\n', '\”, etc.
func (l *Lexer) readChar2(pos Position) Token {
	l.readChar() // consome '
	var ch rune
	if l.ch == '\\' {
		l.readChar()
		switch l.ch {
		case 'n':
			ch = '\n'
		case 't':
			ch = '\t'
		case '\'':
			ch = '\''
		case '\\':
			ch = '\\'
		case '0':
			ch = 0
		default:
			ch = l.ch
		}
	} else {
		ch = l.ch
	}
	l.readChar() // consome o char
	if l.ch == '\'' {
		l.readChar() // consome '
	}
	return Token{Type: CHAR_LITERAL, Lexeme: string(ch), Position: pos}
}

// readString lê uma string com suporte a interpolação $(...)
// Pode gerar múltiplos tokens que ficam em l.pending.
// Retorna o primeiro token.
func (l *Lexer) readString(pos Position, isContinuation bool) Token {
	if !isContinuation {
		l.readChar() // consome a aspas de abertura "
	}

	var part []rune
	var tokens []Token

	for l.ch != 0 {
		if l.ch == '"' {
			// Fecha a string
			l.readChar() // consome "
			if len(part) > 0 || len(tokens) == 0 {
				tokens = append(tokens, Token{
					Type:     STRING_LITERAL,
					Lexeme:   string(part),
					Position: pos,
				})
			}
			break
		}

		if l.ch == '\\' {
			// Escape sequences
			l.readChar()
			switch l.ch {
			case 'n':
				part = append(part, '\n')
			case 't':
				part = append(part, '\t')
			case '"':
				part = append(part, '"')
			case '\\':
				part = append(part, '\\')
			case '$':
				part = append(part, '$')
			default:
				part = append(part, '\\', l.ch)
			}
			l.readChar()
			continue
		}

		if l.ch == '$' && l.peekChar() == '(' {
			// Início de interpolação
			// Emite a parte de string acumulada
			if len(part) > 0 {
				tokens = append(tokens, Token{
					Type:     STRING_PART,
					Lexeme:   string(part),
					Position: pos,
				})
				part = nil
			}
			l.readChar() // $
			l.readChar() // (
			tokens = append(tokens, Token{
				Type:     INTERP_START,
				Lexeme:   "$(",
				Position: l.currentPos(),
			})
			// Os tokens da expressão interpolada serão lidos
			// pelo NextToken normal. Precisamos de um marcador
			// para saber quando fecha o ).
			l.interpDepth++
			break
		}

		if l.ch == '\n' {
			l.line++
			l.column = 0
		}

		part = append(part, l.ch)
		l.readChar()
	}

	if len(tokens) == 0 {
		return Token{Type: STRING_LITERAL, Lexeme: "", Position: pos}
	}

	// O primeiro token é retornado, o resto vai para pending
	first := tokens[0]
	if len(tokens) > 1 {
		l.pending = append(l.pending, tokens[1:]...)
	}
	return first
}

func (l *Lexer) readRawString(pos Position) Token {
	l.readChar() // consome a aspas de abertura "

	var part []rune
	for l.ch != 0 {
		if l.ch == '"' {
			l.readChar() // consome "
			break
		}
		if l.ch == '\n' {
			l.line++
			l.column = 0
		}
		part = append(part, l.ch)
		l.readChar()
	}

	return Token{Type: STRING_LITERAL, Lexeme: string(part), Position: pos}
}

func (l *Lexer) isNextToken(s string) bool {
	idx := l.position
	if l.ch == '\n' {
		idx = l.readPosition
	}
	// Skip spaces and tabs
	for idx < len(l.input) && (l.input[idx] == ' ' || l.input[idx] == '\t' || l.input[idx] == '\r') {
		idx++
	}
	for i := 0; i < len(s); i++ {
		if idx+i >= len(l.input) || l.input[idx+i] != rune(s[i]) {
			return false
		}
	}
	// Se for uma keyword, o próximo char não pode ser letra/dígito
	if isLetter(rune(s[0])) {
		nextIdx := idx + len(s)
		if nextIdx < len(l.input) && (isLetter(l.input[nextIdx]) || isDigit(l.input[nextIdx])) {
			return false
		}
	}
	return true
}

func (l *Lexer) isNextRelevantTokenPipe() bool {
	return l.isNextToken("|>")
}

func (l *Lexer) isNextRelevantTokenWhen() bool {
	return l.isNextToken("when")
}

func (l *Lexer) isNextRelevantTokenMemberAccess() bool {
	return l.isNextToken(".") || l.isNextToken("?.")
}

// --- NextToken ---

func (l *Lexer) NextToken() Token {
	// Drena tokens pendentes da interpolação
	if len(l.pending) > 0 {
		tok := l.pending[0]
		l.pending = l.pending[1:]
		l.lastToken = tok.Type
		return tok
	}

	l.skipWhitespace()

	// Tratamento de newline — insere SEMICOLON virtual se necessário
	if l.ch == '\n' {
		// Suprimir SEMICOLON quando a próxima linha continua a expressão.
		if CanInsertSemicolon(l.lastToken) &&
			(l.isNextRelevantTokenPipe() || l.isNextRelevantTokenWhen() || l.isNextRelevantTokenMemberAccess()) {
			l.line++
			l.column = 0
			l.readChar()
			return l.NextToken()
		}

		l.line++
		l.column = 0
		l.readChar()

		if CanInsertSemicolon(l.lastToken) {
			tok := Token{
				Type:     SEMICOLON,
				Lexeme:   ";",
				Position: Position{Line: l.line - 1, Column: l.column},
			}
			l.lastToken = SEMICOLON
			return tok
		}
		// Newline sem SEMICOLON — continua para o próximo token
		return l.NextToken()
	}

	// Comentários
	if l.ch == '/' {
		if l.peekChar() == '/' {
			l.skipLineComment()
			return l.NextToken()
		}
		if l.peekChar() == '*' {
			l.readChar()
			l.readChar()
			l.skipBlockComment()
			return l.NextToken()
		}
	}

	pos := l.currentPos()
	var tok Token

	switch l.ch {
	// INTERP_END — fechando uma interpolação
	case '(':
		if l.interpDepth > 0 {
			l.parenDepth++
		}
		tok = Token{Type: LPAREN, Lexeme: "(", Position: pos}
		l.readChar()
		l.lastToken = tok.Type
		return tok

	case ')':
		if l.interpDepth > 0 {
			if l.parenDepth > 0 {
				l.parenDepth--
				tok = Token{Type: RPAREN, Lexeme: ")", Position: pos}
				l.readChar()
				l.lastToken = tok.Type
				return tok
			}
			l.interpDepth--
			tok = Token{Type: INTERP_END, Lexeme: ")", Position: pos}
			l.readChar()
			// Continua lendo o restante da string
			strTok := l.readString(pos, true)
			// Se for STRING_LITERAL vazio ao fechar, ignora
			if strTok.Type == STRING_LITERAL && strTok.Lexeme == "" {
				// nada a fazer
			} else {
				l.pending = append([]Token{strTok}, l.pending...)
			}
			l.lastToken = tok.Type
			return tok
		}
		tok = Token{Type: RPAREN, Lexeme: ")", Position: pos}

	case '=':
		switch l.peekChar() {
		case '=':
			l.readChar()
			tok = Token{Type: EQUALS, Lexeme: "==", Position: pos}
		case '>':
			l.readChar()
			tok = Token{Type: FAT_ARROW, Lexeme: "=>", Position: pos}
		default:
			tok = Token{Type: ASSIGN, Lexeme: "=", Position: pos}
		}

	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: NOT_EQUALS, Lexeme: "!=", Position: pos}
		} else {
			tok = Token{Type: BANG, Lexeme: "!", Position: pos}
		}

	case '<':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: LTE, Lexeme: "<=", Position: pos}
		} else if l.peekChar() == '<' {
			l.readChar()
			tok = Token{Type: SHL, Lexeme: "<<", Position: pos}
		} else {
			tok = Token{Type: LT, Lexeme: "<", Position: pos}
		}

	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: GTE, Lexeme: ">=", Position: pos}
		} else if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: SHR, Lexeme: ">>", Position: pos}
		} else {
			tok = Token{Type: GT, Lexeme: ">", Position: pos}
		}

	case '^':
		tok = Token{Type: CARET, Lexeme: "^", Position: pos}

	case '~':
		if l.peekChar() == '?' && l.peekCharN(2) == '>' {
			l.readChar()
			l.readChar()
			tok = Token{Type: ASYNC_PIPE_QUEST, Lexeme: "~?>", Position: pos}
		} else if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: ASYNC_PIPE, Lexeme: "~>", Position: pos}
		} else {
			tok = Token{Type: TILDE, Lexeme: "~", Position: pos}
		}

	case '-':
		if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: ARROW, Lexeme: "->", Position: pos}
		} else {
			tok = Token{Type: MINUS, Lexeme: "-", Position: pos}
		}

	case '|':
		if l.peekChar() == '?' && l.peekCharN(2) == '>' {
			l.readChar() // ?
			l.readChar() // >
			tok = Token{Type: PIPE_QUEST, Lexeme: "|?>", Position: pos}
		} else if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: PIPE, Lexeme: "|>", Position: pos}
		} else if l.peekChar() == '|' {
			l.readChar()
			tok = Token{Type: OR, Lexeme: "||", Position: pos}
		} else {
			tok = Token{Type: PIPE_SINGLE, Lexeme: "|", Position: pos}
		}

	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok = Token{Type: AND, Lexeme: "&&", Position: pos}
		} else {
			tok = Token{Type: AMPERSAND, Lexeme: "&", Position: pos}
		}

	case '?':
		switch l.peekChar() {
		case '.':
			l.readChar()
			tok = Token{Type: SAFE_NAV, Lexeme: "?.", Position: pos}
		case ':':
			l.readChar()
			tok = Token{Type: ELVIS, Lexeme: "?:", Position: pos}
		default:
			tok = Token{Type: QUESTION, Lexeme: "?", Position: pos}
		}

	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			if l.peekChar() == '=' {
				l.readChar()
				tok = Token{Type: RANGE_INCL, Lexeme: "..=", Position: pos}
			} else {
				tok = Token{Type: RANGE, Lexeme: "..", Position: pos}
			}
		} else {
			tok = Token{Type: DOT, Lexeme: ".", Position: pos}
		}

	case '+':
		tok = Token{Type: PLUS, Lexeme: "+", Position: pos}
	case '*':
		tok = Token{Type: ASTERISK, Lexeme: "*", Position: pos}
	case '/':
		tok = Token{Type: SLASH, Lexeme: "/", Position: pos}
	case '%':
		tok = Token{Type: PERCENT, Lexeme: "%", Position: pos}
	case ':':
		tok = Token{Type: COLON, Lexeme: ":", Position: pos}
	case ',':
		tok = Token{Type: COMMA, Lexeme: ",", Position: pos}
	case ';':
		tok = Token{Type: SEMICOLON, Lexeme: ";", Position: pos}
	case '{':
		tok = Token{Type: LBRACE, Lexeme: "{", Position: pos}
	case '}':
		tok = Token{Type: RBRACE, Lexeme: "}", Position: pos}
	case '[':
		tok = Token{Type: LBRACKET, Lexeme: "[", Position: pos}
	case ']':
		tok = Token{Type: RBRACKET, Lexeme: "]", Position: pos}
	case '@':
		tok = Token{Type: AT, Lexeme: "@", Position: pos}
	case '#':
		tok = Token{Type: HASH, Lexeme: "#", Position: pos}
	case '_':
		// Pode ser underscore isolado (wildcard) ou início de ident
		if !isLetter(l.peekChar()) && !isDigit(l.peekChar()) {
			tok = Token{Type: UNDERSCORE, Lexeme: "_", Position: pos}
		} else {
			lexeme := l.readIdentifier()
			tok = Token{Type: LookupIdent(lexeme), Lexeme: lexeme, Position: pos}
			l.lastToken = tok.Type
			return tok
		}

	case '\'':
		tok = l.readChar2(pos)
		l.lastToken = tok.Type
		return tok

	case '"':
		tok = l.readString(pos, false)
		l.lastToken = tok.Type
		return tok

	case 0:
		// EOF — insere SEMICOLON final se necessário
		if CanInsertSemicolon(l.lastToken) {
			tok = Token{Type: SEMICOLON, Lexeme: ";", Position: pos}
			l.lastToken = SEMICOLON
			return tok
		}
		tok = Token{Type: EOF, Lexeme: "", Position: pos}

	default:
		if isLetter(l.ch) {
			lexeme := l.readIdentifier()
			if lexeme == "r" && l.ch == '"' {
				tok = l.readRawString(pos)
				l.lastToken = tok.Type
				return tok
			}
			tok = Token{Type: LookupIdent(lexeme), Lexeme: lexeme, Position: pos}
			l.lastToken = tok.Type
			return tok
		}
		if isDigit(l.ch) {
			lexeme, tokType := l.readNumber()
			tok = Token{Type: tokType, Lexeme: lexeme, Position: pos}
			l.lastToken = tok.Type
			return tok
		}
		tok = Token{Type: ILLEGAL, Lexeme: string(l.ch), Position: pos}
	}

	l.readChar()
	l.lastToken = tok.Type
	return tok
}

// Tokenize retorna todos os tokens do input de uma vez.
// Útil para testes e debug.
func Tokenize(input string) []Token {
	l := NewLexer(input)
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == EOF {
			break
		}
	}
	return tokens
}

// --- Helpers ---

func isLetter(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		ch == '_' ||
		(ch > 127 && ch != utf8.RuneError) // suporte básico a unicode em idents
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}
