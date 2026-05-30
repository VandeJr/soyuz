package checker

import "soyuz/internal/parser"

func (c *Checker) variantFieldNamesForCall(callee parser.Node) []string {
	switch ce := callee.(type) {
	case *parser.MemberExpr:
		objType := c.nodeTypes[ce.Object]
		if objType == nil {
			return nil
		}
		if et, ok := objType.(*EnumType); ok {
			if names, ok := et.VariantFieldNames[ce.Property]; ok {
				return names
			}
		}
	case *parser.Identifier:
		if sym, ok := c.scope.Resolve(ce.Name); ok {
			if ft, ok := sym.Type.(*FuncType); ok {
				return c.fieldNamesFromFuncReturn(ft, ce.Name)
			}
		}
	}
	return nil
}

func (c *Checker) fieldNamesFromFuncReturn(ft *FuncType, variantName string) []string {
	switch ret := ft.Return.(type) {
	case *EnumType:
		if names, ok := ret.VariantFieldNames[variantName]; ok {
			return names
		}
	case *SpecializedType:
		if et, ok := ret.Base.(*EnumType); ok {
			if names, ok := et.VariantFieldNames[variantName]; ok {
				return names
			}
		}
	}
	return nil
}

func (c *Checker) resolveNamedCallArgs(n *parser.CallExpr, ft *FuncType, fieldNames []string) {
	if !hasNamedArg(n.Args) {
		return
	}
	if len(fieldNames) == 0 {
		c.errorf(n.Pos(), "argumentos nomeados não suportados nesta chamada")
		return
	}
	if len(fieldNames) != len(ft.Params) {
		c.errorf(n.Pos(), "número de campos do construtor não corresponde aos parâmetros")
		return
	}

	named := make(map[string]parser.Node)
	var positional []parser.Node
	for _, arg := range n.Args {
		if na, ok := arg.(*parser.NamedArg); ok {
			if _, exists := named[na.Name]; exists {
				c.errorf(na.Pos(), "argumento duplicado: %s", na.Name)
				continue
			}
			named[na.Name] = na.Value
		} else {
			positional = append(positional, arg)
		}
	}
	if len(positional) > 0 && len(named) > 0 {
		c.errorf(n.Pos(), "não misture argumentos posicionais e nomeados")
		return
	}

	ordered := make([]parser.Node, len(ft.Params))
	if len(positional) > 0 {
		if len(positional) != len(ft.Params) {
			c.errorf(n.Pos(), "esperado %d argumentos posicionais, encontrado %d", len(ft.Params), len(positional))
			return
		}
		copy(ordered, positional)
	} else {
		for i, name := range fieldNames {
			if name == "" {
				c.errorf(n.Pos(), "argumento posicional obrigatório na posição %d", i+1)
				return
			}
			val, ok := named[name]
			if !ok {
				c.errorf(n.Pos(), "argumento ausente: %s", name)
				continue
			}
			ordered[i] = val
			delete(named, name)
		}
		for name := range named {
			c.errorf(n.Pos(), "argumento desconhecido: %s", name)
		}
	}
	n.Args = ordered
}

func hasNamedArg(args []parser.Node) bool {
	for _, a := range args {
		if _, ok := a.(*parser.NamedArg); ok {
			return true
		}
	}
	return false
}
