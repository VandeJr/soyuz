package lexer

var keywords = map[string]TokenType{
	// Declarações
	"val":    VAL,
	"var":    VAR,
	"fn":     FN,
	"extern": EXTERN,
	"return": RETURN,
	"pub":    PUB,
	"extend": EXTEND,

	// Tipos compostos
	"record":    RECORD,
	"class":     CLASS,
	"interface": INTERFACE,
	"enum":      ENUM,

	// Controle de fluxo
	"if":       IF,
	"else":     ELSE,
	"when":     WHEN,
	"match":    MATCH,
	"for":      FOR,
	"while":    WHILE,
	"loop":     LOOP,
	"break":    BREAK,
	"continue": CONTINUE,
	"in":       IN,

	// Módulos
	"import": IMPORT,

	// OOP
	"self": SELF,

	// Concurrency
	"task":   TASK,
	"select": SELECT,

	// Tipos primitivos built-in
	"Int":    INT_TYPE,
	"Float":  FLOAT_TYPE,
	"Bool":   BOOL_TYPE,
	"String": STRING_TYPE,
	"Char":   CHAR_TYPE,
	"Unit":   UNIT_TYPE,

	// Valores literais
	"true":  TRUE,
	"false": FALSE,
	"None":  NONE,

	// Construtores de Result / Option
	"Ok":   OK,
	"Err":  ERR,
	"Some": SOME,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
