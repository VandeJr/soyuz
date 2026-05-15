package checker

import (
	"strings"
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// parseFileNodes parseia src e retorna os nós não-import, associando cada um a filePath.
func parseFileNodes(src, filePath string) ([]parser.Node, map[parser.Node]string) {
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	nf := make(map[parser.Node]string)
	var nodes []parser.Node
	for _, n := range prog.Body {
		if _, isImport := n.(*parser.ImportDecl); !isImport {
			nf[n] = filePath
			nodes = append(nodes, n)
		}
	}
	return nodes, nf
}

// mergeProgram constrói um Program mesclado e um nodeFile unificado a partir de múltiplas fatias.
func mergeProgram(parts ...struct {
	nodes []parser.Node
	nf    map[parser.Node]string
}) (*parser.Program, map[parser.Node]string) {
	merged := &parser.Program{}
	globalNF := make(map[parser.Node]string)
	for _, p := range parts {
		merged.Body = append(merged.Body, p.nodes...)
		for n, f := range p.nf {
			globalNF[n] = f
		}
	}
	return merged, globalNF
}

// hasError retorna true se algum erro contém a substring s.
func hasError(errors []TypeError, s string) bool {
	for _, e := range errors {
		if strings.Contains(e.Message, s) {
			return true
		}
	}
	return false
}

// TestM8PubFuncAccessible verifica que uma função pub é acessível cross-file.
func TestM8PubFuncAccessible(t *testing.T) {
	libSrc := `pub fn dobrar(x: Int) -> Int = x * 2`
	mainSrc := `val r = dobrar(5)`

	libNodes, libNF := parseFileNodes(libSrc, "/lib.soyuz")
	mainNodes, mainNF := parseFileNodes(mainSrc, "/main.soyuz")

	prog, nf := mergeProgram(
		struct{ nodes []parser.Node; nf map[parser.Node]string }{libNodes, libNF},
		struct{ nodes []parser.Node; nf map[parser.Node]string }{mainNodes, mainNF},
	)

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("função pub deve ser acessível cross-file, obtido erros: %v", result.Errors)
	}
}

// TestM8PrivateFuncBlocked verifica que uma função sem pub gera erro cross-file.
func TestM8PrivateFuncBlocked(t *testing.T) {
	libSrc := `fn dobrar(x: Int) -> Int = x * 2`
	mainSrc := `val r = dobrar(5)`

	libNodes, libNF := parseFileNodes(libSrc, "/lib.soyuz")
	mainNodes, mainNF := parseFileNodes(mainSrc, "/main.soyuz")

	prog, nf := mergeProgram(
		struct{ nodes []parser.Node; nf map[parser.Node]string }{libNodes, libNF},
		struct{ nodes []parser.Node; nf map[parser.Node]string }{mainNodes, mainNF},
	)

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if !hasError(result.Errors, "dobrar") || !hasError(result.Errors, "não é público") {
		t.Errorf("função privada deve gerar erro cross-file, obtido: %v", result.Errors)
	}
}

// TestM8PubRecordAccessible verifica que um record pub é acessível cross-file.
func TestM8PubRecordAccessible(t *testing.T) {
	libSrc := `pub record Ponto { x: Int, y: Int }`
	mainSrc := `val p = Ponto { x: 1, y: 2 }`

	libNodes, libNF := parseFileNodes(libSrc, "/lib.soyuz")
	mainNodes, mainNF := parseFileNodes(mainSrc, "/main.soyuz")

	prog, nf := mergeProgram(
		struct{ nodes []parser.Node; nf map[parser.Node]string }{libNodes, libNF},
		struct{ nodes []parser.Node; nf map[parser.Node]string }{mainNodes, mainNF},
	)

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("record pub deve ser acessível cross-file, obtido erros: %v", result.Errors)
	}
}

// TestM8PrivateRecordBlocked verifica que um record sem pub gera erro cross-file.
func TestM8PrivateRecordBlocked(t *testing.T) {
	libSrc := `record Ponto { x: Int, y: Int }`
	mainSrc := `val p = Ponto { x: 1, y: 2 }`

	libNodes, libNF := parseFileNodes(libSrc, "/lib.soyuz")
	mainNodes, mainNF := parseFileNodes(mainSrc, "/main.soyuz")

	prog, nf := mergeProgram(
		struct{ nodes []parser.Node; nf map[parser.Node]string }{libNodes, libNF},
		struct{ nodes []parser.Node; nf map[parser.Node]string }{mainNodes, mainNF},
	)

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if !hasError(result.Errors, "Ponto") || !hasError(result.Errors, "não é público") {
		t.Errorf("record privado deve gerar erro cross-file, obtido: %v", result.Errors)
	}
}

// TestM8SameFileNoEnforcement verifica que símbolos do mesmo arquivo não são afetados.
func TestM8SameFileNoEnforcement(t *testing.T) {
	src := `
fn dobrar(x: Int) -> Int = x * 2
val r = dobrar(5)
`
	nodes, nf := parseFileNodes(src, "/main.soyuz")
	prog := &parser.Program{}
	prog.Body = nodes

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("símbolos do mesmo arquivo não devem ser bloqueados, obtido: %v", result.Errors)
	}
}

// TestM8ValPubAccessible verifica que val pub é acessível cross-file.
func TestM8ValPubAccessible(t *testing.T) {
	libSrc := `pub val PI = 3`
	mainSrc := `val r = PI`

	libNodes, libNF := parseFileNodes(libSrc, "/lib.soyuz")
	mainNodes, mainNF := parseFileNodes(mainSrc, "/main.soyuz")

	prog, nf := mergeProgram(
		struct{ nodes []parser.Node; nf map[parser.Node]string }{libNodes, libNF},
		struct{ nodes []parser.Node; nf map[parser.Node]string }{mainNodes, mainNF},
	)

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("val pub deve ser acessível cross-file, obtido: %v", result.Errors)
	}
}

// TestM8PrivateValBlocked verifica que val sem pub gera erro cross-file.
func TestM8PrivateValBlocked(t *testing.T) {
	libSrc := `val PI = 3`
	mainSrc := `val r = PI`

	libNodes, libNF := parseFileNodes(libSrc, "/lib.soyuz")
	mainNodes, mainNF := parseFileNodes(mainSrc, "/main.soyuz")

	prog, nf := mergeProgram(
		struct{ nodes []parser.Node; nf map[parser.Node]string }{libNodes, libNF},
		struct{ nodes []parser.Node; nf map[parser.Node]string }{mainNodes, mainNF},
	)

	c := New()
	c.SetNodeFiles(nf)
	result := c.Check(prog)

	if !hasError(result.Errors, "PI") || !hasError(result.Errors, "não é público") {
		t.Errorf("val privado deve gerar erro cross-file, obtido: %v", result.Errors)
	}
}

// TestM8SingleFileModeNoPubCheck verifica que sem SetNodeFiles não há enforcement (modo legado).
func TestM8SingleFileModeNoPubCheck(t *testing.T) {
	src := `
fn dobrar(x: Int) -> Int = x * 2
val r = dobrar(5)
`
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()

	// Sem SetNodeFiles — modo legado, sem enforcement.
	result := New().Check(prog)

	if len(result.Errors) > 0 {
		t.Errorf("modo single-file não deve ter enforcement de pub, obtido: %v", result.Errors)
	}
}
