package parser

import (
	"strings"

	"soyuz/internal/lexer"
)

// Node is the base interface for every AST node.
type Node interface {
	Pos() lexer.Position
}

// ============================================================
// Program
// ============================================================

type Program struct {
	pos  lexer.Position
	Body []Node
}

func (p *Program) Pos() lexer.Position { return p.pos }

// ============================================================
// Declarations
// ============================================================

type VarKind string

const (
	KindVal VarKind = "val"
	KindVar VarKind = "var"
)

type VarDecl struct {
	pos     lexer.Position
	NamePos lexer.Position
	Pub     bool
	Kind    VarKind
	Name    string  // empty when Pattern is set
	Pattern Pattern // set for destructuring: val (x, y) = ...
	Type    TypeExpr // nil = inferred
	Init    Node
}

func (v *VarDecl) Pos() lexer.Position { return v.pos }

// FuncDecl covers both regular and pattern-matched function declarations.
// Multiple FuncDecls with the same name are pattern variants — the type
// checker groups them and enforces specificity ordering.
type FuncDecl struct {
	pos        lexer.Position
	NamePos    lexer.Position
	Pub        bool
	Name       string
	Generics   []GenericParam
	Params     []FuncParam
	WhenGuard  Node     // when expr (nil = no guard)
	ReturnType TypeExpr // nil = Unit
	Body       Node     // *BlockStmt or expression (IsExprBody = true)
	IsExprBody bool     // fn f(x) -> T = expr  (sugar for { return expr })
}

func (f *FuncDecl) Pos() lexer.Position { return f.pos }

type GenericParam struct {
	Name        string
	Constraints []TypeExpr // T : A + B
}

type FuncParam struct {
	Pos     lexer.Position
	Pattern Pattern
	Type    TypeExpr // nil = inferred from pattern
	Default Node     // nil = required parameter; non-nil = default value expression
}

type RecordDecl struct {
	pos      lexer.Position
	Pub      bool
	Name     string
	Generics []GenericParam
	Fields   []RecordField
}

func (r *RecordDecl) Pos() lexer.Position { return r.pos }

type RecordField struct {
	Pos  lexer.Position
	Name string
	Type TypeExpr
}

type ClassDecl struct {
	pos        lexer.Position
	Pub        bool
	Name       string
	Generics   []GenericParam
	Interfaces []TypeExpr
	Body       []Node // FuncDecl | VarDecl
}

func (c *ClassDecl) Pos() lexer.Position { return c.pos }

type InterfaceDecl struct {
	pos      lexer.Position
	Pub      bool
	Name     string
	Generics []GenericParam
	Methods  []InterfaceMethod
}

func (i *InterfaceDecl) Pos() lexer.Position { return i.pos }

type InterfaceMethod struct {
	Pos        lexer.Position
	Pub        bool
	Name       string
	Params     []FuncParam
	ReturnType TypeExpr
}

type EnumDecl struct {
	pos      lexer.Position
	Pub      bool
	Name     string
	Generics []GenericParam
	Variants []EnumVariant
}

func (e *EnumDecl) Pos() lexer.Position { return e.pos }

type EnumVariant struct {
	Pos    lexer.Position
	Name   string
	Fields []EnumField
}

type EnumField struct {
	Name string // empty = positional
	Type TypeExpr
}

type ExtendDecl struct {
	pos      lexer.Position
	TypeName string
	Methods  []*FuncDecl
}

func (e *ExtendDecl) Pos() lexer.Position { return e.pos }

type ImportPathKind int

const (
	ImportPathLegacy ImportPathKind = iota
	ImportPathStdlib
	ImportPathProjectRoot // @/...
	ImportPathPackageAlias // @alias/...
	ImportPathRelative     // ./ or ../
)

type ImportDecl struct {
	pos           lexer.Position
	Path          string // canonical: "@soyuz/fs", "@/lib/lexer", "@lexer/tokens", "./local"
	Names         []ImportName
	Namespace     string
	PathKind      ImportPathKind
	PackageAlias  string // set when PathKind == ImportPathPackageAlias
	IsStdlib      bool   // deprecated: use PathKind; kept for compat
	ResolvedFiles []string
}

func (i *ImportDecl) Pos() lexer.Position { return i.pos }

// PathSegments returns filesystem path segments for module resolution (without alias/stdlib prefix).
func (i *ImportDecl) PathSegments() []string {
	switch i.PathKind {
	case ImportPathStdlib:
		p := strings.TrimPrefix(i.Path, "@soyuz/")
		if p == "" {
			return nil
		}
		return strings.Split(p, "/")
	case ImportPathProjectRoot:
		p := strings.TrimPrefix(i.Path, "@/")
		if p == "" {
			return nil
		}
		return strings.Split(p, "/")
	case ImportPathPackageAlias:
		p := i.Path
		if strings.HasPrefix(p, "@") {
			p = p[1:]
		}
		if i.PackageAlias != "" {
			p = strings.TrimPrefix(p, i.PackageAlias+"/")
		}
		if p == "" {
			return nil
		}
		return strings.Split(p, "/")
	case ImportPathRelative:
		return strings.Split(i.Path, "/")
	default:
		if strings.HasPrefix(i.Path, "@soyuz/") {
			p := strings.TrimPrefix(i.Path, "@soyuz/")
			return strings.Split(p, "/")
		}
		return strings.Split(i.Path, "/")
	}
}

// IsModuleImport reports whether this imports the whole module as a namespace.
func (i *ImportDecl) IsModuleImport() bool { return len(i.Names) == 0 }

// ExternDecl declares a C function available for FFI: extern fn name(params) -> RetType
type ExternDecl struct {
	pos        lexer.Position
	Pub        bool
	Name       string
	Params     []FuncParam
	ReturnType TypeExpr // nil = Unit/void
}

func (e *ExternDecl) Pos() lexer.Position { return e.pos }

type ImportName struct {
	Name  string
	Alias string // empty = no alias
}

// ============================================================
// Statements
// ============================================================

type BlockStmt struct {
	pos        lexer.Position
	Statements []Node
}

func (b *BlockStmt) Pos() lexer.Position { return b.pos }

type ReturnStmt struct {
	pos   lexer.Position
	Value Node // nil = bare return
}

func (r *ReturnStmt) Pos() lexer.Position { return r.pos }

type BreakStmt struct {
	pos   lexer.Position
	Value Node // nil = bare break; non-nil = break expr (loop value)
}

func (b *BreakStmt) Pos() lexer.Position { return b.pos }

type ContinueStmt struct {
	pos lexer.Position
}

func (c *ContinueStmt) Pos() lexer.Position { return c.pos }

type IfStmt struct {
	pos        lexer.Position
	Condition  Node
	Consequent *BlockStmt
	Alternate  Node // *BlockStmt | *IfStmt | nil
}

func (i *IfStmt) Pos() lexer.Position { return i.pos }

type ForStmt struct {
	pos      lexer.Position
	Binding  string
	Iterable Node
	Body     *BlockStmt
}

func (f *ForStmt) Pos() lexer.Position { return f.pos }

type WhileStmt struct {
	pos       lexer.Position
	Condition Node
	Body      *BlockStmt
}

func (w *WhileStmt) Pos() lexer.Position { return w.pos }

type LoopStmt struct {
	pos  lexer.Position
	Body *BlockStmt
}

func (l *LoopStmt) Pos() lexer.Position { return l.pos }

// ExprStmt wraps an expression used as a statement.
type ExprStmt struct {
	pos  lexer.Position
	Expr Node
}

func (e *ExprStmt) Pos() lexer.Position { return e.pos }

// ============================================================
// Expressions
// ============================================================

type Identifier struct {
	pos  lexer.Position
	Name string
}

func (i *Identifier) Pos() lexer.Position { return i.pos }

type SelfExpr struct {
	pos lexer.Position
}

func (s *SelfExpr) Pos() lexer.Position { return s.pos }

type IntLiteral struct {
	pos   lexer.Position
	Value string
}

func (i *IntLiteral) Pos() lexer.Position { return i.pos }

type FloatLiteral struct {
	pos   lexer.Position
	Value string
}

func (f *FloatLiteral) Pos() lexer.Position { return f.pos }

type StringLiteral struct {
	pos   lexer.Position
	Value string
}

func (s *StringLiteral) Pos() lexer.Position { return s.pos }

// InterpolatedString holds a mix of string parts and interpolated expressions.
// e.g. "olá $(nome)!" → [StringLiteral{"olá "}, Identifier{"nome"}, StringLiteral{"!"}]
type InterpolatedString struct {
	pos   lexer.Position
	Parts []Node
}

func (i *InterpolatedString) Pos() lexer.Position { return i.pos }

type BoolLiteral struct {
	pos   lexer.Position
	Value bool
}

func (b *BoolLiteral) Pos() lexer.Position { return b.pos }

type CharLiteral struct {
	pos   lexer.Position
	Value rune
}

func (c *CharLiteral) Pos() lexer.Position { return c.pos }

type NoneLiteral struct {
	pos lexer.Position
}

func (n *NoneLiteral) Pos() lexer.Position { return n.pos }

type BinaryExpr struct {
	pos      lexer.Position
	Operator string
	Left     Node
	Right    Node
}

func (b *BinaryExpr) Pos() lexer.Position { return b.pos }

type UnaryExpr struct {
	pos      lexer.Position
	Operator string
	Operand  Node
}

func (u *UnaryExpr) Pos() lexer.Position { return u.pos }

type AssignExpr struct {
	pos   lexer.Position
	Left  Node
	Right Node
}

func (a *AssignExpr) Pos() lexer.Position { return a.pos }

type CallExpr struct {
	pos    lexer.Position
	Callee Node
	Args   []Node
}

func (c *CallExpr) Pos() lexer.Position { return c.pos }

type MemberExpr struct {
	pos      lexer.Position
	Object   Node
	Property string
}

func (m *MemberExpr) Pos() lexer.Position { return m.pos }

type SafeNavExpr struct {
	pos      lexer.Position
	Object   Node
	Property string
}

func (s *SafeNavExpr) Pos() lexer.Position { return s.pos }

type ElvisExpr struct {
	pos   lexer.Position
	Left  Node
	Right Node
}

func (e *ElvisExpr) Pos() lexer.Position { return e.pos }

type PipeExpr struct {
	pos   lexer.Position
	Left  Node
	Right Node
}

func (p *PipeExpr) Pos() lexer.Position { return p.pos }

type PipeQuestExpr struct {
	pos   lexer.Position
	Left  Node
	Right Node
}

func (p *PipeQuestExpr) Pos() lexer.Position { return p.pos }

type IndexExpr struct {
	pos    lexer.Position
	Object Node
	Index  Node
}

func (i *IndexExpr) Pos() lexer.Position { return i.pos }

type RangeExpr struct {
	pos       lexer.Position
	From      Node
	To        Node
	Inclusive bool
}

func (r *RangeExpr) Pos() lexer.Position { return r.pos }

type TupleExpr struct {
	pos      lexer.Position
	Elements []Node // nil = unit ()
}

func (t *TupleExpr) Pos() lexer.Position { return t.pos }

type ListExpr struct {
	pos      lexer.Position
	Elements []Node
}

func (l *ListExpr) Pos() lexer.Position { return l.pos }

type MapExpr struct {
	pos     lexer.Position
	Entries []MapEntry
}

func (m *MapExpr) Pos() lexer.Position { return m.pos }

type MapEntry struct {
	Key   Node
	Value Node
}

type OkExpr struct {
	pos   lexer.Position
	Value Node
}

func (o *OkExpr) Pos() lexer.Position { return o.pos }

type ErrExpr struct {
	pos   lexer.Position
	Value Node
}

func (e *ErrExpr) Pos() lexer.Position { return e.pos }

type SomeExpr struct {
	pos   lexer.Position
	Value Node
}

func (s *SomeExpr) Pos() lexer.Position { return s.pos }

// RecordLiteral represents Ponto { x: 1.0, y: 2.0 }
type RecordLiteral struct {
	pos    lexer.Position
	Name   string
	Fields []RecordLiteralField
}

func (r *RecordLiteral) Pos() lexer.Position { return r.pos }

type RecordLiteralField struct {
	Pos   lexer.Position
	Name  string
	Value Node
}

type MatchExpr struct {
	pos     lexer.Position
	Subject Node
	Arms    []MatchArm
}

func (m *MatchExpr) Pos() lexer.Position { return m.pos }

type MatchArm struct {
	Pos     lexer.Position
	Pattern Pattern
	Guard   Node // if condition (nil = no guard)
	Body    Node // expression or *BlockStmt
}

// ArrowFunc is an anonymous function: fn(x: Int) => x * 2
type ArrowFunc struct {
	pos        lexer.Position
	Params     []FuncParam
	ReturnType TypeExpr // nil = inferred
	Body       Node
}

func (a *ArrowFunc) Pos() lexer.Position { return a.pos }

// ============================================================
// Type Expressions
// ============================================================

type TypeExpr interface {
	typeNode()
	TypePos() lexer.Position
}

type NamedType struct {
	pos  lexer.Position
	Name string
}

func (n *NamedType) typeNode()               {}
func (n *NamedType) TypePos() lexer.Position { return n.pos }

// GenericType: List[T], Map[K, V], Result[T]
type GenericType struct {
	pos    lexer.Position
	Name   string
	Params []TypeExpr
}

func (g *GenericType) typeNode()               {}
func (g *GenericType) TypePos() lexer.Position { return g.pos }

// OptionalType: T?
type OptionalType struct {
	pos   lexer.Position
	Inner TypeExpr
}

func (o *OptionalType) typeNode()               {}
func (o *OptionalType) TypePos() lexer.Position { return o.pos }

// TupleType: (A, B, C)
type TupleType struct {
	pos      lexer.Position
	Elements []TypeExpr
}

func (t *TupleType) typeNode()               {}
func (t *TupleType) TypePos() lexer.Position { return t.pos }

// FuncType: (A, B) -> C
type FuncType struct {
	pos        lexer.Position
	ParamTypes []TypeExpr
	ReturnType TypeExpr
}

func (f *FuncType) typeNode()               {}
func (f *FuncType) TypePos() lexer.Position { return f.pos }

// ============================================================
// Patterns
// ============================================================

type Pattern interface {
	patternNode()
	PatternPos() lexer.Position
}

// _ wildcard
type WildcardPattern struct {
	pos lexer.Position
}

func (w *WildcardPattern) patternNode()               {}
func (w *WildcardPattern) PatternPos() lexer.Position { return w.pos }

// binding: n, x, err
type BindingPattern struct {
	pos  lexer.Position
	Name string
}

func (b *BindingPattern) patternNode()               {}
func (b *BindingPattern) PatternPos() lexer.Position { return b.pos }

// literal: 0, "hello", true
type LiteralPattern struct {
	pos   lexer.Position
	Value Node
}

func (l *LiteralPattern) patternNode()               {}
func (l *LiteralPattern) PatternPos() lexer.Position { return l.pos }

// constructor: Some(x), Ok(v), Err(e), None, Variant(a, b)
type ConstructorPattern struct {
	pos  lexer.Position
	Name string
	Args []Pattern
}

func (c *ConstructorPattern) patternNode()               {}
func (c *ConstructorPattern) PatternPos() lexer.Position { return c.pos }

// record: Ponto { x: 0, y } or Ponto { x, y }
type RecordPattern struct {
	pos    lexer.Position
	Name   string
	Fields []RecordPatternField
}

func (r *RecordPattern) patternNode()               {}
func (r *RecordPattern) PatternPos() lexer.Position { return r.pos }

type RecordPatternField struct {
	Name    string
	Pattern Pattern // nil = shorthand  { x }  ≡  { x: x }
}

// tuple: (a, b, c)
type TuplePattern struct {
	pos      lexer.Position
	Elements []Pattern
}

func (t *TuplePattern) patternNode()               {}
func (t *TuplePattern) PatternPos() lexer.Position { return t.pos }

// range: 1..9, 0..=100
type RangePattern struct {
	pos       lexer.Position
	From      Node
	To        Node
	Inclusive bool
}

func (r *RangePattern) patternNode()               {}
func (r *RangePattern) PatternPos() lexer.Position { return r.pos }
