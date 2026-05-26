package checker

import "soyuz/internal/parser"

// pendingClassMethod holds everything needed to check a class method body
// after all top-level function signatures have been registered (Pass 3.7).
type pendingClassMethod struct {
	fd         *parser.FuncDecl
	ct         *ClassType
	selfType   Type
	paramTypes []Type
	retType    Type
	file       string
}

func (c *Checker) checkClassMethodBody(pm pendingClassMethod) {
	if pm.fd.Body == nil {
		return
	}
	parentScope := c.scope
	c.scope = NewScope(parentScope)
	c.scope.Define("self", pm.selfType, true)
	paramIdx := 0
	for _, p := range pm.fd.Params {
		bp, ok := p.Pattern.(*parser.BindingPattern)
		if !ok || bp.Name == "self" {
			continue
		}
		if paramIdx < len(pm.paramTypes) {
			c.scope.Define(bp.Name, pm.paramTypes[paramIdx], true)
		}
		paramIdx++
	}
	oldRet := c.context.returnType
	c.context.returnType = pm.retType
	bodyType := c.checkNode(pm.fd.Body)
	if pm.fd.IsExprBody && !c.isAssignable(pm.retType, bodyType) {
		c.errorf(pm.fd.Body.Pos(), "incompatible return type in method %s: expected %s, got %s", pm.fd.Name, pm.retType, bodyType)
	}
	c.context.returnType = oldRet
	c.scope = parentScope
}

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
	var defaults []parser.Node
	var isOptional []bool
	oldScope := c.scope
	c.scope = funcScope
	for _, p := range first.Params {
		pt := c.resolveTypeExpr(p.Type)
		if pt == Unknown {
			pt = c.inferTypeFromPattern(p.Pattern)
		}
		paramTypes = append(paramTypes, pt)

		optional := false
		if p.Type != nil {
			if _, ok := p.Type.(*parser.OptionalType); ok {
				optional = true
			}
		}
		isOptional = append(isOptional, optional)

		def := p.Default
		if optional && def == nil {
			def = &parser.NoneLiteral{}
		}
		defaults = append(defaults, def)

		if p.Default != nil {
			defaultType := c.checkNode(p.Default)
			if !c.isAssignable(pt, defaultType) {
				c.errorf(p.Default.Pos(), "valor padrão incompatível: esperado %s, encontrado %s", pt, defaultType)
			}
		}
	}

	var retType Type = UnitType
	if first.ReturnType != nil {
		retType = c.resolveTypeExpr(first.ReturnType)
	} else if first.IsExprBody && first.Body != nil {
		// Eager return-type inference for expression-body functions without explicit annotation.
		// We check the body with params in scope so that block-body callers (like main) already
		// see the correct return type when they are checked in Pass 5.
		bodyScope := NewScope(funcScope)
		for i, p := range first.Params {
			if bp, ok := p.Pattern.(*parser.BindingPattern); ok && i < len(paramTypes) {
				bodyScope.Define(bp.Name, paramTypes[i], true)
			}
		}
		c.scope = bodyScope
		retType = c.checkNode(first.Body)
		c.inferredBodies[first] = true
	}
	c.scope = oldScope

	ft := &FuncType{Params: paramTypes, Return: retType, Generics: genericNames, Defaults: defaults, IsOptional: isOptional}
	parentScope.Define(name, ft, true)
	if len(variants) > 0 {
		for _, v := range variants {
			c.registerGlobalSymbol(name, v, v.Pub)
		}
	}

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

	// BUG-01 & BUG-09 & M2: Verificação de exaustividade.
	// Uma função com múltiplas variantes (ou mesmo uma única) deve ter pelo menos uma
	// variante catchall: sem when guard e com todos os padrões sendo binding/wildcard.
	hasCatchall := false
	for _, v := range variants {
		if v.WhenGuard == nil {
			isParamCatchall := true
			for _, p := range v.Params {
				if !isCatchallPattern(p.Pattern) {
					isParamCatchall = false
					break
				}
			}
			if isParamCatchall {
				hasCatchall = true
				break
			}
		}
	}

	if !hasCatchall {
		c.errorf(variants[0].Pos(), "função '%s' requer uma variante catchall sem 'when' e com parâmetros genéricos", name)
	}

	for _, v := range variants {
		if c.nodeFile != nil {
			c.currentFile = c.nodeFile[v]
		}
		c.checkFuncDeclBody(v, ft)
	}
}

func isCatchallPattern(p parser.Pattern) bool {
	switch p.(type) {
	case *parser.BindingPattern, *parser.WildcardPattern:
		return true
	}
	return false
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

	if n.WhenGuard != nil {
		gt := c.checkNode(n.WhenGuard)
		if gt != BoolType {
			c.errorf(n.WhenGuard.Pos(), "o guard 'when' deve ser Bool, encontrado %s", gt)
		}
	}

	oldRet := c.context.returnType
	c.context.returnType = expectedFt.Return
	defer func() { c.context.returnType = oldRet }()

	if c.inferredBodies[n] {
		// Body was already checked during eager inference in registerFuncVariants.
		// Return type is already correct — no need to re-check to avoid duplicate errors.
		return
	}
	bodyType := c.checkNode(n.Body)
	if n.IsExprBody {
		if n.ReturnType == nil {
			// Sem anotação de retorno explícita: inferir do corpo da expressão.
			expectedFt.Return = bodyType
		} else if !c.isAssignable(expectedFt.Return, bodyType) {
			c.errorf(n.Body.Pos(), "incompatible return type: expected %s, got %s", expectedFt.Return, bodyType)
		}
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
		fields[f.Name] = ft
	}

	rt := &RecordType{Name: n.Name, Fields: fields, Generics: genericNames}
	parentScope.Define(n.Name, rt, true)
	c.registerGlobalSymbol(n.Name, n, n.Pub)
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
	c.registerGlobalSymbol(n.Name, n, n.Pub)

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
		// Variant constructors inherit the enum's pub status.
		c.registerGlobalSymbol(v.Name, n, n.Pub)
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
	c.registerGlobalSymbol(n.Name, n, n.Pub)
	return it
}

func (c *Checker) checkClassDecl(n *parser.ClassDecl) Type {
	fields := make(map[string]Type)
	fieldPub := make(map[string]bool)
	fieldInit := make(map[string]parser.Node)
	methods := make(map[string][]*FuncType)
	methodPub := make(map[string]bool)
	var implements []*InterfaceType

	for _, itExpr := range n.Interfaces {
		itType := c.resolveTypeExpr(itExpr)
		if it, ok := itType.(*InterfaceType); ok {
			implements = append(implements, it)
		} else {
			c.errorf(itExpr.TypePos(), "%s is not an interface", itType)
		}
	}

	ct := &ClassType{
		Name:       n.Name,
		Fields:     fields,
		FieldPub:   fieldPub,
		FieldInit:  fieldInit,
		Methods:    methods,
		MethodPub:  methodPub,
		Implements: implements,
	}
	c.scope.Define(n.Name, ct, true)
	c.registerGlobalSymbol(n.Name, n, n.Pub)

	// Field pass: resolve declared field types and collect defaults/visibility.
	for _, member := range n.Body {
		v, ok := member.(*parser.VarDecl)
		if !ok {
			continue
		}
		var ft Type
		if v.Type != nil {
			ft = c.resolveTypeExpr(v.Type)
		} else {
			ft = Unknown
		}

		fields[v.Name] = ft
		fieldPub[v.Name] = v.Pub
		if v.Init != nil {
			fieldInit[v.Name] = v.Init
			defaultType := c.checkNode(v.Init)
			if !c.isAssignable(ft, defaultType) {
				c.errorf(v.Init.Pos(), "valor padrão incompatível para campo %s: esperado %s, encontrado %s", v.Name, ft, defaultType)
			}
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
		nonSelfIdx := 0
		for _, p := range fd.Params {
			if bp, ok2 := p.Pattern.(*parser.BindingPattern); ok2 && bp.Name == "self" {
				continue
			}
			paramTypes = append(paramTypes, c.resolveTypeExpr(p.Type))
			nonSelfIdx++
		}
		var ret Type = UnitType
		if fd.ReturnType != nil {
			ret = c.resolveTypeExpr(fd.ReturnType)
		}
		ft := &FuncType{Params: paramTypes, Return: ret}
		methods[fd.Name] = append(methods[fd.Name], ft)
		methodPub[fd.Name] = fd.Pub
		c.nodeTypes[fd] = ft

		// Defer method body checking to Pass 3.7 so that top-level functions
		// (registered in Pass 3) are already in scope when method bodies run.
		var selfType Type = ct
		if ct.Name == "StringExtensions" {
			selfType = StringType
		}
		c.pendingClassMethods = append(c.pendingClassMethods, pendingClassMethod{
			fd:         fd,
			ct:         ct,
			selfType:   selfType,
			paramTypes: paramTypes,
			retType:    ret,
			file:       c.currentFile,
		})
	}

	// Interface implementation check.
	for _, it := range implements {
		for name, expectedFt := range it.Methods {
			variants, ok := methods[name]
			if !ok || len(variants) == 0 {
				c.errorf(n.Pos(), "class %s does not implement method %s required by interface %s", n.Name, name, it.Name)
				continue
			}
			found := false
			for _, actualFt := range variants {
				if c.isCompatibleFunc(expectedFt, actualFt) {
					found = true
					break
				}
			}
			if !found {
				c.errorf(n.Pos(), "method signature %s in %s incompatible with interface %s", name, n.Name, it.Name)
			}
		}
	}

	return ct
}

func (c *Checker) checkVarDecl(decl *parser.VarDecl) Type {
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
	if c.inTopLevel {
		c.registerGlobalSymbol(decl.Name, decl, decl.Pub)
	}
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

// registerModuleNamespace cria um ClassType sintético no scope com o nome do módulo
// (ex: "mock") contendo todas as declarações pub do arquivo do módulo.
// Isso permite acesso qualificado: mock.assert_true(...).
func (c *Checker) registerModuleNamespace(prog *parser.Program, imp *parser.ImportDecl) {
	if imp.Path == "" || len(imp.ResolvedFiles) == 0 || c.nodeFile == nil {
		return
	}
	modName := imp.Namespace

	resolvedSet := make(map[string]bool, len(imp.ResolvedFiles))
	for _, f := range imp.ResolvedFiles {
		resolvedSet[f] = true
	}

	ns := &ClassType{
		Name:      modName,
		Methods:   make(map[string][]*FuncType),
		MethodPub: make(map[string]bool),
		Fields:    make(map[string]Type),
		FieldPub:  make(map[string]bool),
		FieldInit: make(map[string]parser.Node),
	}

	for _, node := range prog.Body {
		// Só considera nós originários dos arquivos do módulo.
		if !resolvedSet[c.nodeFile[node]] {
			continue
		}
		var name string
		var pub bool
		switch n := node.(type) {
		case *parser.FuncDecl:
			name, pub = n.Name, n.Pub
		case *parser.ExternDecl:
			name, pub = n.Name, n.Pub
		}
		if name == "" || !pub {
			continue
		}
		sym, ok := c.scope.Resolve(name)
		if !ok {
			continue
		}
		ft, ok := sym.Type.(*FuncType)
		if !ok {
			continue
		}
		ns.Methods[name] = []*FuncType{ft}
		ns.MethodPub[name] = true
	}

	if len(ns.Methods) > 0 {
		c.scope.Define(modName, ns, true)
	}
}

func (c *Checker) checkExternDecl(n *parser.ExternDecl) Type {
	var paramTypes []Type
	for _, p := range n.Params {
		pt := c.resolveTypeExpr(p.Type)
		paramTypes = append(paramTypes, pt)
	}
	var retType Type = UnitType
	if n.ReturnType != nil {
		retType = c.resolveTypeExpr(n.ReturnType)
	}
	ft := &FuncType{Params: paramTypes, Return: retType}
	c.scope.Define(n.Name, ft, true)
	if c.inTopLevel || c.nodeFile != nil {
		c.registerGlobalSymbol(n.Name, n, n.Pub)
	}
	return ft
}

type pendingExtendMethod struct {
	fd         *parser.FuncDecl
	typeName   string
	selfType   Type
	paramTypes []Type
	retType    Type
	file       string
}

func (c *Checker) resolveExtendTarget(typeName string) (Type, bool) {
	if st, ok := c.extendSelfTypes[typeName]; ok {
		return st, true
	}
	if sym, ok := c.scope.Resolve(typeName); ok {
		return sym.Type, true
	}
	return nil, false
}

func (c *Checker) checkExtendDecl(n *parser.ExtendDecl) Type {
	selfType, ok := c.resolveExtendTarget(n.TypeName)
	if !ok {
		c.errorf(n.Pos(), "extend: tipo '%s' não encontrado", n.TypeName)
		return Unknown
	}
	if c.typeExtensions[n.TypeName] == nil {
		c.typeExtensions[n.TypeName] = make(map[string][]*FuncType)
	}
	for _, fd := range n.Methods {
		var paramTypes []Type
		nonSelfIdx := 0
		for _, p := range fd.Params {
			if bp, ok := p.Pattern.(*parser.BindingPattern); ok && bp.Name == "self" {
				continue
			}
			var pt Type
			if p.Type != nil {
				pt = c.resolveTypeExpr(p.Type)
			} else {
				pt = Unknown
			}
			paramTypes = append(paramTypes, pt)
			nonSelfIdx++
		}
		var ret Type = UnitType
		if fd.ReturnType != nil {
			ret = c.resolveTypeExpr(fd.ReturnType)
		}
		ft := &FuncType{Params: paramTypes, Return: ret}
		c.typeExtensions[n.TypeName][fd.Name] = append(c.typeExtensions[n.TypeName][fd.Name], ft)
		c.nodeTypes[fd] = ft
		c.pendingExtendMethods = append(c.pendingExtendMethods, pendingExtendMethod{
			fd:         fd,
			typeName:   n.TypeName,
			selfType:   selfType,
			paramTypes: paramTypes,
			retType:    ret,
			file:       c.currentFile,
		})
	}
	return UnitType
}

func (c *Checker) checkExtendMethodBody(pm pendingExtendMethod) {
	if pm.fd.Body == nil {
		return
	}
	parentScope := c.scope
	c.scope = NewScope(parentScope)
	c.scope.Define("self", pm.selfType, true)
	paramIdx := 0
	for _, p := range pm.fd.Params {
		bp, ok := p.Pattern.(*parser.BindingPattern)
		if !ok || bp.Name == "self" {
			continue
		}
		if paramIdx < len(pm.paramTypes) {
			c.scope.Define(bp.Name, pm.paramTypes[paramIdx], true)
			paramIdx++
		}
	}
	prev := c.context.returnType
	c.context.returnType = pm.retType
	c.checkNode(pm.fd.Body)
	c.context.returnType = prev
	c.scope = parentScope
}
