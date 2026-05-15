package parser

import (
	"testing"

	"soyuz/internal/lexer"
)

// helper: parse source string and return the Program
func parseSource(t *testing.T, src string) *Program {
	t.Helper()
	tokens := lexer.Tokenize(src)
	p := New(tokens)
	prog := p.Parse()
	if p.HasErrors() {
		for _, e := range p.Errors() {
			t.Errorf("parse error: %s", e.Error())
		}
	}
	return prog
}

// helper: assert node count at top level
func assertBodyLen(t *testing.T, prog *Program, n int) {
	t.Helper()
	if len(prog.Body) != n {
		t.Fatalf("esperado %d nós no body, obtido %d", n, len(prog.Body))
	}
}

// ============================================================
// Variable declarations
// ============================================================

func TestValDecl(t *testing.T) {
	prog := parseSource(t, `val x: Int = 42`)
	assertBodyLen(t, prog, 1)

	decl, ok := prog.Body[0].(*VarDecl)
	if !ok {
		t.Fatalf("esperado *VarDecl, obtido %T", prog.Body[0])
	}
	if decl.Kind != KindVal {
		t.Errorf("esperado kind=val, obtido %s", decl.Kind)
	}
	if decl.Name != "x" {
		t.Errorf("esperado name=x, obtido %s", decl.Name)
	}
	if _, ok := decl.Type.(*NamedType); !ok {
		t.Errorf("esperado tipo *NamedType")
	}
	if _, ok := decl.Init.(*IntLiteral); !ok {
		t.Errorf("esperado init *IntLiteral")
	}
}

func TestVarDecl(t *testing.T) {
	prog := parseSource(t, `var contador = 0`)
	assertBodyLen(t, prog, 1)
	decl := prog.Body[0].(*VarDecl)
	if decl.Kind != KindVar {
		t.Errorf("esperado var, obtido %s", decl.Kind)
	}
}

func TestConstDecl(t *testing.T) {
	prog := parseSource(t, `const PI = 3.14`)
	assertBodyLen(t, prog, 1)
	decl := prog.Body[0].(*VarDecl)
	if decl.Kind != KindConst {
		t.Errorf("esperado const")
	}
	if _, ok := decl.Init.(*FloatLiteral); !ok {
		t.Errorf("esperado FloatLiteral")
	}
}

// ============================================================
// Function declarations
// ============================================================

func TestFuncDecl(t *testing.T) {
	prog := parseSource(t, `
fn soma(a: Int, b: Int) -> Int {
    return a
}`)
	assertBodyLen(t, prog, 1)

	fn := prog.Body[0].(*FuncDecl)
	if fn.Name != "soma" {
		t.Errorf("esperado nome=soma")
	}
	if len(fn.Params) != 2 {
		t.Errorf("esperado 2 params, obtido %d", len(fn.Params))
	}
	if fn.IsExprBody {
		t.Error("não deveria ser expr body")
	}
}

func TestFuncDeclExprBody(t *testing.T) {
	prog := parseSource(t, `fn dobrar(x: Int) -> Int = x`)
	assertBodyLen(t, prog, 1)
	fn := prog.Body[0].(*FuncDecl)
	if !fn.IsExprBody {
		t.Error("esperado IsExprBody=true")
	}
}

func TestFuncDeclPatternVariants(t *testing.T) {
	// Multiple declarations with the same name = pattern variants
	prog := parseSource(t, `
fn fatorial(0) -> Int = 1
fn fatorial(n: Int) -> Int = n`)
	assertBodyLen(t, prog, 2)

	f1 := prog.Body[0].(*FuncDecl)
	f2 := prog.Body[1].(*FuncDecl)
	if f1.Name != "fatorial" || f2.Name != "fatorial" {
		t.Error("esperado nome=fatorial em ambos")
	}
	// First param of f1 should be a LiteralPattern
	if _, ok := f1.Params[0].Pattern.(*LiteralPattern); !ok {
		t.Errorf("esperado LiteralPattern, obtido %T", f1.Params[0].Pattern)
	}
	// First param of f2 should be a BindingPattern
	if _, ok := f2.Params[0].Pattern.(*BindingPattern); !ok {
		t.Errorf("esperado BindingPattern, obtido %T", f2.Params[0].Pattern)
	}
}

func TestFuncWithSelfParam(t *testing.T) {
	prog := parseSource(t, `fn salvar(self) -> Unit { return () }`)
	assertBodyLen(t, prog, 1)
	fn := prog.Body[0].(*FuncDecl)
	bp, ok := fn.Params[0].Pattern.(*BindingPattern)
	if !ok || bp.Name != "self" {
		t.Error("esperado self como BindingPattern")
	}
}

func TestFuncWithGenerics(t *testing.T) {
	prog := parseSource(t, `fn identidade[T](x: T) -> T = x`)
	fn := prog.Body[0].(*FuncDecl)
	if len(fn.Generics) != 1 || fn.Generics[0].Name != "T" {
		t.Errorf("esperado um genérico T")
	}
}

func TestFuncWithGenericConstraint(t *testing.T) {
	prog := parseSource(t, `fn maior[T : Comparable](a: T, b: T) -> T = a`)
	fn := prog.Body[0].(*FuncDecl)
	if len(fn.Generics[0].Constraints) != 1 {
		t.Error("esperado uma constraint")
	}
}

// ============================================================
// Record declarations
// ============================================================

func TestRecordDecl(t *testing.T) {
	prog := parseSource(t, `
record Ponto {
    x: Float
    y: Float
}`)
	assertBodyLen(t, prog, 1)

	r := prog.Body[0].(*RecordDecl)
	if r.Name != "Ponto" {
		t.Errorf("esperado Ponto")
	}
	if len(r.Fields) != 2 {
		t.Errorf("esperado 2 campos, obtido %d", len(r.Fields))
	}
}

func TestGenericRecord(t *testing.T) {
	prog := parseSource(t, `record Par[A, B] { primeiro: A, segundo: B }`)
	r := prog.Body[0].(*RecordDecl)
	if len(r.Generics) != 2 {
		t.Errorf("esperado 2 genéricos")
	}
}

// ============================================================
// Class declarations
// ============================================================

func TestClassDecl(t *testing.T) {
	prog := parseSource(t, `
class Usuario : Persistivel {
    val nome: String
    var idade: Int

    fn salvar(self) -> Unit {
        return ()
    }
}`)
	assertBodyLen(t, prog, 1)

	c := prog.Body[0].(*ClassDecl)
	if c.Name != "Usuario" {
		t.Errorf("esperado Usuario")
	}
	if len(c.Interfaces) != 1 {
		t.Errorf("esperado 1 interface")
	}
	if len(c.Body) != 3 {
		t.Errorf("esperado 3 membros (val, var, fn), obtido %d", len(c.Body))
	}
}

// ============================================================
// Interface declarations
// ============================================================

func TestInterfaceDecl(t *testing.T) {
	prog := parseSource(t, `
interface Persistivel {
    fn salvar(self) -> Unit
    fn deletar(self, id: Int) -> Unit
}`)
	iface := prog.Body[0].(*InterfaceDecl)
	if len(iface.Methods) != 2 {
		t.Errorf("esperado 2 métodos")
	}
}

// ============================================================
// Enum declarations
// ============================================================

func TestEnumDecl(t *testing.T) {
	prog := parseSource(t, `
enum Forma {
    Circulo(raio: Float),
    Retangulo(w: Float, h: Float),
    Quadrado(lado: Float),
}`)
	e := prog.Body[0].(*EnumDecl)
	if len(e.Variants) != 3 {
		t.Errorf("esperado 3 variantes, obtido %d", len(e.Variants))
	}
}

// ============================================================
// Import declarations
// ============================================================

func TestImportSimple(t *testing.T) {
	prog := parseSource(t, `import parser.Lexer`)
	imp := prog.Body[0].(*ImportDecl)
	if len(imp.Path) != 2 {
		t.Errorf("esperado path len=2, obtido %d", len(imp.Path))
	}
}

func TestImportDestructured(t *testing.T) {
	prog := parseSource(t, `import parser.{ Token, TokenType }`)
	imp := prog.Body[0].(*ImportDecl)
	if len(imp.Names) != 2 {
		t.Errorf("esperado 2 names, obtido %d", len(imp.Names))
	}
}

func TestImportWildcard(t *testing.T) {
	prog := parseSource(t, `import parser.*`)
	imp := prog.Body[0].(*ImportDecl)
	if !imp.Wildcard {
		t.Error("esperado wildcard=true")
	}
}

// ============================================================
// Expressions
// ============================================================

func TestBinaryExpr(t *testing.T) {
	prog := parseSource(t, `val r = 1 + 2 * 3`)
	decl := prog.Body[0].(*VarDecl)

	// Should parse as 1 + (2 * 3) due to precedence
	add, ok := decl.Init.(*BinaryExpr)
	if !ok || add.Operator != "+" {
		t.Fatalf("esperado BinaryExpr(+), obtido %T", decl.Init)
	}
	mul, ok := add.Right.(*BinaryExpr)
	if !ok || mul.Operator != "*" {
		t.Fatalf("esperado BinaryExpr(*) no lado direito")
	}
}

func TestPipeExpr(t *testing.T) {
	prog := parseSource(t, `val r = buscar(1) |> validar |> transformar`)
	decl := prog.Body[0].(*VarDecl)

	// |> is left-associative: (buscar(1) |> validar) |> transformar
	outer, ok := decl.Init.(*PipeExpr)
	if !ok {
		t.Fatalf("esperado PipeExpr, obtido %T", decl.Init)
	}
	if _, ok := outer.Left.(*PipeExpr); !ok {
		t.Fatalf("esperado PipeExpr aninhado à esquerda")
	}
}

func TestMatchExpr(t *testing.T) {
	prog := parseSource(t, `
val msg = match resultado {
    Ok(v)  => "ok"
    Err(e) => "erro"
    _      => "outro"
}`)
	decl := prog.Body[0].(*VarDecl)
	m, ok := decl.Init.(*MatchExpr)
	if !ok {
		t.Fatalf("esperado *MatchExpr, obtido %T", decl.Init)
	}
	if len(m.Arms) != 3 {
		t.Errorf("esperado 3 arms, obtido %d", len(m.Arms))
	}
}

func TestMatchGuard(t *testing.T) {
	prog := parseSource(t, `
val desc = match n {
    x if x < 0 => "negativo"
    _           => "positivo"
}`)
	decl := prog.Body[0].(*VarDecl)
	m := decl.Init.(*MatchExpr)
	if m.Arms[0].Guard == nil {
		t.Error("esperado guard na primeira arm")
	}
}

func TestArrowFunc(t *testing.T) {
	prog := parseSource(t, `val dobrar = fn(x: Int) => x`)
	decl := prog.Body[0].(*VarDecl)
	if _, ok := decl.Init.(*ArrowFunc); !ok {
		t.Fatalf("esperado *ArrowFunc, obtido %T", decl.Init)
	}
}

func TestRecordLiteral(t *testing.T) {
	prog := parseSource(t, `val p = Ponto { x: 1, y: 2 }`)
	decl := prog.Body[0].(*VarDecl)
	rl, ok := decl.Init.(*RecordLiteral)
	if !ok {
		t.Fatalf("esperado *RecordLiteral, obtido %T", decl.Init)
	}
	if rl.Name != "Ponto" {
		t.Errorf("esperado name=Ponto")
	}
	if len(rl.Fields) != 2 {
		t.Errorf("esperado 2 campos")
	}
}

func TestListExpr(t *testing.T) {
	prog := parseSource(t, `val xs = [1, 2, 3]`)
	decl := prog.Body[0].(*VarDecl)
	ls, ok := decl.Init.(*ListExpr)
	if !ok {
		t.Fatalf("esperado *ListExpr, obtido %T", decl.Init)
	}
	if len(ls.Elements) != 3 {
		t.Errorf("esperado 3 elementos")
	}
}

func TestMapExpr(t *testing.T) {
	prog := parseSource(t, `val m = ["a": 1, "b": 2]`)
	decl := prog.Body[0].(*VarDecl)
	me, ok := decl.Init.(*MapExpr)
	if !ok {
		t.Fatalf("esperado *MapExpr, obtido %T", decl.Init)
	}
	if len(me.Entries) != 2 {
		t.Errorf("esperado 2 entradas")
	}
}

func TestTupleExpr(t *testing.T) {
	prog := parseSource(t, `val t = (1, "hello", true)`)
	decl := prog.Body[0].(*VarDecl)
	te, ok := decl.Init.(*TupleExpr)
	if !ok {
		t.Fatalf("esperado *TupleExpr, obtido %T", decl.Init)
	}
	if len(te.Elements) != 3 {
		t.Errorf("esperado 3 elementos")
	}
}

func TestSafeNavAndElvis(t *testing.T) {
	prog := parseSource(t, `val n = user?.nome ?: "anon"`)
	decl := prog.Body[0].(*VarDecl)
	ev, ok := decl.Init.(*ElvisExpr)
	if !ok {
		t.Fatalf("esperado *ElvisExpr, obtido %T", decl.Init)
	}
	if _, ok := ev.Left.(*SafeNavExpr); !ok {
		t.Fatalf("esperado *SafeNavExpr à esquerda do elvis")
	}
}

func TestOkErrSome(t *testing.T) {
	prog := parseSource(t, `
val a = Ok(42)
val b = Err(MeuErro { msg: "falhou" })
val c = Some("valor")
`)
	if _, ok := prog.Body[0].(*VarDecl).Init.(*OkExpr); !ok {
		t.Error("esperado OkExpr")
	}
	if _, ok := prog.Body[1].(*VarDecl).Init.(*ErrExpr); !ok {
		t.Error("esperado ErrExpr")
	}
	if _, ok := prog.Body[2].(*VarDecl).Init.(*SomeExpr); !ok {
		t.Error("esperado SomeExpr")
	}
}

func TestRangeExpr(t *testing.T) {
	prog := parseSource(t, `val r = 0..10`)
	decl := prog.Body[0].(*VarDecl)
	rng, ok := decl.Init.(*RangeExpr)
	if !ok {
		t.Fatalf("esperado *RangeExpr")
	}
	if rng.Inclusive {
		t.Error("não deveria ser inclusivo")
	}
}

func TestRangeInclusive(t *testing.T) {
	prog := parseSource(t, `val r = 0..=10`)
	decl := prog.Body[0].(*VarDecl)
	rng := decl.Init.(*RangeExpr)
	if !rng.Inclusive {
		t.Error("deveria ser inclusivo")
	}
}

// ============================================================
// Statements
// ============================================================

func TestIfElse(t *testing.T) {
	prog := parseSource(t, `
fn f(x: Int) {
    if x > 0 {
        return x
    } else {
        return x
    }
}`)
	fn := prog.Body[0].(*FuncDecl)
	block := fn.Body.(*BlockStmt)
	ifStmt, ok := block.Statements[0].(*IfStmt)
	if !ok {
		t.Fatalf("esperado *IfStmt")
	}
	if ifStmt.Alternate == nil {
		t.Error("esperado else")
	}
}

func TestForLoop(t *testing.T) {
	prog := parseSource(t, `
fn f() {
    for item in lista {
        return item
    }
}`)
	fn := prog.Body[0].(*FuncDecl)
	block := fn.Body.(*BlockStmt)
	fl, ok := block.Statements[0].(*ForStmt)
	if !ok {
		t.Fatalf("esperado *ForStmt")
	}
	if fl.Binding != "item" {
		t.Errorf("esperado binding=item")
	}
}

func TestLoopBreakValue(t *testing.T) {
	prog := parseSource(t, `
fn f() {
    val r = loop {
        break 42
    }
    return r
}`)
	fn := prog.Body[0].(*FuncDecl)
	block := fn.Body.(*BlockStmt)
	decl := block.Statements[0].(*VarDecl)
	ls, ok := decl.Init.(*LoopStmt)
	if !ok {
		t.Fatalf("esperado *LoopStmt, obtido %T", decl.Init)
	}
	brk, ok := ls.Body.Statements[0].(*BreakStmt)
	if !ok {
		t.Fatalf("esperado *BreakStmt")
	}
	if brk.Value == nil {
		t.Error("esperado valor no break")
	}
}

// ============================================================
// Types
// ============================================================

func TestOptionalType(t *testing.T) {
	prog := parseSource(t, `val x: String? = None`)
	decl := prog.Body[0].(*VarDecl)
	if _, ok := decl.Type.(*OptionalType); !ok {
		t.Fatalf("esperado *OptionalType, obtido %T", decl.Type)
	}
}

func TestGenericType(t *testing.T) {
	prog := parseSource(t, `val xs: List[Int] = []`)
	decl := prog.Body[0].(*VarDecl)
	gt, ok := decl.Type.(*GenericType)
	if !ok {
		t.Fatalf("esperado *GenericType")
	}
	if gt.Name != "List" || len(gt.Params) != 1 {
		t.Error("esperado List[Int]")
	}
}

func TestTupleType(t *testing.T) {
	prog := parseSource(t, `val t: (Int, String) = (1, "a")`)
	decl := prog.Body[0].(*VarDecl)
	if _, ok := decl.Type.(*TupleType); !ok {
		t.Fatalf("esperado *TupleType")
	}
}

func TestFuncType(t *testing.T) {
	prog := parseSource(t, `val f: (Int) -> Int = fn(x: Int) => x`)
	decl := prog.Body[0].(*VarDecl)
	if _, ok := decl.Type.(*FuncType); !ok {
		t.Fatalf("esperado *FuncType, obtido %T", decl.Type)
	}
}

// ============================================================
// Patterns
// ============================================================

func TestRecordPattern(t *testing.T) {
	prog := parseSource(t, `
fn distancia(Ponto { x: 0, y: 0 }) -> Float = 0.0
fn distancia(Ponto { x, y }) -> Float = x`)
	if len(prog.Body) != 2 {
		t.Fatalf("esperado 2 declarações")
	}
	f1 := prog.Body[0].(*FuncDecl)
	rp, ok := f1.Params[0].Pattern.(*RecordPattern)
	if !ok {
		t.Fatalf("esperado *RecordPattern, obtido %T", f1.Params[0].Pattern)
	}
	if rp.Name != "Ponto" {
		t.Errorf("esperado Ponto")
	}
}

func TestConstructorPatternInMatch(t *testing.T) {
	prog := parseSource(t, `
val r = match opt {
    Some(x) => x
    None    => 0
}`)
	decl := prog.Body[0].(*VarDecl)
	m := decl.Init.(*MatchExpr)

	cp, ok := m.Arms[0].Pattern.(*ConstructorPattern)
	if !ok {
		t.Fatalf("esperado *ConstructorPattern, obtido %T", m.Arms[0].Pattern)
	}
	if cp.Name != "Some" || len(cp.Args) != 1 {
		t.Error("esperado Some(x)")
	}

	none, ok := m.Arms[1].Pattern.(*ConstructorPattern)
	if !ok || none.Name != "None" {
		t.Error("esperado None")
	}
}

func TestRangePattern(t *testing.T) {
	prog := parseSource(t, `
val d = match n {
    1..9  => "pequeno"
    _     => "grande"
}`)
	decl := prog.Body[0].(*VarDecl)
	m := decl.Init.(*MatchExpr)
	if _, ok := m.Arms[0].Pattern.(*RangePattern); !ok {
		t.Fatalf("esperado *RangePattern")
	}
}

// ============================================================
// Pub modifier
// ============================================================

func TestPubDeclarations(t *testing.T) {
	prog := parseSource(t, `
pub val MAX = 100
pub fn exportada() -> Int = 1
pub record Config { debug: Bool }
pub class Service : Runnable { fn run(self) { return () } }
pub interface Runnable { fn run(self) }
pub enum Status { Ativo, Inativo }
`)
	for i, node := range prog.Body {
		switch n := node.(type) {
		case *VarDecl:
			if !n.Pub {
				t.Errorf("nó %d deveria ser pub", i)
			}
		case *FuncDecl:
			if !n.Pub {
				t.Errorf("nó %d deveria ser pub", i)
			}
		case *RecordDecl:
			if !n.Pub {
				t.Errorf("nó %d deveria ser pub", i)
			}
		case *ClassDecl:
			if !n.Pub {
				t.Errorf("nó %d deveria ser pub", i)
			}
		case *InterfaceDecl:
			if !n.Pub {
				t.Errorf("nó %d deveria ser pub", i)
			}
		case *EnumDecl:
			if !n.Pub {
				t.Errorf("nó %d deveria ser pub", i)
			}
		}
	}
}

// ============================================================
// Full example
// ============================================================

func TestFullExample(t *testing.T) {
	src := `
import collections.{ List }

record Produto {
    id: Int
    nome: String
    preco: Float
}

interface Estoque {
    fn adicionar(self, p: Produto) -> Unit
    fn buscar(self, id: Int) -> Produto?
}

class LojaVirtual : Estoque {
    var produtos: List[Produto] = []

    fn adicionar(self, p: Produto) -> Unit {
        return ()
    }

    fn buscar(self, id: Int) -> Produto? {
        return None
    }

    fn totalEstoque(self) -> Float {
        val r = self.produtos |> map(fn(p: Produto) => p.preco)
        return r
    }
}

fn main() {
    val loja = LojaVirtual { produtos: [] }
    val preco = loja.buscar(1)?.preco ?: 0.0
    return ()
}
`
	prog := parseSource(t, src)
	if len(prog.Body) == 0 {
		t.Error("esperado nós no body")
	}
}
