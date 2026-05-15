package checker

import "soyuz/internal/parser"

func (c *Checker) inferGenerics(f *FuncType, args []parser.Node) []Type {
	inferred := make(map[string]Type)
	for i, arg := range args {
		if i >= len(f.Params) {
			break
		}
		argType := c.checkNode(arg)
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

func (c *Checker) unify(param, actual Type, inferred map[string]Type) {
	if tp, ok := param.(*TypeParameter); ok {
		if _, exists := inferred[tp.Name]; !exists {
			inferred[tp.Name] = actual
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
	return t
}
