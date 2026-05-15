package checker

import (
	"fmt"
	"strconv"
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

func (c *Checker) checkMemberExpr(n *parser.MemberExpr) Type {
	objType := c.checkNode(n.Object)
	switch t := objType.(type) {
	case *EnumType:
		// Enum.Variant — look up directly in the enum's own variants to avoid scope collisions
		// when two enums share the same variant name (e.g. two enums with Ok/Err).
		if fieldTypes, ok := t.Variants[n.Property]; ok {
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
		c.errorf(n.Pos(), "enum %s não tem variante %s", t.Name, n.Property)
		return Unknown
	case *ClassType:
		if variants, ok := t.Methods[n.Property]; ok && len(variants) > 0 {
			if !t.MethodPub[n.Property] && c.currentClass != t {
				c.errorf(n.Pos(), "método '%s' de '%s' é privado", n.Property, t.Name)
			}
			return variants[0]
		}
		if ft, ok := t.Fields[n.Property]; ok {
			if !t.FieldPub[n.Property] && c.currentClass != t {
				c.errorf(n.Pos(), "campo '%s' de '%s' é privado", n.Property, t.Name)
			}
			return ft
		}
		c.errorf(n.Pos(), "class %s has no member %s", t.Name, n.Property)
		return Unknown
	case *InterfaceType:
		if ft, ok := t.Methods[n.Property]; ok {
			return ft
		}
		c.errorf(n.Pos(), "interface %s has no method %s", t.Name, n.Property)
		return Unknown
	case *RecordType:
		if ft, ok := t.Fields[n.Property]; ok {
			return ft
		}
		c.errorf(n.Pos(), "record %s has no field %s", t.Name, n.Property)
		return Unknown
	case *TupleType:
		idx, err := strconv.Atoi(n.Property)
		if err != nil {
			c.errorf(n.Pos(), "invalid tuple index: %s", n.Property)
			return Unknown
		}
		if idx < 0 || idx >= len(t.Elements) {
			c.errorf(n.Pos(), "tuple index out of bounds: %d (arity %d)", idx, len(t.Elements))
			return Unknown
		}
		return t.Elements[idx]
	}
	// Unknown object type — allow, return Unknown
	return Unknown
}

func (c *Checker) checkSelfExpr(n *parser.SelfExpr) Type {
	if c.currentClass != nil {
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

	oldRet := currentContext.returnType
	currentContext.returnType = expectedRet
	defer func() { currentContext.returnType = oldRet }()

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
				if _, isFunc := sym.Type.(*FuncType); !isFunc {
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
	if currentContext.returnType != nil && !c.isAssignable(currentContext.returnType, got) {
		c.errorf(n.Pos(), "incompatible return: expected %s, got %s", currentContext.returnType, got)
	}
	return got
}

func (c *Checker) checkCallExpr(n *parser.CallExpr) Type {
	var ft *FuncType
	var calleeType Type

	// M5: overloaded method resolution — must happen before normal callee checking
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
	} else {
		param = c.checkNode(n.Index)
	}
	return &SpecializedType{Base: base, Params: []Type{param}}
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
		if (left == IntType || left == FloatType) && left.String() == right.String() {
			return BoolType
		}
		c.errorf(n.Pos(), "order comparison not supported for %s and %s", left, right)
		return BoolType
	case "&&", "||":
		if left != BoolType || right != BoolType {
			c.errorf(n.Pos(), "logical operators require Bool, got %s and %s", left, right)
		}
		return BoolType
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
			if ct, ok2 := st.Base.(*ClassType); ok2 && ct.Name == "List" && len(st.Params) > 0 {
				bindingType = st.Params[0]
			} else {
				c.errorf(n.Iterable.Pos(), "for-in: tipo '%s' não é iterável", iterType)
			}
		} else {
			c.errorf(n.Iterable.Pos(), "for-in: tipo '%s' não é iterável", iterType)
		}
	}

	c.scope.Define(n.Binding, bindingType, false)
	c.checkBlock(n.Body)
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
		got := c.checkNode(f.Value)
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
		case "Unit":
			return UnitType
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
