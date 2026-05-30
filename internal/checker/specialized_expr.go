package checker

import "soyuz/internal/parser"

func (c *Checker) checkSpecializedExpr(n *parser.SpecializedExpr) Type {
	baseType := c.checkNode(n.Base)
	specParams := c.typeArgsFromExprs(n.TypeArgs)
	if specParams == nil {
		return Unknown
	}

	switch bt := baseType.(type) {
	case *FuncType:
		if len(bt.Generics) == 0 {
			c.errorf(n.Pos(), "%s não é genérico", n.Base)
			return Unknown
		}
		if len(specParams) != len(bt.Generics) {
			c.errorf(n.Pos(), "esperado %d argumentos de tipo, encontrado %d", len(bt.Generics), len(specParams))
			return Unknown
		}
		return c.instantiateFunc(bt, specParams)
	case *RecordType:
		if len(bt.Generics) == 0 {
			c.errorf(n.Pos(), "%s não é genérico", n.Base)
			return Unknown
		}
		if len(specParams) != len(bt.Generics) {
			c.errorf(n.Pos(), "esperado %d argumentos de tipo, encontrado %d", len(bt.Generics), len(specParams))
			return Unknown
		}
		return &SpecializedType{Base: bt, Params: specParams}
	case *ClassType:
		if len(bt.Generics) == 0 {
			c.errorf(n.Pos(), "%s não é genérico", n.Base)
			return Unknown
		}
		if len(specParams) != len(bt.Generics) {
			c.errorf(n.Pos(), "esperado %d argumentos de tipo, encontrado %d", len(bt.Generics), len(specParams))
			return Unknown
		}
		return &SpecializedType{Base: bt, Params: specParams}
	case *EnumType:
		if len(bt.Generics) == 0 {
			c.errorf(n.Pos(), "%s não é genérico", n.Base)
			return Unknown
		}
		if len(specParams) != len(bt.Generics) {
			c.errorf(n.Pos(), "esperado %d argumentos de tipo, encontrado %d", len(bt.Generics), len(specParams))
			return Unknown
		}
		return &SpecializedType{Base: bt, Params: specParams}
	default:
		if sym, ok := n.Base.(*parser.Identifier); ok {
			c.errorf(n.Pos(), "tipo explícito inválido para %s", sym.Name)
		}
		return Unknown
	}
}

func (c *Checker) typeArgsFromExprs(args []parser.TypeExpr) []Type {
	if len(args) == 0 {
		return nil
	}
	out := make([]Type, len(args))
	for i, te := range args {
		out[i] = c.resolveTypeExpr(te)
	}
	return out
}
