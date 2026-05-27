package checker

import (
	"fmt"
	"strings"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

type Type interface {
	String() string
}

type BasicType struct {
	Name string
}

func (b *BasicType) String() string { return b.Name }

type FuncType struct {
	Params     []Type
	Return     Type
	Generics   []string
	Defaults   []parser.Node // parallel to Params; nil entry = required parameter
	IsOptional []bool        // parallel to Params; true = T? (auto-wrap/None injection)
}

func (f *FuncType) String() string {
	gen := ""
	if len(f.Generics) > 0 {
		gen = "[" + strings.Join(f.Generics, ", ") + "]"
	}
	paramStrs := make([]string, len(f.Params))
	for i, p := range f.Params {
		if i < len(f.IsOptional) && f.IsOptional[i] {
			if st, ok := p.(*SpecializedType); ok && len(st.Params) == 1 {
				paramStrs[i] = st.Params[0].String() + "?"
			} else {
				paramStrs[i] = p.String() + "?"
			}
		} else {
			paramStrs[i] = p.String()
		}
	}
	return gen + "(" + strings.Join(paramStrs, ", ") + ") -> " + f.Return.String()
}

type RecordType struct {
	Name     string
	Fields   map[string]Type
	Generics []string
}

func (r *RecordType) String() string { return r.Name }

type EnumType struct {
	Name     string
	Variants map[string][]Type // VariantName -> FieldTypes
	Generics []string
}

func (e *EnumType) String() string { return e.Name }

type InterfaceType struct {
	Name    string
	Methods map[string]*FuncType
}

func (i *InterfaceType) String() string { return i.Name }

type ClassType struct {
	Name       string
	Fields     map[string]Type
	FieldPub   map[string]bool        // true = accessible outside the class
	FieldInit  map[string]parser.Node // default init expression (nil = required)
	Methods    map[string][]*FuncType // may have multiple variants (overloaded by arity)
	MethodPub  map[string]bool        // true = accessible outside the class
	Implements []*InterfaceType
	Generics   []string
}

func (c *ClassType) String() string { return c.Name }

// FindMethod returns the FuncType variant matching the given arity (-1 = return first).
func (c *ClassType) FindMethod(name string, arity int) *FuncType {
	variants, ok := c.Methods[name]
	if !ok || len(variants) == 0 {
		return nil
	}
	if arity < 0 || len(variants) == 1 {
		return variants[0]
	}
	for _, ft := range variants {
		if len(ft.Params) == arity {
			return ft
		}
	}
	return variants[0]
}

type TupleType struct {
	Elements []Type
}

func (t *TupleType) String() string {
	if len(t.Elements) == 0 {
		return "Unit"
	}
	parts := make([]string, len(t.Elements))
	for i, e := range t.Elements {
		parts[i] = e.String()
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

type TypeParameter struct {
	Name string
}

func (t *TypeParameter) String() string { return t.Name }

type SpecializedType struct {
	Base   Type
	Params []Type
}

func (s *SpecializedType) String() string {
	paramStrs := make([]string, len(s.Params))
	for i, p := range s.Params {
		paramStrs[i] = p.String()
	}
	return s.Base.String() + "[" + strings.Join(paramStrs, ", ") + "]"
}

var (
	IntType    = &BasicType{Name: "Int"}
	FloatType  = &BasicType{Name: "Float"}
	BoolType   = &BasicType{Name: "Bool"}
	StringType = &BasicType{Name: "String"}
	CharType   = &BasicType{Name: "Char"}
	UnitType   = &BasicType{Name: "Unit"}
	Unknown    = &BasicType{Name: "Unknown"}
)

type TypeError struct {
	Pos     lexer.Position
	End     lexer.Position
	File    string // source file; empty in single-file mode
	Code    string
	Message string
}

func (e TypeError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("[%s %v]: %s", e.File, e.Pos, e.Message)
	}
	return fmt.Sprintf("%v: %s", e.Pos, e.Message)
}

// TypeWarning is a non-fatal diagnostic (e.g., must-use violations).
type TypeWarning struct {
	Pos     lexer.Position
	End     lexer.Position
	File    string
	Code    string
	Message string
}

func (w TypeWarning) String() string {
	if w.File != "" {
		return fmt.Sprintf("[%s %v]: aviso: %s", w.File, w.Pos, w.Message)
	}
	return fmt.Sprintf("%v: aviso: %s", w.Pos, w.Message)
}

type Scope struct {
	Parent  *Scope
	Symbols map[string]Symbol
}

type Symbol struct {
	Type     Type
	IsReadonly bool
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Parent:  parent,
		Symbols: make(map[string]Symbol),
	}
}

func (s *Scope) Define(name string, t Type, readonly bool) {
	s.Symbols[name] = Symbol{Type: t, IsReadonly: readonly}
}

func (s *Scope) Resolve(name string) (Symbol, bool) {
	sym, ok := s.Symbols[name]
	if !ok && s.Parent != nil {
		return s.Parent.Resolve(name)
	}
	return sym, ok
}
