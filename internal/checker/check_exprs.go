package checker

import (
	"fmt"
	"strconv"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func (c *Checker) checkTupleExpr(n *parser.TupleExpr) Type {
	if len(n.Elements) == 0 {
		return UnitType
	}
	elems := make([]Type, len(n.Elements))
	for i, e := range n.Elements {
		elems[i] = c.checkNode(e)
	}
	return &TupleType{Elements: elems}
}

func (c *Checker) checkListExpr(n *parser.ListExpr) Type {
	var elemType Type = Unknown
	if len(n.Elements) == 0 && c.context.expectedType != nil {
		if st, ok := c.context.expectedType.(*SpecializedType); ok {
			if ct, ok := st.Base.(*ClassType); ok && ct.Name == "List" && len(st.Params) > 0 {
				elemType = st.Params[0]
			}
		}
	}
	for _, e := range n.Elements {
		t := c.checkNode(e)
		if elemType == Unknown {
			elemType = t
		} else if !c.isAssignable(elemType, t) {
			if c.isAssignable(t, elemType) {
				elemType = t
			} else {
				c.errorf(e.Pos(), "incompatible element type in list: expected %s, got %s", elemType, t)
			}
		}
	}
	base := c.resolveTypeExpr(&parser.NamedType{Name: "List"})
	return &SpecializedType{Base: base, Params: []Type{elemType}}
}

func (c *Checker) checkMapExpr(n *parser.MapExpr) Type {
	var keyType Type = Unknown
	var valType Type = Unknown
	for _, entry := range n.Entries {
		kt := c.checkNode(entry.Key)
		vt := c.checkNode(entry.Value)

		if keyType == Unknown {
			keyType = kt
		} else if !c.isAssignable(keyType, kt) {
			c.errorf(entry.Key.Pos(), "incompatible key type in map: expected %s, got %s", keyType, kt)
		}

		if valType == Unknown {
			valType = vt
		} else if !c.isAssignable(valType, vt) {
			c.errorf(entry.Value.Pos(), "incompatible value type in map: expected %s, got %s", valType, vt)
		}
	}
	base := c.resolveTypeExpr(&parser.NamedType{Name: "Map"})
	return &SpecializedType{Base: base, Params: []Type{keyType, valType}}
}

func (c *Checker) checkTaskExpr(n *parser.TaskExpr) Type {
	innerType := c.checkNode(n.Inner)
	base := c.resolveTypeExpr(&parser.NamedType{Name: "Task"})
	return &SpecializedType{Base: base, Params: []Type{innerType}}
}

// checkSelectExpr type-checks a select { ... } expression.
//
// Recv arms accept either:
//   - ch.recv() / ch.tryRecv() on Channel[T]  → binding gets T
//   - t.await() on Task[T]                    → binding gets T (synthesized bridge channel in codegen)
// Default arm: no channel needed.
func (c *Checker) checkSelectExpr(n *parser.SelectExpr) Type {
	var resultType Type = UnitType
	first := true

	for i, arm := range n.Arms {
		parentScope := c.scope
		c.scope = NewScope(parentScope)

		if arm.IsDefault {
			bodyType := c.checkNode(arm.Body)
			if first {
				resultType = bodyType
				first = false
			} else if !c.isAssignable(resultType, bodyType) && !c.isAssignable(bodyType, resultType) {
				c.errorf(arm.Pos, "select arm %d: tipo do corpo incompatível com arms anteriores (esperado %s, obtido %s)", i, resultType, bodyType)
			}
		} else {
			if arm.Chan == nil {
				c.errorf(arm.Pos, "select arm %d: expressão de canal ausente", i)
				c.scope = parentScope
				continue
			}
			// Check if this is a task await arm: t.await()
			var innerType Type = Unknown
			if isTaskAwaitCall(arm.Chan) {
				// t.await() arm — unwrap Task[T] → T
				if call, ok := arm.Chan.(*parser.CallExpr); ok {
					if me, ok2 := call.Callee.(*parser.MemberExpr); ok2 {
						taskType := c.checkNode(me.Object)
						if st, ok3 := taskType.(*SpecializedType); ok3 && len(st.Params) > 0 {
							innerType = st.Params[0]
						}
					}
				}
			} else {
				// ch.recv() / ch.tryRecv() arm
				chanCallType := c.checkNode(arm.Chan)
				if st, ok := chanCallType.(*SpecializedType); ok {
					if et, ok2 := st.Base.(*EnumType); ok2 && et.Name == "Option" && len(st.Params) > 0 {
						innerType = st.Params[0]
					}
				}
			}
			if arm.Binding != "" {
				c.scope.Define(arm.Binding, innerType, true)
			}
			bodyType := c.checkNode(arm.Body)
			if first {
				resultType = bodyType
				first = false
			} else if !c.isAssignable(resultType, bodyType) && !c.isAssignable(bodyType, resultType) {
				c.errorf(arm.Pos, "select arm %d: tipo do corpo incompatível com arms anteriores (esperado %s, obtido %s)", i, resultType, bodyType)
			}
		}

		c.scope = parentScope
	}

	return resultType
}

// isTaskAwaitCall reports whether node is a t.await() call expression.
func isTaskAwaitCall(node parser.Node) bool {
	call, ok := node.(*parser.CallExpr)
	if !ok {
		return false
	}
	me, ok := call.Callee.(*parser.MemberExpr)
	if !ok {
		return false
	}
	return me.Property == "await"
}

func (c *Checker) checkMemberExpr(n *parser.MemberExpr) Type {
	if id, ok := n.Object.(*parser.Identifier); ok {
		if sym, exists := c.scope.Resolve(id.Name); exists {
			// Only treat as a namespace if the identifier IS the class name itself
			// (e.g. Task.all, TaskHandle.current). Local variables/parameters that
			// happen to have a no-field ClassType (e.g. `handle: TaskHandle`) must
			// not be flagged as namespace references.
			if ct, ok := sym.Type.(*ClassType); ok && id.Name == ct.Name && len(ct.Fields) == 0 && len(ct.Methods) > 0 {
				c.checkModuleNamespaceAccess(id.Name, n.Pos())
			}
		}
	}
	objType := c.checkNode(n.Object)
	return c.resolveMemberType(objType, n.Property, n.Pos())
}

func (c *Checker) resolveMemberType(objType Type, property string, pos lexer.Position) Type {
	switch t := objType.(type) {
	case *EnumType:
		// Enum.Variant — look up directly in the enum's own variants to avoid scope collisions
		// when two enums share the same variant name (e.g. two enums with Ok/Err).
		if fieldTypes, ok := t.Variants[property]; ok {
			var retType Type = t
			if len(t.Generics) > 0 {
				tparams := make([]Type, len(t.Generics))
				for i, name := range t.Generics {
					tparams[i] = &TypeParameter{Name: name}
				}
				retType = &SpecializedType{Base: t, Params: tparams}
			}
			if len(fieldTypes) == 0 {
				return retType
			}
			return &FuncType{Params: fieldTypes, Return: retType, Generics: t.Generics}
		}
		c.errorf(pos, "enum %s não tem variante %s", t.Name, property)
		return Unknown
	case *SpecializedType:
		if ct, ok := t.Base.(*ClassType); ok {
			var sub map[string]Type
			if len(ct.Generics) > 0 && len(t.Params) > 0 {
				sub = make(map[string]Type)
				for i, gname := range ct.Generics {
					if i < len(t.Params) {
						sub[gname] = t.Params[i]
					}
				}
			}
			if variants, ok2 := ct.Methods[property]; ok2 && len(variants) > 0 {
				if pub, has := ct.MethodPub[property]; has && !pub && !c.canAccessClassMember(ct) {
					c.errorf(pos, "método '%s' de '%s' é privado", property, ct.Name)
				}
				ft := variants[0]
				if sub != nil {
					newParams := make([]Type, len(ft.Params))
					for i, p := range ft.Params {
						newParams[i] = c.substitute(p, sub)
					}
					return &FuncType{Params: newParams, Return: c.substitute(ft.Return, sub)}
				}
				return ft
			}
			if methods, ok := c.typeExtensions[ct.Name]; ok {
				if variants, ok2 := methods[property]; ok2 && len(variants) > 0 {
					ft := variants[0]
					if sub != nil {
						newParams := make([]Type, len(ft.Params))
						for i, p := range ft.Params {
							newParams[i] = c.substitute(p, sub)
						}
						return &FuncType{Params: newParams, Return: c.substitute(ft.Return, sub)}
					}
					return ft
				}
			}
			// Fields with type-parameter substitution (e.g. MutexGuard[T].value → T)
			if ft, ok2 := ct.Fields[property]; ok2 {
				if pub, has := ct.FieldPub[property]; has && !pub && !c.canAccessClassMember(ct) {
					c.errorf(pos, "campo '%s' de '%s' é privado", property, ct.Name)
				}
				if sub != nil {
					return c.substitute(ft, sub)
				}
				return ft
			}
		}
		return Unknown
	case *ClassType:
		if variants, ok := t.Methods[property]; ok && len(variants) > 0 {
			if pub, has := t.MethodPub[property]; has && !pub && !c.canAccessClassMember(t) {
				c.errorf(pos, "método '%s' de '%s' é privado", property, t.Name)
			}
			return variants[0]
		}
		if methods, ok := c.typeExtensions[t.Name]; ok {
			if variants, ok2 := methods[property]; ok2 && len(variants) > 0 {
				return variants[0]
			}
		}
		if ft, ok := t.Fields[property]; ok {
			if pub, has := t.FieldPub[property]; has && !pub && !c.canAccessClassMember(t) {
				c.errorf(pos, "campo '%s' de '%s' é privado", property, t.Name)
			}
			return ft
		}
		c.errorf(pos, "class %s has no member %s", t.Name, property)
		return Unknown
	case *InterfaceType:
		if ft, ok := t.Methods[property]; ok {
			return ft
		}
		c.errorf(pos, "interface %s has no method %s", t.Name, property)
		return Unknown
	case *RecordType:
		if ft, ok := t.Fields[property]; ok {
			return ft
		}
		c.errorf(pos, "record %s has no field %s", t.Name, property)
		return Unknown
	case *TupleType:
		idx, err := strconv.Atoi(property)
		if err != nil {
			c.errorf(pos, "invalid tuple index: %s", property)
			return Unknown
		}
		if idx < 0 || idx >= len(t.Elements) {
			c.errorf(pos, "tuple index out of bounds: %d (arity %d)", idx, len(t.Elements))
			return Unknown
		}
		return t.Elements[idx]
	case *BasicType:
		if methods, ok := c.typeExtensions[t.Name]; ok {
			if variants, ok2 := methods[property]; ok2 && len(variants) > 0 {
				return variants[0]
			}
		}
		if t.Name == "String" {
			if sym, ok := c.scope.Resolve("StringExtensions"); ok {
				if ct, ok2 := sym.Type.(*ClassType); ok2 {
					if variants, ok3 := ct.Methods[property]; ok3 && len(variants) > 0 {
						return variants[0]
					}
				}
			}
		}
		c.errorf(pos, "%s não tem método '%s'", t.Name, property)
		return Unknown
	}
	// Unknown object type — allow, return Unknown
	return Unknown
}

func (c *Checker) wrapOptionType(inner Type) Type {
	base := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
	return &SpecializedType{Base: base, Params: []Type{inner}}
}

// checkSafeNavExpr: `obj?.field` — obj must be Option[T], result is Option[FieldType].
func (c *Checker) checkSafeNavExpr(n *parser.SafeNavExpr) Type {
	objType := c.checkNode(n.Object)
	innerType, kind := c.unwrapResultOption(objType)
	if kind != "Option" {
		c.errorf(n.Pos(), "safe navigation (?.) requer Option[T], obtido %s", objType)
		return Unknown
	}
	memberType := c.resolveMemberType(innerType, n.Property, n.Pos())
	if memberType == Unknown {
		return Unknown
	}
	return c.wrapOptionType(memberType)
}

func (c *Checker) checkSelfExpr(n *parser.SelfExpr) Type {
	if sym, ok := c.scope.Resolve("self"); ok {
		return sym.Type
	}
	if c.currentExtend != "" {
		if st, ok := c.extendSelfTypes[c.currentExtend]; ok {
			return st
		}
	}
	if c.currentClass != nil {
		if c.currentClass.Name == "StringExtensions" {
			return StringType
		}
		return c.currentClass
	}
	c.errorf(n.Pos(), "self usado fora de um método de classe")
	return Unknown
}

func (c *Checker) checkArrowFunc(n *parser.ArrowFunc) Type {
	parentScope := c.scope
	c.scope = NewScope(parentScope)
	defer func() { c.scope = parentScope }()

	// M4a: use injected hints when a param has no type annotation
	hints := c.arrowFuncHints[n]

	paramNames := map[string]bool{}
	var paramTypes []Type
	for i, p := range n.Params {
		var pt Type
		if p.Type != nil {
			pt = c.resolveTypeExpr(p.Type)
		} else if i < len(hints) {
			pt = hints[i]
		} else {
			pt = Unknown
		}
		paramTypes = append(paramTypes, pt)
		c.checkPattern(p.Pattern, pt)
		if bp, ok := p.Pattern.(*parser.BindingPattern); ok {
			paramNames[bp.Name] = true
		}
	}

	var expectedRet Type
	if n.ReturnType != nil {
		expectedRet = c.resolveTypeExpr(n.ReturnType)
	}

	oldRet := c.context.returnType
	c.context.returnType = expectedRet
	defer func() { c.context.returnType = oldRet }()

	bodyType := c.checkNode(n.Body)
	if expectedRet == nil {
		expectedRet = bodyType
	} else if !c.isAssignable(expectedRet, bodyType) {
		c.errorf(n.Body.Pos(), "incompatible return type in lambda: expected %s, got %s", expectedRet, bodyType)
	}

	// Detect variables captured from the enclosing scope.
	c.captures[n] = freeIdentifiers(n.Body, paramNames, parentScope)

	return &FuncType{Params: paramTypes, Return: expectedRet}
}

// freeIdentifiers walks an AST node and returns the names of variables that
// are resolved in parentScope but are not in the arrow function's own param set.
// Global functions (FuncType symbols) are excluded since they are called directly.
func freeIdentifiers(node parser.Node, paramNames map[string]bool, parentScope *Scope) []string {
	seen := map[string]bool{}
	var result []string
	var walk func(parser.Node)
	walk = func(n parser.Node) {
		if n == nil {
			return
		}
		switch v := n.(type) {
		case *parser.Identifier:
			if paramNames[v.Name] || seen[v.Name] {
				return
			}
			if sym, ok := parentScope.Resolve(v.Name); ok {
				// Encontra em qual escopo o símbolo está definido.
				s := parentScope
				isGlobal := false
				for s != nil {
					if _, ok := s.Symbols[v.Name]; ok {
						if s.Parent == nil {
							isGlobal = true
						}
						break
					}
					s = s.Parent
				}

				// Variáveis locais (mesmo que sejam funções/lambdas) devem ser capturadas.
				// Apenas funções globais (topo do módulo) podem ser chamadas diretamente por nome no codegen.
				// O uso de sym garante que o símbolo existe e tem um tipo.
				if !isGlobal {
					_ = sym // Garante uso da variável
					seen[v.Name] = true
					result = append(result, v.Name)
				}
			}
		case *parser.BinaryExpr:
			walk(v.Left)
			walk(v.Right)
		case *parser.UnaryExpr:
			walk(v.Operand)
		case *parser.CallExpr:
			walk(v.Callee)
			for _, a := range v.Args {
				walk(a)
			}
		case *parser.IfStmt:
			walk(v.Condition)
			walk(v.Consequent)
			if v.Alternate != nil {
				walk(v.Alternate)
			}
		case *parser.BlockStmt:
			for _, s := range v.Statements {
				walk(s)
			}
		case *parser.ReturnStmt:
			if v.Value != nil {
				walk(v.Value)
			}
		case *parser.VarDecl:
			if v.Init != nil {
				walk(v.Init)
			}
		case *parser.AssignExpr:
			walk(v.Left)
			walk(v.Right)
		case *parser.MemberExpr:
			walk(v.Object)
		case *parser.MatchExpr:
			walk(v.Subject)
			for _, arm := range v.Arms {
				if arm.Guard != nil {
					walk(arm.Guard)
				}
				walk(arm.Body)
			}
		case *parser.PipeExpr:
			walk(v.Left)
			walk(v.Right)
		case *parser.PipeQuestExpr:
			walk(v.Left)
			walk(v.Right)
		case *parser.ExprStmt:
			walk(v.Expr)
		case *parser.RecordLiteral:
			for _, f := range v.Fields {
				walk(f.Value)
			}
		case *parser.InterpolatedString:
			for _, p := range v.Parts {
				walk(p)
			}
		case *parser.TupleExpr:
			for _, e := range v.Elements {
				walk(e)
			}
		case *parser.ArrowFunc:
			// Nested arrow func — skip; its own capture analysis handles the transitive case.
		case *parser.SelfExpr:
			if _, ok := parentScope.Resolve("self"); ok {
				if !seen["self"] {
					seen["self"] = true
					result = append(result, "self")
				}
			}
		}
	}
	walk(node)
	return result
}

func (c *Checker) checkPipeExpr(n *parser.PipeExpr) Type {
	var call *parser.CallExpr
	if rc, ok := n.Right.(*parser.CallExpr); ok {
		newArgs := append([]parser.Node{n.Left}, rc.Args...)
		call = &parser.CallExpr{Callee: rc.Callee, Args: newArgs}
	} else {
		call = &parser.CallExpr{Callee: n.Right, Args: []parser.Node{n.Left}}
	}

	t := c.checkCallExpr(call)
	if ft, ok := c.specializations[call]; ok {
		c.specializations[n] = ft
	}
	return t
}

// unwrapResultOption returns the inner payload type and enum name ("Result" or "Option").
func (c *Checker) unwrapResultOption(t Type) (Type, string) {
	if st, ok := t.(*SpecializedType); ok {
		if et, ok := st.Base.(*EnumType); ok && len(st.Params) > 0 {
			if et.Name == "Result" || et.Name == "Option" {
				return st.Params[0], et.Name
			}
		}
	}
	return Unknown, ""
}

// asResultType normalizes a return type to Result[T] for |?> chaining.
func (c *Checker) asResultType(t Type) Type {
	if inner, kind := c.unwrapResultOption(t); kind != "" {
		if kind == "Result" {
			return t
		}
		if eb := c.resultEnum(); eb != nil {
			return &SpecializedType{Base: eb, Params: []Type{inner}}
		}
	}
	if eb := c.resultEnum(); eb != nil {
		return &SpecializedType{Base: eb, Params: []Type{t}}
	}
	return Unknown
}

func (c *Checker) resultEnum() *EnumType {
	sym, ok := c.scope.Resolve("Result")
	if !ok {
		return nil
	}
	et, ok := sym.Type.(*EnumType)
	if !ok {
		return nil
	}
	return et
}

func (c *Checker) checkPipeQuestExpr(n *parser.PipeQuestExpr) Type {
	leftType := c.checkNode(n.Left)
	innerType, kind := c.unwrapResultOption(leftType)
	if kind == "" {
		c.errorf(n.Left.Pos(), "|?> requer Result ou Option à esquerda, obtido %s", leftType)
		return Unknown
	}

	var call *parser.CallExpr
	if rc, ok := n.Right.(*parser.CallExpr); ok {
		newArgs := append([]parser.Node{n.Left}, rc.Args...)
		call = &parser.CallExpr{Callee: rc.Callee, Args: newArgs}
	} else {
		call = &parser.CallExpr{Callee: n.Right, Args: []parser.Node{n.Left}}
	}

	// Type-check call as if the piped value were the unwrapped payload.
	if af, ok := call.Args[0].(*parser.ArrowFunc); ok {
		c.arrowFuncHints[af] = []Type{innerType}
	}
	calleeType := c.checkNode(call.Callee)
	ft, ok := calleeType.(*FuncType)
	if !ok {
		c.errorf(n.Right.Pos(), "|?> lado direito deve ser chamável")
		return Unknown
	}
	if len(ft.Params) > 0 && innerType != Unknown && !c.isAssignable(ft.Params[0], innerType) && !c.isAssignable(innerType, ft.Params[0]) {
		c.errorf(n.Pos(), "|?> argumento incompatível: esperado %s, obtido %s", ft.Params[0], innerType)
	}
	for i := 1; i < len(call.Args); i++ {
		c.checkNode(call.Args[i])
	}

	outType := c.asResultType(ft.Return)
	c.nodeTypes[call] = ft.Return
	c.specializations[n] = &FuncType{Return: outType}
	_ = kind
	return outType
}

// checkAsyncPipeExpr type-checks `a ~> f ~> g` (and ~?> steps).
// For each step the function's first parameter type is verified against the current
// flowing type. The result type is Task[ReturnTypeOfLastStep].
func (c *Checker) checkAsyncPipeExpr(n *parser.AsyncPipeExpr) Type {
	if len(n.Steps) < 2 {
		c.errorf(n.Pos(), "~> requer pelo menos um step além do valor inicial")
		return Unknown
	}

	// Type of the "current" value flowing through the chain.
	currentType := c.checkNode(n.Steps[0])

	for _, rawStep := range n.Steps[1:] {
		// Unwrap ~?> step marker if present.
		isQuestStep := false
		step := rawStep
		if qs, ok := rawStep.(*parser.AsyncPipeQuestStep); ok {
			isQuestStep = true
			step = qs.Step
			// ~?> requires the previous result to be Result[T] or Option[T].
			inner, kind := c.unwrapResultOption(currentType)
			if kind == "" {
				c.errorf(qs.Pos(), "~?> requer Result ou Option à esquerda, obtido %s", currentType)
				return Unknown
			}
			currentType = inner
		}

		// Resolve the step callee and check compatibility.
		// Use a temporary synthetic identifier with the correct current type
		// instead of re-using n.Steps[0] (which has the original type).
		const tmpName = "__async_pipe_tmp__"
		c.scope.Define(tmpName, currentType, true)
		tmpIdent := &parser.Identifier{Name: tmpName}
		c.nodeTypes[tmpIdent] = currentType

		var call *parser.CallExpr
		if rc, ok := step.(*parser.CallExpr); ok {
			call = &parser.CallExpr{Callee: rc.Callee, Args: append([]parser.Node{tmpIdent}, rc.Args...)}
		} else {
			call = &parser.CallExpr{Callee: step, Args: []parser.Node{tmpIdent}}
		}

		retType := c.checkCallExpr(call)
		if ft, ok := c.specializations[call]; ok {
			c.specializations[rawStep] = ft
		}

		if isQuestStep {
			// ~?> wraps the return type in Result if not already.
			retType = c.asResultType(retType)
		}
		currentType = retType
	}

	// The whole expression produces Task[currentType].
	base := c.resolveTypeExpr(&parser.NamedType{Name: "Task"})
	taskType := &SpecializedType{Base: base, Params: []Type{currentType}}
	return taskType
}

func (c *Checker) checkMatchExpr(n *parser.MatchExpr) Type {
	subjectType := c.checkNode(n.Subject)
	var armsType Type = Unknown

	for _, arm := range n.Arms {
		parentScope := c.scope
		c.scope = NewScope(parentScope)

		c.checkPattern(arm.Pattern, subjectType)

		if arm.Guard != nil {
			guardType := c.checkNode(arm.Guard)
			if guardType != BoolType {
				c.errorf(arm.Guard.Pos(), "match guard must be Bool, got %s", guardType)
			}
		}

		bodyType := c.checkNode(arm.Body)
		if armsType == Unknown {
			armsType = bodyType
		} else if !c.isAssignable(armsType, bodyType) && !c.isAssignable(bodyType, armsType) {
			c.errorf(arm.Pos, "arm type incompatible with previous arms: expected %s, got %s", armsType, bodyType)
		}

		c.scope = parentScope
	}

	c.checkMatchExhaustiveness(n, subjectType)
	return armsType
}

func (c *Checker) checkMatchExhaustiveness(n *parser.MatchExpr, subjectType Type) {
	// Unwrap SpecializedType (e.g. Option[T], Result[T], Tree[Int]).
	if st, ok := subjectType.(*SpecializedType); ok {
		subjectType = st.Base
	}
	et, ok := subjectType.(*EnumType)
	if !ok {
		return
	}

	// A wildcard or bare binding pattern covers everything — no check needed.
	for _, arm := range n.Arms {
		if arm.Guard != nil {
			continue // guarded arm may not always fire; skip
		}
		switch arm.Pattern.(type) {
		case *parser.WildcardPattern, *parser.BindingPattern:
			return
		}
	}

	covered := make(map[string]bool)
	for _, arm := range n.Arms {
		if cp, ok := arm.Pattern.(*parser.ConstructorPattern); ok {
			covered[cp.Name] = true
		}
	}

	var missing []string
	for name := range et.Variants {
		if !covered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		// Sort for deterministic error messages.
		for i := 1; i < len(missing); i++ {
			for j := i; j > 0 && missing[j] < missing[j-1]; j-- {
				missing[j], missing[j-1] = missing[j-1], missing[j]
			}
		}
		msg := "match não é exaustivo: variantes não cobertas de " + et.Name + ": "
		for i, m := range missing {
			if i > 0 {
				msg += ", "
			}
			msg += m
		}
		c.errorf(n.Pos(), "%s", msg)
	}
}

func (c *Checker) checkInterpolatedString(n *parser.InterpolatedString) Type {
	for _, part := range n.Parts {
		c.checkNode(part)
	}
	return StringType
}

// checkElvisExpr: `x ?: default` — x must be Option[T], result is T.
func (c *Checker) checkElvisExpr(n *parser.ElvisExpr) Type {
	leftType := c.checkNode(n.Left)
	c.checkNode(n.Right)
	if st, ok := leftType.(*SpecializedType); ok {
		if et, ok := st.Base.(*EnumType); ok && et.Name == "Option" && len(st.Params) > 0 {
			return st.Params[0]
		}
	}
	return Unknown
}

func (c *Checker) checkReturnStmt(n *parser.ReturnStmt) Type {
	var got Type = UnitType
	if n.Value != nil {
		got = c.checkNode(n.Value)
	}
	if c.context.returnType != nil && !c.isAssignable(c.context.returnType, got) {
		c.errorf(n.Pos(), "incompatible return: expected %s, got %s", c.context.returnType, got)
	}
	return got
}

func (c *Checker) checkCallExpr(n *parser.CallExpr) Type {
	var ft *FuncType
	var calleeType Type

	// Safe navigation method call: obj?.method(args)
	if sn, ok := n.Callee.(*parser.SafeNavExpr); ok {
		objType := c.checkNode(sn.Object)
		innerType, kind := c.unwrapResultOption(objType)
		if kind != "Option" {
			c.errorf(sn.Pos(), "safe navigation (?.) requer Option[T], obtido %s", objType)
			return Unknown
		}
		memberType := c.resolveMemberType(innerType, sn.Property, sn.Pos())
		methodFT, ok := memberType.(*FuncType)
		if !ok {
			c.errorf(n.Callee.Pos(), "attempt to call a value that is not a function: %s", memberType)
			return Unknown
		}
		ft = methodFT
		c.specializations[n] = ft
		if len(n.Args) > len(ft.Params) {
			c.errorf(n.Pos(), "argumentos em excesso: esperado %d, encontrado %d", len(ft.Params), len(n.Args))
			return c.wrapOptionType(ft.Return)
		}
		if len(n.Args) < len(ft.Params) {
			c.errorf(n.Pos(), "argumentos insuficientes: esperado %d, encontrado %d", len(ft.Params), len(n.Args))
			return c.wrapOptionType(ft.Return)
		}
		for i, arg := range n.Args {
			if af, ok2 := arg.(*parser.ArrowFunc); ok2 && i < len(ft.Params) {
				if expectedFT, ok3 := ft.Params[i].(*FuncType); ok3 {
					c.arrowFuncHints[af] = expectedFT.Params
				}
			}
			at := c.checkNode(arg)
			if !c.isAssignable(ft.Params[i], at) {
				c.errorf(arg.Pos(), "incompatible argument %d: expected %s, got %s", i+1, ft.Params[i], at)
			}
		}
		return c.wrapOptionType(ft.Return)
	}

	// M7: TaskHandle.current() — returns Option[TaskHandle].
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if ct, isCT := objType.(*ClassType); isCT && ct.Name == "TaskHandle" && me.Property == "current" {
			optEnumRaw, _ := c.scope.Resolve("Option")
			optEnum, _ := optEnumRaw.Type.(*EnumType)
			retType := &SpecializedType{Base: optEnum, Params: []Type{ct}}
			c.specializations[n] = &FuncType{Return: retType}
			return retType
		}
	}

	// M14: Arc.new — static constructor; Arc.clone/get/refcount — instance methods.
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		// Arc.new(val: T) → Arc[T]
		if ct, isCT := objType.(*ClassType); isCT && ct.Name == "Arc" && me.Property == "new" && len(n.Args) == 1 {
			argType := c.checkNode(n.Args[0])
			sym, _ := c.scope.Resolve("Arc")
			baseCT, _ := sym.Type.(*ClassType)
			retType := &SpecializedType{Base: baseCT, Params: []Type{argType}}
			c.specializations[n] = &FuncType{Return: retType}
			return retType
		}
		// arc.clone() → Arc[T]; arc.get() → T; arc.refcount() → Int
		if st, isST := objType.(*SpecializedType); isST {
			if ct, isCT := st.Base.(*ClassType); isCT && ct.Name == "Arc" {
				var innerType Type = Unknown
				if len(st.Params) > 0 {
					innerType = st.Params[0]
				}
				switch me.Property {
				case "clone":
					c.specializations[n] = &FuncType{Return: st}
					return st
				case "get":
					c.specializations[n] = &FuncType{Return: innerType}
					return innerType
				case "refcount":
					return IntType
				}
			}
		}
	}

	// Channel.new(capacity) — static constructor. capacity=0 → rendezvous.
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if ct, isCT := objType.(*ClassType); isCT && me.Property == "new" && ct.Name == "Channel" {
			if len(n.Args) == 1 {
				c.checkNode(n.Args[0]) // capacity: Int
				sym, _ := c.scope.Resolve("Channel")
				baseCT, _ := sym.Type.(*ClassType)
				retType := &SpecializedType{Base: baseCT, Params: []Type{Unknown}}
				c.specializations[n] = &FuncType{Return: retType}
				return retType
			}
		}
	}

	// Channel[T] instance methods — recv/tryRecv return Option[T].
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if st, isST := objType.(*SpecializedType); isST {
			if ct, isCT := st.Base.(*ClassType); isCT {
				switch ct.Name {
				case "Channel":
					var innerType Type = Unknown
					if len(st.Params) > 0 {
						innerType = st.Params[0]
					}
					switch me.Property {
					case "recv", "tryRecv":
						optEnumRaw, _ := c.scope.Resolve("Option")
						optEnum, _ := optEnumRaw.Type.(*EnumType)
						retType := &SpecializedType{Base: optEnum, Params: []Type{innerType}}
						c.specializations[n] = &FuncType{Return: retType}
						return retType
					case "send":
						if len(n.Args) == 1 {
							c.checkNode(n.Args[0])
						}
						return UnitType
					case "close":
						return UnitType
					case "isClosed":
						return BoolType
					}
				}
			}
		}
	}

	// M8: Mutex.new / RwLock.new / Atomic.new — static constructors.
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if ct, isCT := objType.(*ClassType); isCT && me.Property == "new" && len(n.Args) == 1 {
			switch ct.Name {
			case "Mutex", "RwLock", "Atomic":
				argType := c.checkNode(n.Args[0])
				sym, _ := c.scope.Resolve(ct.Name)
				baseCT, _ := sym.Type.(*ClassType)
				retType := &SpecializedType{Base: baseCT, Params: []Type{argType}}
				c.specializations[n] = &FuncType{Return: retType}
				return retType
			}
		}
	}

	// M6: Task.all / Task.any / Task.allSettled — static combinators on the Task namespace.
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if ct, isCT := objType.(*ClassType); isCT && ct.Name == "Task" {
			switch me.Property {
			case "all", "allSettled":
				innerTypes := make([]Type, len(n.Args))
				for i, arg := range n.Args {
					at := c.checkNode(arg)
					if st, ok := at.(*SpecializedType); ok {
						if baseCT, ok2 := st.Base.(*ClassType); ok2 && baseCT.Name == "Task" && len(st.Params) > 0 {
							innerTypes[i] = st.Params[0]
						} else {
							c.errorf(arg.Pos(), "Task.%s espera Task[T], obtido %s", me.Property, at)
							innerTypes[i] = Unknown
						}
					} else {
						c.errorf(arg.Pos(), "Task.%s espera Task[T], obtido %s", me.Property, at)
						innerTypes[i] = Unknown
					}
				}
				retType := &TupleType{Elements: innerTypes}
				c.specializations[n] = &FuncType{Return: retType}
				return retType
			case "fan":
				// M-18: Task.fan(input, f, g, h, ...) — fan-out paralelo.
				// Primeiro argumento: valor de entrada (não função).
				// Argumentos restantes: funções T -> Ai.
				// Uso típico: entrada |> Task.fan(f, g, h)
				if len(n.Args) < 2 {
					c.errorf(n.Pos(), "Task.fan requer ao menos um valor de entrada e uma função")
					return Unknown
				}
				firstType := c.checkNode(n.Args[0])
				if _, isFn := firstType.(*FuncType); isFn {
					c.errorf(n.Args[0].Pos(), "Task.fan: use entrada |> Task.fan(f, g, ...) — primeiro argumento deve ser o valor de entrada, não uma função")
					return Unknown
				}
				taskBase := c.resolveTypeExpr(&parser.NamedType{Name: "Task"})
				taskTypes := make([]Type, len(n.Args)-1)
				for i, arg := range n.Args[1:] {
					ft := c.checkNode(arg)
					fnType, ok := ft.(*FuncType)
					if !ok {
						c.errorf(arg.Pos(), "Task.fan: argumento %d deve ser uma função, obtido %s", i+1, ft)
						taskTypes[i] = Unknown
						continue
					}
					taskTypes[i] = &SpecializedType{Base: taskBase, Params: []Type{fnType.Return}}
				}
				retType := &TupleType{Elements: taskTypes}
				c.specializations[n] = &FuncType{Return: retType}
				return retType
			case "pipe":
				// M-19: Task.pipe(input, f, g, h, ...) — pipeline paralelo com channels.
				// Task.pipe(inCh: Channel[T], f, g, h) também aceita Channel[T] como input.
				// Retorna Channel[R] onde R é o tipo de retorno do último stage.
				if len(n.Args) < 2 {
					c.errorf(n.Pos(), "Task.pipe requer ao menos um valor de entrada e uma função de stage")
					return Unknown
				}
				firstType := c.checkNode(n.Args[0])
				// Determine input element type: Channel[T] → T, plain value → keep type.
				var elemType Type
				if st, isST := firstType.(*SpecializedType); isST {
					if ct2, isCT2 := st.Base.(*ClassType); isCT2 && ct2.Name == "Channel" && len(st.Params) > 0 {
						elemType = st.Params[0]
					} else {
						elemType = firstType
					}
				} else {
					elemType = firstType
				}
				currentType := elemType
				for i, arg := range n.Args[1:] {
					ft := c.checkNode(arg)
					fnType, ok := ft.(*FuncType)
					if !ok {
						c.errorf(arg.Pos(), "Task.pipe: argumento %d deve ser uma função, obtido %s", i+1, ft)
						return Unknown
					}
					if len(fnType.Params) > 0 && currentType != Unknown &&
						!c.isAssignable(fnType.Params[0], currentType) {
						c.errorf(arg.Pos(), "Task.pipe: tipo de entrada %s incompatível com parâmetro %s da função no stage %d",
							currentType, fnType.Params[0], i+1)
					}
					currentType = fnType.Return
				}
				chanBase := c.resolveTypeExpr(&parser.NamedType{Name: "Channel"})
				retType := &SpecializedType{Base: chanBase, Params: []Type{currentType}}
				c.specializations[n] = &FuncType{Return: retType}
				return retType
			case "gather":
				// Task.gather(list: List[T], fn: T -> U) -> List[U]
				// Parallel map: spawns fn(item) for each item, awaits all, returns List[U].
				if len(n.Args) != 2 {
					c.errorf(n.Pos(), "Task.gather requer exatamente 2 argumentos: Task.gather(list, fn)")
					return Unknown
				}
				listType := c.checkNode(n.Args[0])
				var elemType Type = Unknown
				if st, ok := listType.(*SpecializedType); ok {
					if ct2, ok2 := st.Base.(*ClassType); ok2 && ct2.Name == "List" && len(st.Params) > 0 {
						elemType = st.Params[0]
					} else {
						c.errorf(n.Args[0].Pos(), "Task.gather: primeiro argumento deve ser List[T], obtido %s", listType)
					}
				} else {
					c.errorf(n.Args[0].Pos(), "Task.gather: primeiro argumento deve ser List[T], obtido %s", listType)
				}
				if af, ok := n.Args[1].(*parser.ArrowFunc); ok {
					c.arrowFuncHints[af] = []Type{elemType}
				}
				fnType2 := c.checkNode(n.Args[1])
				var retElemType Type = Unknown
				if ft, ok := fnType2.(*FuncType); ok {
					retElemType = ft.Return
				} else {
					c.errorf(n.Args[1].Pos(), "Task.gather: segundo argumento deve ser uma função T -> U, obtido %s", fnType2)
				}
				listBase2 := c.resolveTypeExpr(&parser.NamedType{Name: "List"})
				retType := &SpecializedType{Base: listBase2, Params: []Type{retElemType}}
				c.specializations[n] = &FuncType{Return: retType}
				return retType
			}
		}
	}

	// Task[T] instance callbacks — .tap(fn: T -> Unit) -> Task[T]
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if st, isST := objType.(*SpecializedType); isST && len(st.Params) > 0 {
			if ct, isCT := st.Base.(*ClassType); isCT && ct.Name == "Task" {
				switch me.Property {
				case "tap":
					if len(n.Args) != 1 {
						c.errorf(n.Pos(), ".tap espera exatamente um argumento (fn: T -> Unit)")
						return Unknown
					}
					innerType := st.Params[0]
					if af, ok2 := n.Args[0].(*parser.ArrowFunc); ok2 {
						c.arrowFuncHints[af] = []Type{innerType}
					}
					c.checkNode(n.Args[0])
					c.specializations[n] = &FuncType{Return: st}
					return st
				case "always":
					// .always(fn: Unit -> Unit) -> Task[T]
					if len(n.Args) != 1 {
						c.errorf(n.Pos(), ".always espera exatamente um argumento (fn: Unit -> Unit)")
						return Unknown
					}
					c.checkNode(n.Args[0])
					c.specializations[n] = &FuncType{Return: st}
					return st
				}
			}
		}
	}

	// M3 HOF: special-case for List[T] functional methods and Map[K,V] key/value extraction.
	// Must run before generic overload resolution so we can return the correct concrete types.
	if me, isME := n.Callee.(*parser.MemberExpr); isME {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if st, isST := objType.(*SpecializedType); isST && len(st.Params) > 0 {
			if ct, isCT := st.Base.(*ClassType); isCT && ct.Name == "List" {
				elemType := st.Params[0]
				switch me.Property {
				case "map":
					if len(n.Args) == 1 {
						if af, ok2 := n.Args[0].(*parser.ArrowFunc); ok2 {
							c.arrowFuncHints[af] = []Type{elemType}
						}
						lambdaType := c.checkNode(n.Args[0])
						retElemType := Type(Unknown)
						if ft2, ok2 := lambdaType.(*FuncType); ok2 {
							retElemType = ft2.Return
						}
						resultType := &SpecializedType{Base: ct, Params: []Type{retElemType}}
						c.specializations[n] = &FuncType{Return: resultType}
						return resultType
					}
				case "filter":
					if len(n.Args) == 1 {
						if af, ok2 := n.Args[0].(*parser.ArrowFunc); ok2 {
							c.arrowFuncHints[af] = []Type{elemType}
						}
						c.checkNode(n.Args[0])
						c.specializations[n] = &FuncType{Return: st}
						return st
					}
				case "reduce":
					if len(n.Args) == 2 {
						initType := c.checkNode(n.Args[1])
						if af, ok2 := n.Args[0].(*parser.ArrowFunc); ok2 {
							c.arrowFuncHints[af] = []Type{initType, elemType}
						}
						c.checkNode(n.Args[0])
						c.specializations[n] = &FuncType{Return: initType}
						return initType
					}
				case "join":
					if len(n.Args) == 1 {
						c.checkNode(n.Args[0])
						c.specializations[n] = &FuncType{Return: StringType}
						return StringType
					}
				case "isEmpty":
					c.specializations[n] = &FuncType{Return: BoolType}
					return BoolType
				case "iter":
					if len(n.Args) == 0 {
						if iterSym, ok2 := c.scope.Resolve("Iterator"); ok2 {
							resultType := &SpecializedType{Base: iterSym.Type, Params: []Type{elemType}}
							c.specializations[n] = &FuncType{Return: resultType}
							return resultType
						}
					}
				case "set":
					if len(n.Args) == 2 {
						c.checkNode(n.Args[0])
						c.checkNode(n.Args[1])
						c.specializations[n] = &FuncType{Return: UnitType}
						return UnitType
					}
				case "remove":
					if len(n.Args) == 1 {
						c.checkNode(n.Args[0])
						c.specializations[n] = &FuncType{Return: elemType}
						return elemType
					}
				case "pop":
					if len(n.Args) == 0 {
						c.specializations[n] = &FuncType{Return: elemType}
						return elemType
					}
				case "prepend":
					if len(n.Args) == 1 {
						c.checkNode(n.Args[0])
						c.specializations[n] = &FuncType{Return: UnitType}
						return UnitType
					}
				case "clear":
					if len(n.Args) == 0 {
						c.specializations[n] = &FuncType{Return: UnitType}
						return UnitType
					}
				case "copy":
					if len(n.Args) == 0 {
						resultType := &SpecializedType{Base: ct, Params: []Type{elemType}}
						c.specializations[n] = &FuncType{Return: resultType}
						return resultType
					}
				case "concat":
					if len(n.Args) == 1 {
						c.checkNode(n.Args[0])
						resultType := &SpecializedType{Base: ct, Params: []Type{elemType}}
						c.specializations[n] = &FuncType{Return: resultType}
						return resultType
					}
				}
			}
			if ct, isCT := st.Base.(*ClassType); isCT && ct.Name == "Map" && len(st.Params) == 2 {
				switch me.Property {
				case "keys":
					if listSym, ok2 := c.scope.Resolve("List"); ok2 {
						resultType := &SpecializedType{Base: listSym.Type, Params: []Type{st.Params[0]}}
						c.specializations[n] = &FuncType{Return: resultType}
						return resultType
					}
				case "values":
					if listSym, ok2 := c.scope.Resolve("List"); ok2 {
						resultType := &SpecializedType{Base: listSym.Type, Params: []Type{st.Params[1]}}
						c.specializations[n] = &FuncType{Return: resultType}
						return resultType
					}
				case "iter":
					if len(n.Args) == 0 {
						if iterSym, ok2 := c.scope.Resolve("Iterator"); ok2 {
							resultType := &SpecializedType{Base: iterSym.Type, Params: []Type{st.Params[0]}}
							c.specializations[n] = &FuncType{Return: resultType}
							return resultType
						}
					}
				}
			}
			if ct, isCT := st.Base.(*ClassType); isCT && ct.Name == "Iterator" && len(st.Params) > 0 {
				elemType := st.Params[0]
				switch me.Property {
				case "next":
					if len(n.Args) == 0 {
						optBase := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
						resultType := &SpecializedType{Base: optBase, Params: []Type{elemType}}
						c.specializations[n] = &FuncType{Return: resultType}
						return resultType
					}
				case "isEmpty":
					if len(n.Args) == 0 {
						c.specializations[n] = &FuncType{Return: BoolType}
						return BoolType
					}
				}
			}
		}
	}

	// M5: overloaded method resolution
	// so that we pick the correct variant by arity instead of defaulting to variants[0].
	if me, ok := n.Callee.(*parser.MemberExpr); ok {
		objType := c.nodeTypes[me.Object]
		if objType == nil {
			objType = c.checkNode(me.Object)
		}
		if ct, isCT := objType.(*ClassType); isCT {
			if variants, hasMethod := ct.Methods[me.Property]; hasMethod && len(variants) > 1 {
				arity := len(n.Args)
				for _, v := range variants {
					if len(v.Params) == arity {
						ft = v
						break
					}
				}
				if ft == nil {
					c.errorf(n.Pos(), "nenhuma sobrecarga de '%s' aceita %d argumento(s)", me.Property, arity)
					return Unknown
				}
				c.specializations[n] = ft
				for i, arg := range n.Args {
					if af, ok2 := arg.(*parser.ArrowFunc); ok2 && i < len(ft.Params) {
						if expectedFT, ok3 := ft.Params[i].(*FuncType); ok3 {
							c.arrowFuncHints[af] = expectedFT.Params
						}
					}
					at := c.checkNode(arg)
					if !c.isAssignable(ft.Params[i], at) {
						c.errorf(arg.Pos(), "incompatible argument %d: expected %s, got %s", i+1, ft.Params[i], at)
					}
				}
				return ft.Return
			}
		}
	}

	if genericCall, ok := n.Callee.(*parser.IndexExpr); ok {
		calleeType = c.checkNode(genericCall.Object)
		if f, ok := calleeType.(*FuncType); ok {
			specType := c.checkNode(genericCall.Index)
			ft = c.instantiateFunc(f, []Type{specType})
		}
	} else if se, ok := n.Callee.(*parser.SpecializedExpr); ok {
		calleeType = c.checkSpecializedExpr(se)
		if f, ok := calleeType.(*FuncType); ok {
			ft = f
		}
	} else {
		calleeType = c.checkNode(n.Callee)
		if f, ok := calleeType.(*FuncType); ok {
			if len(f.Generics) > 0 && len(n.Args) > 0 {
				ft = c.instantiateFunc(f, c.inferGenerics(f, n.Args))
			} else {
				ft = f
			}
		}
	}

	if ft == nil {
		if calleeType != Unknown {
			c.errorf(n.Callee.Pos(), "attempt to call a value that is not a function: %s", calleeType)
		}
		return Unknown
	}

	c.specializations[n] = ft

	// M3: For each arg at an optional param position, apply auto-wrap rules:
	//   _ → NoneLiteral (explicit None placeholder)
	//   SomeExpr / NoneLiteral → leave as-is (already wrapped)
	//   anything else → wrap with SomeExpr automatically
	if len(ft.IsOptional) > 0 {
		for i := range n.Args {
			if i >= len(ft.IsOptional) || !ft.IsOptional[i] {
				continue
			}
			arg := n.Args[i]
			if id, ok := arg.(*parser.Identifier); ok && id.Name == "_" {
				n.Args[i] = &parser.NoneLiteral{}
			} else {
				switch arg.(type) {
				case *parser.SomeExpr, *parser.NoneLiteral:
					// already explicitly wrapped
				default:
					n.Args[i] = &parser.SomeExpr{Value: arg}
				}
			}
		}
	}

	if fieldNames := c.variantFieldNamesForCall(n.Callee); len(fieldNames) > 0 {
		c.resolveNamedCallArgs(n, ft, fieldNames)
	}

	c.applyEnumParamSugar(n, ft)

	// M4b: currying — detect _ placeholder in non-optional positions
	if c.hasCurryPlaceholder(n, ft) {
		return c.checkCurryCall(n, ft)
	}

	if len(n.Args) > len(ft.Params) {
		c.errorf(n.Pos(), "argumentos em excesso: esperado %d, encontrado %d", len(ft.Params), len(n.Args))
		return ft.Return
	}
	if len(n.Args) < len(ft.Params) {
		// Allow short call only when all missing params have defaults.
		missingFrom := len(n.Args)
		hasAllDefaults := len(ft.Defaults) >= len(ft.Params)
		if hasAllDefaults {
			for i := missingFrom; i < len(ft.Params); i++ {
				if ft.Defaults[i] == nil {
					hasAllDefaults = false
					break
				}
			}
		}
		if !hasAllDefaults {
			c.errorf(n.Pos(), "argumentos insuficientes: esperado %d, encontrado %d", len(ft.Params), len(n.Args))
			return ft.Return
		}
		c.synthCallArgs[n] = ft.Defaults[missingFrom:]
	}
	for i, arg := range n.Args {
		// M4a: inject param type hints for ArrowFunc args when expected type is a FuncType
		if af, ok2 := arg.(*parser.ArrowFunc); ok2 && i < len(ft.Params) {
			if expectedFT, ok3 := ft.Params[i].(*FuncType); ok3 {
				c.arrowFuncHints[af] = expectedFT.Params
			}
		}
		at := c.checkNode(arg)
		if !c.isAssignable(ft.Params[i], at) {
			c.errorf(arg.Pos(), "incompatible argument %d: expected %s, got %s", i+1, ft.Params[i], at)
		}
	}
	return ft.Return
}

// hasCurryPlaceholder reports whether any non-optional arg position holds a bare _ identifier.
func (c *Checker) hasCurryPlaceholder(n *parser.CallExpr, ft *FuncType) bool {
	for i, arg := range n.Args {
		if id, ok := arg.(*parser.Identifier); ok && id.Name == "_" {
			if i >= len(ft.IsOptional) || !ft.IsOptional[i] {
				return true
			}
		}
	}
	return false
}

// checkCurryCall rewrites call(a, _, c) into fn(__curry_0) => call(a, __curry_0, c)
// and checks the resulting closure, storing the mapping for codegen.
func (c *Checker) checkCurryCall(n *parser.CallExpr, ft *FuncType) Type {
	var curryParams []parser.FuncParam
	var curryParamHints []Type
	newArgs := make([]parser.Node, len(n.Args))
	curryIdx := 0

	for i, arg := range n.Args {
		isPlaceholder := false
		if id, ok := arg.(*parser.Identifier); ok && id.Name == "_" {
			if i >= len(ft.IsOptional) || !ft.IsOptional[i] {
				isPlaceholder = true
			}
		}
		if isPlaceholder && i < len(ft.Params) {
			paramName := fmt.Sprintf("__curry_%d", curryIdx)
			curryIdx++
			curryParams = append(curryParams, parser.FuncParam{
				Pattern: &parser.BindingPattern{Name: paramName},
			})
			curryParamHints = append(curryParamHints, ft.Params[i])
			newArgs[i] = &parser.Identifier{Name: paramName}
		} else {
			newArgs[i] = arg
		}
	}

	innerCall := &parser.CallExpr{Callee: n.Callee, Args: newArgs}
	af := &parser.ArrowFunc{Params: curryParams, Body: innerCall}
	c.arrowFuncHints[af] = curryParamHints
	c.curriedCalls[n] = af

	afType := c.checkArrowFunc(af)
	c.nodeTypes[af] = afType
	return afType
}

func (c *Checker) checkSpecialization(n *parser.IndexExpr) Type {
	base := c.checkNode(n.Object)
	var param Type = Unknown
	if id, ok := n.Index.(*parser.Identifier); ok {
		param = c.resolveTypeExpr(&parser.NamedType{Name: id.Name})
	} else if te := typeExprFromNode(n.Index); te != nil {
		param = c.resolveTypeExpr(te)
	} else {
		param = c.checkNode(n.Index)
	}
	return &SpecializedType{Base: base, Params: []Type{param}}
}

func typeExprFromNode(n parser.Node) parser.TypeExpr {
	switch v := n.(type) {
	case *parser.Identifier:
		return &parser.NamedType{Name: v.Name}
	default:
		return nil
	}
}

func (c *Checker) checkIdentifier(n *parser.Identifier) Type {
	switch n.Name {
	case "Int":
		return IntType
	case "Float":
		return FloatType
	case "String":
		return StringType
	case "Bool":
		return BoolType
	case "Unit":
		return UnitType
	}
	sym, ok := c.scope.Resolve(n.Name)
	if !ok {
		c.errorf(n.Pos(), "undefined identifier: %s", n.Name)
		return Unknown
	}
	c.checkGlobalAccess(n.Name, n.Pos())
	// Auto-specialize zero-arg generic enum constructors used as values
	// (e.g. `Nothing` of type `[T]() -> Maybe[T]` becomes `Maybe[Unknown]`).
	if ft, ok := sym.Type.(*FuncType); ok && len(ft.Params) == 0 && len(ft.Generics) > 0 {
		if _, ok2 := ft.Return.(*SpecializedType); ok2 {
			sub := make(map[string]Type)
			for _, name := range ft.Generics {
				sub[name] = Unknown
			}
			return c.substitute(ft.Return, sub)
		}
	}
	return sym.Type
}

func (c *Checker) checkBinaryExpr(n *parser.BinaryExpr) Type {
	left := c.checkNode(n.Left)
	right := c.checkNode(n.Right)

	switch n.Operator {
	case "+", "-", "*", "/", "%":
		if left == IntType && right == IntType {
			return IntType
		}
		if left == FloatType && right == FloatType {
			return FloatType
		}
		c.errorf(n.Pos(), "operator %s not supported for %s and %s", n.Operator, left, right)
		return Unknown
	case "==", "!=":
		if !c.isAssignable(left, right) && !c.isAssignable(right, left) {
			c.errorf(n.Pos(), "comparison of incompatible types: %s and %s", left, right)
		}
		return BoolType
	case "<", ">", "<=", ">=":
		if (left == IntType || left == FloatType || left == CharType) && left.String() == right.String() {
			return BoolType
		}
		c.errorf(n.Pos(), "order comparison not supported for %s and %s", left, right)
		return BoolType
	case "&&", "||":
		if left != BoolType || right != BoolType {
			c.errorf(n.Pos(), "logical operators require Bool, got %s and %s", left, right)
		}
		return BoolType
	case "&", "|", "^", "<<", ">>":
		if left != IntType || right != IntType {
			c.errorf(n.Pos(), "operador bitwise %s requer Int, recebeu %s e %s", n.Operator, left, right)
		}
		return IntType
	}
	return Unknown
}

func (c *Checker) checkUnaryExpr(n *parser.UnaryExpr) Type {
	operand := c.checkNode(n.Operand)
	switch n.Operator {
	case "-":
		if operand != IntType && operand != FloatType {
			c.errorf(n.Pos(), "unary - not supported for %s", operand)
		}
		return operand
	case "!":
		if operand != BoolType {
			c.errorf(n.Pos(), "unary ! requires Bool, got %s", operand)
		}
		return BoolType
	case "~":
		if operand != IntType {
			c.errorf(n.Pos(), "operador ~ requer Int, recebeu %s", operand)
		}
		return IntType
	}
	return Unknown
}

func (c *Checker) checkAssignExpr(n *parser.AssignExpr) Type {
	leftType := c.checkNode(n.Left)
	rightType := c.checkNode(n.Right)

	switch l := n.Left.(type) {
	case *parser.Identifier:
		sym, ok := c.scope.Resolve(l.Name)
		if ok && sym.IsReadonly {
			c.errorf(n.Pos(), "cannot assign to immutable variable (val): %s", l.Name)
		}
	case *parser.MemberExpr:
		// Assignment to field (e.g. self.count = 10)
		// For now we assume all fields are mutable if they belong to a class or record.
		// In the future we might check for 'val' vs 'var' fields.
	default:
		c.errorf(n.Pos(), "left side of assignment must be a variable or field")
	}

	if !c.isAssignable(leftType, rightType) {
		c.errorf(n.Pos(), "assignment of incompatible types: %s and %s", leftType, rightType)
	}
	return rightType
}


func (c *Checker) checkForStmt(n *parser.ForStmt) Type {
	parent := c.scope
	c.scope = NewScope(parent)
	defer func() { c.scope = parent }()

	var bindingType Type = IntType
	if _, ok := n.Iterable.(*parser.RangeExpr); !ok {
		iterType := c.checkNode(n.Iterable)
		if st, ok := iterType.(*SpecializedType); ok {
			if ct, ok2 := st.Base.(*ClassType); ok2 && len(st.Params) > 0 {
				switch ct.Name {
				case "List", "Iterator":
					bindingType = st.Params[0]
				case "Map":
					bindingType = st.Params[0] // iterate keys
				default:
					c.errorf(n.Iterable.Pos(), "for-in: tipo '%s' não é iterável", iterType)
				}
			} else {
				c.errorf(n.Iterable.Pos(), "for-in: tipo '%s' não é iterável", iterType)
			}
		} else {
			c.errorf(n.Iterable.Pos(), "for-in: tipo '%s' não é iterável", iterType)
		}
	}

	c.scope.Define(n.Binding, bindingType, false)
	c.enterLoop()
	c.checkBlock(n.Body)
	c.leaveLoop()
	return UnitType
}

func (c *Checker) checkBlock(n *parser.BlockStmt) Type {
	parent := c.scope
	c.scope = NewScope(parent)
	defer func() { c.scope = parent }()

	var lastType Type = UnitType
	for _, stmt := range n.Statements {
		lastType = c.checkNode(stmt)
	}
	return lastType
}

func (c *Checker) checkRecordLiteral(n *parser.RecordLiteral) Type {
	sym, ok := c.scope.Resolve(n.Name)
	if !ok {
		c.errorf(n.Pos(), "undefined type: %s", n.Name)
		return Unknown
	}
	c.checkGlobalAccess(n.Name, n.Pos())

	var fields map[string]Type
	var typeName string
	var resultType Type
	var generics []string

	switch t := sym.Type.(type) {
	case *RecordType:
		fields, typeName, resultType, generics = t.Fields, t.Name, t, t.Generics
	case *ClassType:
		fields, typeName, resultType, generics = t.Fields, t.Name, t, t.Generics
	default:
		c.errorf(n.Pos(), "%s is not a record or class", n.Name)
		return Unknown
	}

	inferred := make(map[string]Type)
	provided := make(map[string]bool)
	for _, f := range n.Fields {
		expected, ok := fields[f.Name]
		if !ok {
			c.errorf(f.Pos, "%s %s does not have field %s", getKindName(resultType), typeName, f.Name)
			continue
		}
		expectedForCheck := expected
		if len(inferred) > 0 {
			sub := make(map[string]Type, len(inferred))
			for k, v := range inferred {
				sub[k] = v
			}
			expectedForCheck = c.substitute(expected, sub)
		}
		prevExpected := c.context.expectedType
		c.context.expectedType = expectedForCheck
		got := c.checkNode(f.Value)
		c.context.expectedType = prevExpected
		c.unify(expected, got, inferred)

		if !c.isAssignable(expected, got) {
			// If it's a type parameter, it's always assignable (unify handles it).
			// If not, it's a hard error.
			if _, ok := expected.(*TypeParameter); !ok {
				c.errorf(f.Pos, "incompatible type for field %s: expected %s, got %s", f.Name, expected, got)
			}
		}
		provided[f.Name] = true
	}

	if len(generics) > 0 && len(n.TypeArgs) == 0 {
		for _, name := range generics {
			if t, ok := inferred[name]; !ok || t == Unknown {
				c.errorf(n.Pos(), "não foi possível inferir parâmetros de tipo para %s; use anotação explícita", typeName)
				break
			}
		}
	}

	for name := range fields {
		if !provided[name] {
			// M5: class fields with default init are optional — inject synthetic field.
			if ct, ok2 := resultType.(*ClassType); ok2 {
				if initNode, hasDefault := ct.FieldInit[name]; hasDefault {
					c.checkNode(initNode) // register node type for codegen
					n.Fields = append(n.Fields, parser.RecordLiteralField{
						Pos:   n.Pos(),
						Name:  name,
						Value: initNode,
					})
					continue
				}
			}
			c.errorf(n.Pos(), "missing field in initialization of %s: %s", typeName, name)
		}
	}

	if len(n.TypeArgs) > 0 {
		specParams := c.typeArgsFromExprs(n.TypeArgs)
		if len(specParams) != len(generics) {
			c.errorf(n.Pos(), "esperado %d argumentos de tipo, encontrado %d", len(generics), len(specParams))
			return Unknown
		}
		return &SpecializedType{Base: resultType, Params: specParams}
	}

	if len(generics) > 0 {
		var specParams []Type
		for _, name := range generics {
			if t, ok := inferred[name]; ok {
				specParams = append(specParams, t)
			} else {
				specParams = append(specParams, Unknown)
			}
		}
		return &SpecializedType{Base: resultType, Params: specParams}
	}

	return resultType
}

func (c *Checker) resolveTypeExpr(e parser.TypeExpr) Type {
	if e == nil {
		return Unknown
	}
	switch t := e.(type) {
	case *parser.FuncType:
		var params []Type
		for _, p := range t.ParamTypes {
			params = append(params, c.resolveTypeExpr(p))
		}
		ret := c.resolveTypeExpr(t.ReturnType)
		return &FuncType{Params: params, Return: ret}
	case *parser.NamedType:
		switch t.Name {
		case "Int":
			return IntType
		case "Float":
			return FloatType
		case "String":
			return StringType
		case "Bool":
			return BoolType
		case "Char":
			return CharType
		case "Unit":
			return UnitType
		case "Error":
			if sym, ok := c.scope.Resolve("Error"); ok {
				return sym.Type
			}
			return Unknown
		}
		if sym, ok := c.scope.Resolve(t.Name); ok {
			c.checkGlobalAccess(t.Name, t.TypePos())
			return sym.Type
		}
		c.errorf(t.TypePos(), "unknown type: %s", t.Name)
		return Unknown
	case *parser.GenericType:
		base := c.resolveTypeExpr(&parser.NamedType{Name: t.Name})
		var params []Type
		for _, p := range t.Params {
			params = append(params, c.resolveTypeExpr(p))
		}
		return &SpecializedType{Base: base, Params: params}
	case *parser.OptionalType:
		inner := c.resolveTypeExpr(t.Inner)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
		return &SpecializedType{Base: base, Params: []Type{inner}}
	case *parser.TupleType:
		if len(t.Elements) == 0 {
			return UnitType
		}
		elems := make([]Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = c.resolveTypeExpr(e)
		}
		return &TupleType{Elements: elems}
	}
	return Unknown
}

// enumParamInner reports whether param is Option[T] or Result[T] and returns the inner type T.
func (c *Checker) enumParamInner(param Type, enumName string) (Type, bool) {
	st, ok := param.(*SpecializedType)
	if !ok {
		return Unknown, false
	}
	et, ok := st.Base.(*EnumType)
	if !ok || et.Name != enumName || len(st.Params) == 0 {
		return Unknown, false
	}
	return st.Params[0], true
}

func (c *Checker) errorInterfaceType() Type {
	if sym, ok := c.scope.Resolve("Error"); ok {
		return sym.Type
	}
	return Unknown
}

func (c *Checker) implementsError(t Type) bool {
	errType := c.errorInterfaceType()
	if errType == Unknown || t == Unknown {
		return false
	}
	if c.isAssignable(errType, t) {
		return true
	}
	if it, ok := t.(*InterfaceType); ok && it.Name == "Error" {
		return true
	}
	return false
}

// applyEnumParamSugar auto-wraps bare values for explicit Option[T] and Result[T] params.
// T? params are handled separately via IsOptional.
func (c *Checker) applyEnumParamSugar(n *parser.CallExpr, ft *FuncType) {
	for i := range n.Args {
		if i >= len(ft.Params) {
			break
		}
		if i < len(ft.IsOptional) && ft.IsOptional[i] {
			continue
		}
		param := ft.Params[i]
		arg := n.Args[i]

		if _, ok := c.enumParamInner(param, "Option"); ok {
			switch arg.(type) {
			case *parser.SomeExpr, *parser.NoneLiteral:
			default:
				n.Args[i] = &parser.SomeExpr{Value: arg}
			}
			continue
		}

		if _, ok := c.enumParamInner(param, "Result"); ok {
			switch arg.(type) {
			case *parser.OkExpr, *parser.ErrExpr:
			default:
				at := c.checkNode(arg)
				if c.implementsError(at) {
					n.Args[i] = &parser.ErrExpr{Value: arg}
				} else {
					n.Args[i] = &parser.OkExpr{Value: arg}
				}
			}
		}
	}
}

func (c *Checker) isAssignable(expected, actual Type) bool {
	if expected == Unknown || actual == Unknown {
		return true
	}
	if expected.String() == actual.String() {
		return true
	}
	if _, ok := expected.(*TypeParameter); ok {
		return true
	}
	if it, ok := expected.(*InterfaceType); ok {
		if ct, ok := actual.(*ClassType); ok {
			for _, impl := range ct.Implements {
				if impl.Name == it.Name {
					return true
				}
			}
		}
	}
	// Zero-argument enum constructor used as value (e.g. `Red` where `Color` is expected,
	// or `Nothing` where `Maybe[T]` is expected).
	if ft, ok := actual.(*FuncType); ok && len(ft.Params) == 0 {
		switch ft.Return.(type) {
		case *EnumType, *SpecializedType:
			return c.isAssignable(expected, ft.Return)
		}
	}
	if st, ok := expected.(*SpecializedType); ok {
		if actual.String() == st.Base.String() {
			return true
		}
		if actSt, ok := actual.(*SpecializedType); ok {
			if actSt.Base.String() == st.Base.String() {
				// Assignable when expected has TypeParameters (generic function params)
				// or when actual has Unknown params (uninferred generic)
				hasTypeParam := false
				for _, p := range st.Params {
					if _, ok := p.(*TypeParameter); ok {
						hasTypeParam = true
						break
					}
				}
				if hasTypeParam {
					return true
				}
				if len(actSt.Params) > 0 && actSt.Params[0] == Unknown {
					return true
				}
				// Both sides concrete — check params pairwise
				if len(st.Params) == len(actSt.Params) {
					allOk := true
					for i := range st.Params {
						if !c.isAssignable(st.Params[i], actSt.Params[i]) {
							allOk = false
							break
						}
					}
					if allOk {
						return true
					}
				}
				// Actual still has free type params (e.g. Arvore[T] passed where Arvore[Int] expected).
				inferred := make(map[string]Type)
				for i := range st.Params {
					c.unify(actSt.Params[i], st.Params[i], inferred)
				}
				allOk := true
				for i, p := range actSt.Params {
					if tp, ok := p.(*TypeParameter); ok {
						if bound, exists := inferred[tp.Name]; !exists || !c.isAssignable(st.Params[i], bound) {
							allOk = false
							break
						}
					} else if !c.isAssignable(st.Params[i], p) {
						allOk = false
						break
					}
				}
				if allOk {
					return true
				}
			}
		}
	}
	return false
}

func (c *Checker) isCompatibleFunc(expected, actual *FuncType) bool {
	if len(expected.Params) != len(actual.Params) {
		return false
	}
	for i := range expected.Params {
		if expected.Params[i].String() != actual.Params[i].String() {
			return false
		}
	}
	return expected.Return.String() == actual.Return.String()
}

func (c *Checker) enterLoop() {
	c.loopDepth++
}

func (c *Checker) leaveLoop() {
	c.loopDepth--
}

func (c *Checker) checkWhileStmt(n *parser.WhileStmt) Type {
	c.enterLoop()
	c.checkNode(n.Condition)
	c.checkBlock(n.Body)
	c.leaveLoop()
	return UnitType
}

func (c *Checker) checkLoopStmt(n *parser.LoopStmt) Type {
	c.enterLoop()
	c.loopBreakTypes = append(c.loopBreakTypes, Unknown)
	c.checkBlock(n.Body)
	result := c.loopBreakTypes[len(c.loopBreakTypes)-1]
	c.loopBreakTypes = c.loopBreakTypes[:len(c.loopBreakTypes)-1]
	c.leaveLoop()
	if result == Unknown {
		return UnitType
	}
	return result
}

func (c *Checker) checkBreakStmt(n *parser.BreakStmt) Type {
	if c.loopDepth == 0 {
		c.errorf(n.Pos(), "break outside of loop")
		return UnitType
	}
	if n.Value != nil {
		valType := c.checkNode(n.Value)
		idx := len(c.loopBreakTypes) - 1
		if c.loopBreakTypes[idx] == Unknown {
			c.loopBreakTypes[idx] = valType
		} else if !c.isAssignable(c.loopBreakTypes[idx], valType) && !c.isAssignable(valType, c.loopBreakTypes[idx]) {
			c.errorf(n.Value.Pos(), "break value incompatible with previous breaks: expected %s, got %s",
				c.loopBreakTypes[idx], valType)
		}
	}
	return UnitType
}

func (c *Checker) checkContinueStmt(n *parser.ContinueStmt) Type {
	if c.loopDepth == 0 {
		c.errorf(n.Pos(), "continue outside of loop")
	}
	return UnitType
}
