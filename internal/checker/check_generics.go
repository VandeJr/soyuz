package checker

import "soyuz/internal/parser"

func (c *Checker) inferGenerics(f *FuncType, args []parser.Node) []Type {
	inferred := make(map[string]Type)
	for i, arg := range args {
		if i >= len(f.Params) {
			break
		}
		argType := c.inferExprType(arg)
		c.unify(f.Params[i], argType, inferred)
	}

	var results []Type
	for _, name := range f.Generics {
		if t, ok := inferred[name]; ok {
			results = append(results, t)
		} else {
			results = append(results, Unknown)
		}
	}
	return results
}

// inferExprType returns a type for generic inference without fully checking nested generic calls.
func (c *Checker) inferExprType(n parser.Node) Type {
	if t, ok := c.nodeTypes[n]; ok && t != Unknown {
		return t
	}
	switch n := n.(type) {
	case *parser.IntLiteral:
		return IntType
	case *parser.FloatLiteral:
		return FloatType
	case *parser.BoolLiteral:
		return BoolType
	case *parser.StringLiteral:
		return StringType
	case *parser.CharLiteral:
		return CharType
	case *parser.NamedArg:
		return c.inferExprType(n.Value)
	case *parser.Identifier:
		if sym, ok := c.scope.Resolve(n.Name); ok {
			return sym.Type
		}
		return Unknown
	case *parser.MemberExpr:
		objType := c.inferExprType(n.Object)
		return c.resolveMemberType(objType, n.Property, n.Pos())
	case *parser.SpecializedExpr:
		baseType := c.inferExprType(n.Base)
		specParams := c.typeArgsFromExprs(n.TypeArgs)
		if specParams == nil {
			return Unknown
		}
		switch bt := baseType.(type) {
		case *FuncType:
			if len(specParams) != len(bt.Generics) {
				return Unknown
			}
			return c.instantiateFunc(bt, specParams)
		case *RecordType:
			return &SpecializedType{Base: bt, Params: specParams}
		case *ClassType:
			return &SpecializedType{Base: bt, Params: specParams}
		case *EnumType:
			return &SpecializedType{Base: bt, Params: specParams}
		default:
			return Unknown
		}
	case *parser.CallExpr:
		var calleeType Type
		if se, ok := n.Callee.(*parser.SpecializedExpr); ok {
			calleeType = c.inferExprType(se)
		} else {
			calleeType = c.inferExprType(n.Callee)
		}
		if f, ok := calleeType.(*FuncType); ok {
			if len(f.Generics) > 0 && len(n.Args) > 0 {
				spec := c.inferGenerics(f, n.Args)
				return c.instantiateFunc(f, spec).Return
			}
			return f.Return
		}
		return Unknown
	default:
		return c.checkNode(n)
	}
}

func (c *Checker) unify(param, actual Type, inferred map[string]Type) {
	if tp, ok := param.(*TypeParameter); ok {
		if _, exists := inferred[tp.Name]; !exists {
			inferred[tp.Name] = actual
		}
		return
	}
	if tt, ok := param.(*TupleType); ok {
		if ta, ok := actual.(*TupleType); ok && len(tt.Elements) == len(ta.Elements) {
			for i, p := range tt.Elements {
				c.unify(p, ta.Elements[i], inferred)
			}
		}
		return
	}
	if sp, ok := param.(*SpecializedType); ok {
		if sa, ok := actual.(*SpecializedType); ok && sp.Base.String() == sa.Base.String() {
			for i, p := range sp.Params {
				if i < len(sa.Params) {
					c.unify(p, sa.Params[i], inferred)
				}
			}
		}
	}
}

func (c *Checker) instantiateFunc(f *FuncType, specParams []Type) *FuncType {
	if len(specParams) == 0 {
		return f
	}
	sub := make(map[string]Type)
	for i, name := range f.Generics {
		if i < len(specParams) {
			sub[name] = specParams[i]
		}
	}
	newParams := make([]Type, len(f.Params))
	for i, p := range f.Params {
		newParams[i] = c.substitute(p, sub)
	}
	return &FuncType{Params: newParams, Return: c.substitute(f.Return, sub)}
}

func (c *Checker) substitute(t Type, sub map[string]Type) Type {
	if tp, ok := t.(*TypeParameter); ok {
		if s, ok := sub[tp.Name]; ok {
			return s
		}
	}
	if st, ok := t.(*SpecializedType); ok {
		newParams := make([]Type, len(st.Params))
		for i, p := range st.Params {
			newParams[i] = c.substitute(p, sub)
		}
		return &SpecializedType{Base: st.Base, Params: newParams}
	}
	if tt, ok := t.(*TupleType); ok {
		newElems := make([]Type, len(tt.Elements))
		for i, e := range tt.Elements {
			newElems[i] = c.substitute(e, sub)
		}
		return &TupleType{Elements: newElems}
	}
	return t
}
