package checker

import (
	"fmt"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

type Checker struct {
	errors          []TypeError
	scope           *Scope
	nodeTypes       map[parser.Node]Type
	specializations map[parser.Node]*FuncType
	funcVariants    map[string][]*parser.FuncDecl
	captures        map[*parser.ArrowFunc][]string
	currentClass    *ClassType
}

type CheckResult struct {
	Errors              []TypeError
	NodeTypes           map[parser.Node]Type
	Specializations     map[parser.Node]*FuncType
	FuncVariants        map[string][]*parser.FuncDecl
	Captures            map[*parser.ArrowFunc][]string
	ImplicitWeakFields  map[string]map[string]bool // typeName → fieldName → true
}

type context struct {
	returnType Type
}

var currentContext context

func New() *Checker {
	scope := NewScope(nil)

	errorIface := &InterfaceType{
		Name: "Error",
		Methods: map[string]*FuncType{
			"message": {Params: []Type{}, Return: StringType},
			"code":    {Params: []Type{}, Return: IntType},
		},
	}
	scope.Define("Error", errorIface, true)

	optionEnum := &EnumType{
		Name: "Option",
		Variants: map[string][]Type{
			"Some": {Unknown},
			"None": {},
		},
	}
	scope.Define("Option", optionEnum, true)
	scope.Define("Some", optionEnum, true)
	scope.Define("None", optionEnum, true)

	resultEnum := &EnumType{
		Name: "Result",
		Variants: map[string][]Type{
			"Ok":  {Unknown},
			"Err": {errorIface},
		},
	}
	scope.Define("Result", resultEnum, true)
	scope.Define("Ok", resultEnum, true)
	scope.Define("Err", resultEnum, true)

	listType := &ClassType{
		Name:     "List",
		Generics: []string{"T"},
		Methods: map[string]*FuncType{
			"size":   {Params: []Type{}, Return: IntType},
			"get":    {Params: []Type{IntType}, Return: &TypeParameter{Name: "T"}},
			"append": {Params: []Type{&TypeParameter{Name: "T"}}, Return: UnitType},
		},
	}
	scope.Define("List", listType, true)

	mapType := &ClassType{
		Name:     "Map",
		Generics: []string{"K", "V"},
		Methods: map[string]*FuncType{
			"size": {Params: []Type{}, Return: IntType},
			"get":  {Params: []Type{&TypeParameter{Name: "K"}}, Return: &TypeParameter{Name: "V"}},
			"set":  {Params: []Type{&TypeParameter{Name: "K"}, &TypeParameter{Name: "V"}}, Return: UnitType},
		},
	}
	scope.Define("Map", mapType, true)

	printFunc := &FuncType{Params: []Type{Unknown}, Return: UnitType}
	scope.Define("print", printFunc, true)

	return &Checker{
		scope:           scope,
		nodeTypes:       make(map[parser.Node]Type),
		specializations: make(map[parser.Node]*FuncType),
		funcVariants:    make(map[string][]*parser.FuncDecl),
		captures:        make(map[*parser.ArrowFunc][]string),
	}
}

func (c *Checker) Check(prog *parser.Program) *CheckResult {
	// Pass 1: group function variants, separate type decls from value nodes.
	var typeNodes []parser.Node
	var valueNodes []parser.Node
	for _, node := range prog.Body {
		if fd, ok := node.(*parser.FuncDecl); ok {
			c.funcVariants[fd.Name] = append(c.funcVariants[fd.Name], fd)
			continue
		}
		switch node.(type) {
		case *parser.RecordDecl, *parser.EnumDecl, *parser.InterfaceDecl, *parser.ClassDecl:
			typeNodes = append(typeNodes, node)
		default:
			valueNodes = append(valueNodes, node)
		}
	}

	// Pass 2: register type declarations (records, enums, interfaces, classes).
	for _, node := range typeNodes {
		c.checkNode(node)
	}

	// After Pass 2: detect implicit weak fields from type cycles.
	implicitWeak := DetectImplicitWeakFields(prog)

	// Pass 3: register all function signatures so calls can resolve them.
	for name, variants := range c.funcVariants {
		c.registerFuncVariants(name, variants)
	}

	// Pass 4: check value nodes (var decls, expressions) that may call functions.
	for _, node := range valueNodes {
		c.checkNode(node)
	}

	// Pass 5: type-check all function bodies.
	for name, variants := range c.funcVariants {
		c.checkFuncVariantsBody(name, variants)
	}

	return &CheckResult{
		Errors:             c.errors,
		NodeTypes:          c.nodeTypes,
		Specializations:    c.specializations,
		FuncVariants:       c.funcVariants,
		Captures:           c.captures,
		ImplicitWeakFields: implicitWeak,
	}
}

func (c *Checker) checkNode(node parser.Node) Type {
	t := c.doCheckNode(node)
	if node != nil {
		c.nodeTypes[node] = t
	}
	return t
}

func (c *Checker) doCheckNode(node parser.Node) Type {
	switch n := node.(type) {
	case *parser.VarDecl:
		return c.checkVarDecl(n)
	case *parser.FuncDecl:
		return c.checkFuncDecl(n)
	case *parser.RecordDecl:
		return c.checkRecordDecl(n)
	case *parser.EnumDecl:
		return c.checkEnumDecl(n)
	case *parser.InterfaceDecl:
		return c.checkInterfaceDecl(n)
	case *parser.ClassDecl:
		return c.checkClassDecl(n)
	case *parser.ReturnStmt:
		return c.checkReturnStmt(n)
	case *parser.IntLiteral:
		return IntType
	case *parser.FloatLiteral:
		return FloatType
	case *parser.BoolLiteral:
		return BoolType
	case *parser.StringLiteral:
		return StringType
	case *parser.Identifier:
		return c.checkIdentifier(n)
	case *parser.OkExpr:
		valType := c.checkNode(n.Value)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Result"})
		return &SpecializedType{Base: base, Params: []Type{valType}}
	case *parser.ErrExpr:
		c.checkNode(n.Value)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Result"})
		return &SpecializedType{Base: base, Params: []Type{Unknown}}
	case *parser.SomeExpr:
		valType := c.checkNode(n.Value)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
		return &SpecializedType{Base: base, Params: []Type{valType}}
	case *parser.RecordLiteral:
		return c.checkRecordLiteral(n)
	case *parser.BinaryExpr:
		return c.checkBinaryExpr(n)
	case *parser.UnaryExpr:
		return c.checkUnaryExpr(n)
	case *parser.AssignExpr:
		return c.checkAssignExpr(n)
	case *parser.CallExpr:
		return c.checkCallExpr(n)
	case *parser.IndexExpr:
		return c.checkSpecialization(n)
	case *parser.MatchExpr:
		return c.checkMatchExpr(n)
	case *parser.InterpolatedString:
		return c.checkInterpolatedString(n)
	case *parser.ArrowFunc:
		return c.checkArrowFunc(n)
	case *parser.PipeExpr:
		return c.checkPipeExpr(n)
	case *parser.TupleExpr:
		return c.checkTupleExpr(n)
	case *parser.ListExpr:
		return c.checkListExpr(n)
	case *parser.MapExpr:
		return c.checkMapExpr(n)
	case *parser.MemberExpr:
		return c.checkMemberExpr(n)
	case *parser.SelfExpr:
		return c.checkSelfExpr(n)
	case *parser.ExprStmt:
		return c.checkNode(n.Expr)
	case *parser.BlockStmt:
		return c.checkBlock(n)
	default:
		return Unknown
	}
}

func (c *Checker) errorf(pos lexer.Position, format string, args ...any) {
	c.errors = append(c.errors, TypeError{
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	})
}

func (c *Checker) isHeapType(t Type) bool {
	switch st := t.(type) {
	case *RecordType, *ClassType, *InterfaceType, *TupleType, *EnumType, *FuncType:
		return true
	case *SpecializedType:
		return c.isHeapType(st.Base)
	case *BasicType:
		return st.Name == "String"
	}
	return false
}
