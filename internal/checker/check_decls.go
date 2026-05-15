package checker

import "soyuz/internal/parser"

func (c *Checker) registerFuncVariants(name string, variants []*parser.FuncDecl) {
	if len(variants) == 0 {
		return
	}
	first := variants[0]

	parentScope := c.scope
	funcScope := NewScope(parentScope)

	var genericNames []string
	for _, g := range first.Generics {
		genericNames = append(genericNames, g.Name)
		funcScope.Define(g.Name, &TypeParameter{Name: g.Name}, true)
	}

	var paramTypes []Type
	oldScope := c.scope
	c.scope = funcScope
	for _, p := range first.Params {
		pt := c.resolveTypeExpr(p.Type)
		if pt == Unknown {
			pt = c.inferTypeFromPattern(p.Pattern)
		}
		paramTypes = append(paramTypes, pt)
	}

	var retType Type = UnitType
	if first.ReturnType != nil {
		retType = c.resolveTypeExpr(first.ReturnType)
	}
	c.scope = oldScope

	ft := &FuncType{Params: paramTypes, Return: retType, Generics: genericNames}
	parentScope.Define(name, ft, true)

	for _, v := range variants {
		c.nodeTypes[v] = ft
	}
}

func (c *Checker) inferTypeFromPattern(pat parser.Pattern) Type {
	switch p := pat.(type) {
	case *parser.LiteralPattern:
		return c.checkNode(p.Value)
	case *parser.ConstructorPattern:
		if p.Name == "Some" || p.Name == "None" {
			base := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
			return &SpecializedType{Base: base, Params: []Type{Unknown}}
		}
		if p.Name == "Ok" || p.Name == "Err" {
			base := c.resolveTypeExpr(&parser.NamedType{Name: "Result"})
			return &SpecializedType{Base: base, Params: []Type{Unknown}}
		}
		if sym, ok := c.scope.Resolve(p.Name); ok {
			if ft, ok := sym.Type.(*FuncType); ok {
				return ft.Return
			}
			return sym.Type
		}
	case *parser.RecordPattern:
		if sym, ok := c.scope.Resolve(p.Name); ok {
			return sym.Type
		}
	}
	return Unknown
}

func (c *Checker) checkFuncVariantsBody(name string, variants []*parser.FuncDecl) {
	sym, ok := c.scope.Resolve(name)
	if !ok {
		return
	}
	ft, ok := sym.Type.(*FuncType)
	if !ok {
		return
	}
	for _, v := range variants {
		c.checkFuncDeclBody(v, ft)
	}
}

func (c *Checker) checkFuncDeclBody(n *parser.FuncDecl, expectedFt *FuncType) {
	parentScope := c.scope
	c.scope = NewScope(parentScope)
	defer func() { c.scope = parentScope }()

	for _, gName := range expectedFt.Generics {
		c.scope.Define(gName, &TypeParameter{Name: gName}, true)
	}

	for i, p := range n.Params {
		if i >= len(expectedFt.Params) {
			break
		}
		c.checkPattern(p.Pattern, expectedFt.Params[i])
	}

	oldRet := currentContext.returnType
	currentContext.returnType = expectedFt.Return
	defer func() { currentContext.returnType = oldRet }()

	bodyType := c.checkNode(n.Body)
	if n.IsExprBody && !c.isAssignable(expectedFt.Return, bodyType) {
		c.errorf(n.Body.Pos(), "incompatible return type: expected %s, got %s", expectedFt.Return, bodyType)
	}
}

func (c *Checker) checkFuncDecl(n *parser.FuncDecl) Type {
	c.registerFuncVariants(n.Name, []*parser.FuncDecl{n})
	sym, _ := c.scope.Resolve(n.Name)
	ft := sym.Type.(*FuncType)
	c.checkFuncDeclBody(n, ft)
	return ft
}

func (c *Checker) checkRecordDecl(n *parser.RecordDecl) Type {
	parentScope := c.scope
	c.scope = NewScope(parentScope)
	defer func() { c.scope = parentScope }()

	var genericNames []string
	for _, g := range n.Generics {
		genericNames = append(genericNames, g.Name)
		c.scope.Define(g.Name, &TypeParameter{Name: g.Name}, true)
	}

	fields := make(map[string]Type)
	for _, f := range n.Fields {
		ft := c.resolveTypeExpr(f.Type)
		if f.Weak {
			if !c.isHeapType(ft) {
				c.errorf(f.Pos, "weak só pode ser usado em tipos heap (records ou classes), encontrado %s", ft)
			}
		}
		fields[f.Name] = ft
	}

	rt := &RecordType{Name: n.Name, Fields: fields, Generics: genericNames}
	parentScope.Define(n.Name, rt, true)
	return rt
}

func (c *Checker) checkEnumDecl(n *parser.EnumDecl) Type {
	parentScope := c.scope
	c.scope = NewScope(parentScope)
	defer func() { c.scope = parentScope }()

	var genericNames []string
	for _, g := range n.Generics {
		genericNames = append(genericNames, g.Name)
		c.scope.Define(g.Name, &TypeParameter{Name: g.Name}, true)
	}

	variants := make(map[string][]Type)
	et := &EnumType{Name: n.Name, Variants: variants, Generics: genericNames}
	parentScope.Define(n.Name, et, true)

	// For generic enums, constructor return type is SpecializedType{et, [T1, T2, ...]}
	// so that instantiateFunc can substitute type params in the return type.
	var constructorReturn Type = et
	if len(genericNames) > 0 {
		tparams := make([]Type, len(genericNames))
		for i, name := range genericNames {
			tparams[i] = &TypeParameter{Name: name}
		}
		constructorReturn = &SpecializedType{Base: et, Params: tparams}
	}

	for _, v := range n.Variants {
		var fieldTypes []Type
		for _, f := range v.Fields {
			fieldTypes = append(fieldTypes, c.resolveTypeExpr(f.Type))
		}
		variants[v.Name] = fieldTypes
		vt := &FuncType{Params: fieldTypes, Return: constructorReturn, Generics: genericNames}
		parentScope.Define(v.Name, vt, true)
	}
	return et
}

func (c *Checker) checkInterfaceDecl(n *parser.InterfaceDecl) Type {
	methods := make(map[string]*FuncType)
	for _, m := range n.Methods {
		var params []Type
		for _, p := range m.Params {
			// Skip the implicit self parameter.
			if bp, ok := p.Pattern.(*parser.BindingPattern); ok && bp.Name == "self" {
				continue
			}
			params = append(params, c.resolveTypeExpr(p.Type))
		}
		var ret Type = UnitType
		if m.ReturnType != nil {
			ret = c.resolveTypeExpr(m.ReturnType)
		}
		methods[m.Name] = &FuncType{Params: params, Return: ret}
	}
	it := &InterfaceType{Name: n.Name, Methods: methods}
	c.scope.Define(n.Name, it, true)
	return it
}

func (c *Checker) checkClassDecl(n *parser.ClassDecl) Type {
	fields := make(map[string]Type)
	methods := make(map[string]*FuncType)
	var implements []*InterfaceType

	for _, itExpr := range n.Interfaces {
		itType := c.resolveTypeExpr(itExpr)
		if it, ok := itType.(*InterfaceType); ok {
			implements = append(implements, it)
		} else {
			c.errorf(itExpr.TypePos(), "%s is not an interface", itType)
		}
	}

	ct := &ClassType{Name: n.Name, Fields: fields, Methods: methods, Implements: implements}
	c.scope.Define(n.Name, ct, true)

	// Field pass: resolve declared field types.
	for _, member := range n.Body {
		if v, ok := member.(*parser.VarDecl); ok {
			var ft Type
			if v.Type != nil {
				ft = c.resolveTypeExpr(v.Type)
			} else {
				ft = Unknown
			}

			if v.Weak {
				if v.Kind != parser.KindVar {
					c.errorf(v.Pos(), "weak só pode ser usado com var")
				}
				if !c.isHeapType(ft) {
					c.errorf(v.Pos(), "weak só pode ser usado em tipos heap (records ou classes), encontrado %s", ft)
				}
			}
			fields[v.Name] = ft
		}
	}

	// Method pass: build FuncType for each method, check body with self in scope.
	prevClass := c.currentClass
	c.currentClass = ct
	defer func() { c.currentClass = prevClass }()

	for _, member := range n.Body {
		fd, ok := member.(*parser.FuncDecl)
		if !ok {
			continue
		}
		// Build method FuncType, skipping the implicit self parameter.
		var paramTypes []Type
		for _, p := range fd.Params {
			if bp, ok2 := p.Pattern.(*parser.BindingPattern); ok2 && bp.Name == "self" {
				continue
			}
			paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
		}
		var ret Type = UnitType
		if fd.ReturnType != nil {
			ret = c.resolveTypeExpr(fd.ReturnType)
		}
		ft := &FuncType{Params: paramTypes, Return: ret}
		methods[fd.Name] = ft
		c.nodeTypes[fd] = ft

		// Check method body with self and params in scope.
		if fd.Body != nil {
			parentScope := c.scope
			c.scope = NewScope(parentScope)
			c.scope.Define("self", ct, true)
			for i, p := range fd.Params {
				if bp, ok2 := p.Pattern.(*parser.BindingPattern); ok2 {
					if bp.Name == "self" {
						continue
					}
					if i < len(paramTypes) {
						c.scope.Define(bp.Name, paramTypes[i], true)
					}
				}
			}
			oldRet := currentContext.returnType
			currentContext.returnType = ret
			bodyType := c.checkNode(fd.Body)
			if fd.IsExprBody && !c.isAssignable(ret, bodyType) {
				c.errorf(fd.Body.Pos(), "incompatible return type in method %s: expected %s, got %s", fd.Name, ret, bodyType)
			}
			currentContext.returnType = oldRet
			c.scope = parentScope
		}
	}

	for _, it := range implements {
		for name, expectedFt := range it.Methods {
			actualFt, ok := methods[name]
			if !ok {
				c.errorf(n.Pos(), "class %s does not implement method %s required by interface %s", n.Name, name, it.Name)
				continue
			}
			if !c.isCompatibleFunc(expectedFt, actualFt) {
				c.errorf(n.Pos(), "method signature %s in %s incompatible with interface %s", name, n.Name, it.Name)
			}
		}
	}

	return ct
}

func (c *Checker) checkVarDecl(decl *parser.VarDecl) Type {
	if decl.Weak {
		c.errorf(decl.Pos(), "weak não é suportado em variáveis locais, apenas em campos de records ou classes")
	}

	var initType Type = Unknown
	if decl.Init != nil {
		initType = c.checkNode(decl.Init)
	}
	if decl.Type != nil {
		expected := c.resolveTypeExpr(decl.Type)
		if initType != Unknown && !c.isAssignable(expected, initType) {
			c.errorf(decl.Init.Pos(), "incompatible type: expected %s, got %s", expected, initType)
		}
		initType = expected
	}
	if decl.Pattern != nil {
		c.checkPattern(decl.Pattern, initType)
		return initType
	}
	c.scope.Define(decl.Name, initType, decl.Kind != parser.KindVar)
	return initType
}

func getKindName(t Type) string {
	switch t.(type) {
	case *RecordType:
		return "record"
	case *ClassType:
		return "class"
	default:
		return "type"
	}
}
