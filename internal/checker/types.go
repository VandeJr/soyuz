package checker

import (
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
	Params   []Type
	Return   Type
	Generics []string
	Defaults []parser.Node // parallel to Params; nil entry = required parameter
}

func (f *FuncType) String() string {
	gen := ""
	if len(f.Generics) > 0 {
		gen = "[" + strings.Join(f.Generics, ", ") + "]"
	}
	paramStrs := make([]string, len(f.Params))
	for i, p := range f.Params {
		paramStrs[i] = p.String()
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
	Methods    map[string]*FuncType
	Implements []*InterfaceType
	Generics   []string
}

func (c *ClassType) String() string { return c.Name }

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
	UnitType   = &BasicType{Name: "Unit"}
	Unknown    = &BasicType{Name: "Unknown"}
)

type TypeError struct {
	Pos     lexer.Position
	Message string
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
