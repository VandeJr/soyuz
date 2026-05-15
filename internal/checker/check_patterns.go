package checker

import "soyuz/internal/parser"

func (c *Checker) checkPattern(pat parser.Pattern, subjectType Type) {
	switch p := pat.(type) {
	case *parser.WildcardPattern:
		// nothing to bind
	case *parser.BindingPattern:
		c.scope.Define(p.Name, subjectType, true)
	case *parser.LiteralPattern:
		litType := c.checkNode(p.Value)
		if !c.isAssignable(subjectType, litType) {
			c.errorf(p.PatternPos(), "literal pattern incompatible with subject type: %s and %s", subjectType, litType)
		}
	case *parser.ConstructorPattern:
		c.checkConstructorPattern(p, subjectType)
	case *parser.RecordPattern:
		c.checkRecordPattern(p, subjectType)
	case *parser.RangePattern:
		fromType := c.checkNode(p.From)
		toType := c.checkNode(p.To)
		if fromType != IntType || toType != IntType {
			c.errorf(p.PatternPos(), "range pattern only supported for Int, got %s and %s", fromType, toType)
		}
	case *parser.TuplePattern:
		tt, ok := subjectType.(*TupleType)
		if !ok {
			c.errorf(p.PatternPos(), "tuple pattern requer um tuple, obteve %s", subjectType)
			return
		}
		if len(p.Elements) != len(tt.Elements) {
			c.errorf(p.PatternPos(), "tuple pattern tem %d elementos, mas valor tem %d", len(p.Elements), len(tt.Elements))
			return
		}
		for i, elem := range p.Elements {
			c.checkPattern(elem, tt.Elements[i])
		}
	}
}

func (c *Checker) checkConstructorPattern(p *parser.ConstructorPattern, subjectType Type) {
	if p.Name == "Some" || p.Name == "None" || p.Name == "Ok" || p.Name == "Err" {
		for _, arg := range p.Args {
			c.checkPattern(arg, Unknown)
		}
		return
	}

	// Build type-param substitution when subject is a SpecializedType (e.g. Maybe[Int]).
	var typeSub map[string]Type
	unwrapped := subjectType
	if st, ok := subjectType.(*SpecializedType); ok {
		unwrapped = st.Base
		if et2, ok := st.Base.(*EnumType); ok && len(et2.Generics) == len(st.Params) {
			typeSub = make(map[string]Type)
			for i, name := range et2.Generics {
				typeSub[name] = st.Params[i]
			}
		}
	}

	et, ok := unwrapped.(*EnumType)
	if !ok {
		if sym, ok := c.scope.Resolve(p.Name); ok {
			if ft, ok := sym.Type.(*FuncType); ok {
				if enumType, ok := ft.Return.(*EnumType); ok {
					et = enumType
				} else if st, ok := ft.Return.(*SpecializedType); ok {
					if enumType, ok := st.Base.(*EnumType); ok {
						et = enumType
					}
				}
			} else if enumType, ok := sym.Type.(*EnumType); ok {
				et = enumType
			}
		}
		if et == nil {
			c.errorf(p.PatternPos(), "constructor pattern %s requires an Enum, got %s", p.Name, subjectType)
			return
		}
	}

	fieldTypes, ok := et.Variants[p.Name]
	if !ok {
		c.errorf(p.PatternPos(), "variant %s not found in enum %s", p.Name, et.Name)
		return
	}
	if len(p.Args) != len(fieldTypes) {
		c.errorf(p.PatternPos(), "incorrect number of arguments for variant %s: expected %d, got %d",
			p.Name, len(fieldTypes), len(p.Args))
		return
	}
	for i, arg := range p.Args {
		ft := fieldTypes[i]
		if typeSub != nil {
			ft = c.substitute(ft, typeSub)
		}
		c.checkPattern(arg, ft)
	}
}

func (c *Checker) checkRecordPattern(p *parser.RecordPattern, subjectType Type) {
	rt, ok := subjectType.(*RecordType)
	if !ok {
		c.errorf(p.PatternPos(), "record pattern requires a Record, got %s", subjectType)
		return
	}
	if p.Name != rt.Name {
		c.errorf(p.PatternPos(), "incompatible record name: expected %s, got %s", rt.Name, p.Name)
	}
	for _, f := range p.Fields {
		fieldType, ok := rt.Fields[f.Name]
		if !ok {
			c.errorf(p.PatternPos(), "record %s does not have field %s", rt.Name, f.Name)
			continue
		}
		if f.Pattern != nil {
			c.checkPattern(f.Pattern, fieldType)
		} else {
			c.scope.Define(f.Name, fieldType, true)
		}
	}
}
