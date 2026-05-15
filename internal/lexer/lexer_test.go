package lexer

import (
	"testing"
)

func TestBasicTokens(t *testing.T) {
	input := `val x: Int = 10`
	expected := []TokenType{VAL, IDENT, COLON, INT_TYPE, ASSIGN, INT_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)
}

func TestFunctionDecl(t *testing.T) {
	input := `fn soma(a: Int, b: Int) -> Int {
    return a
}`
	expected := []TokenType{
		FN, IDENT, LPAREN,
		IDENT, COLON, INT_TYPE, COMMA,
		IDENT, COLON, INT_TYPE,
		RPAREN, ARROW, INT_TYPE, LBRACE,
		RETURN, IDENT, SEMICOLON,
		RBRACE, SEMICOLON, EOF,
	}
	assertTokenTypes(t, input, expected)
}

func TestPipeOperator(t *testing.T) {
	input := `val r = buscar(1) |> validar |> transformar`
	expected := []TokenType{
		VAL, IDENT, ASSIGN,
		IDENT, LPAREN, INT_LITERAL, RPAREN,
		PIPE, IDENT,
		PIPE, IDENT,
		SEMICOLON, EOF,
	}
	assertTokenTypes(t, input, expected)
}

func TestArrowAndFatArrow(t *testing.T) {
	input := `fn(x: Int) => x`
	expected := []TokenType{FN, LPAREN, IDENT, COLON, INT_TYPE, RPAREN, FAT_ARROW, IDENT, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)
}

func TestRanges(t *testing.T) {
	input := `0..10`
	expected := []TokenType{INT_LITERAL, RANGE, INT_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)

	input2 := `0..=10`
	expected2 := []TokenType{INT_LITERAL, RANGE_INCL, INT_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input2, expected2)
}

func TestSafeNavAndElvis(t *testing.T) {
	input := `nome?.length ?: "default"`
	expected := []TokenType{IDENT, SAFE_NAV, IDENT, ELVIS, STRING_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)
}

func TestResultConstructors(t *testing.T) {
	input := `Ok(valor)`
	expected := []TokenType{OK, LPAREN, IDENT, RPAREN, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)

	input2 := `Err(MeuErro { msg: "falhou" })`
	expected2 := []TokenType{ERR, LPAREN, IDENT, LBRACE, IDENT, COLON, STRING_LITERAL, RBRACE, RPAREN, SEMICOLON, EOF}
	assertTokenTypes(t, input2, expected2)
}

func TestMapLiteral(t *testing.T) {
	input := `val m = ["a": 1, "b": 2]`
	expected := []TokenType{
		VAL, IDENT, ASSIGN,
		LBRACKET, STRING_LITERAL, COLON, INT_LITERAL, COMMA,
		STRING_LITERAL, COLON, INT_LITERAL,
		RBRACKET, SEMICOLON, EOF,
	}
	assertTokenTypes(t, input, expected)
}

func TestGenerics(t *testing.T) {
	input := `fn identidade[T](x: T) -> T`
	expected := []TokenType{
		FN, IDENT, LBRACKET, IDENT, RBRACKET,
		LPAREN, IDENT, COLON, IDENT, RPAREN,
		ARROW, IDENT, SEMICOLON, EOF,
	}
	assertTokenTypes(t, input, expected)
}

func TestSemicolonInsertion(t *testing.T) {
	input := "val x = 10\nval y = 20"
	tokens := Tokenize(input)

	// Deve ter SEMICOLON após o 10
	found := false
	for i, tok := range tokens {
		if tok.Type == INT_LITERAL && tok.Lexeme == "10" {
			if i+1 < len(tokens) && tokens[i+1].Type == SEMICOLON {
				found = true
			}
		}
	}
	if !found {
		t.Error("esperado SEMICOLON virtual após literal na newline")
	}
}

func TestFloatLiteral(t *testing.T) {
	input := `3.14`
	expected := []TokenType{FLOAT_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)
}

func TestComment(t *testing.T) {
	input := "val x = 10 // isso é um comentário\nval y = 20"
	expected := []TokenType{VAL, IDENT, ASSIGN, INT_LITERAL, SEMICOLON, VAL, IDENT, ASSIGN, INT_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)
}

func TestWildcard(t *testing.T) {
	input := `_ => "default"`
	expected := []TokenType{UNDERSCORE, FAT_ARROW, STRING_LITERAL, SEMICOLON, EOF}
	assertTokenTypes(t, input, expected)
}

// --- helpers ---

func assertTokenTypes(t *testing.T, input string, expected []TokenType) {
	t.Helper()
	tokens := Tokenize(input)

	if len(tokens) != len(expected) {
		t.Errorf("input: %q\nesperado %d tokens, obtido %d", input, len(expected), len(tokens))
		t.Log("tokens obtidos:")
		for i, tok := range tokens {
			t.Logf("  [%d] %s %q", i, tok.Type, tok.Lexeme)
		}
		return
	}

	for i, tok := range tokens {
		if tok.Type != expected[i] {
			t.Errorf("token[%d]: esperado %s, obtido %s (%q) — input: %q",
				i, expected[i], tok.Type, tok.Lexeme, input)
		}
	}
}
