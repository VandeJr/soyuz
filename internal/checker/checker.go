package checker

import (
	"fmt"
	"path/filepath"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

type Checker struct {
	errors               []TypeError
	warnings             []TypeWarning
	scope                *Scope
	nodeTypes            map[parser.Node]Type
	specializations      map[parser.Node]*FuncType
	funcVariants         map[string][]*parser.FuncDecl
	captures             map[*parser.ArrowFunc][]string
	currentClass         *ClassType
	synthCallArgs        map[*parser.CallExpr][]parser.Node
	inferredBodies       map[*parser.FuncDecl]bool
	arrowFuncHints       map[*parser.ArrowFunc][]Type
	curriedCalls         map[*parser.CallExpr]*parser.ArrowFunc
	pendingClassMethods  []pendingClassMethod
	pendingExtendMethods []pendingExtendMethod
	// M8.1: cross-file pub enforcement
	nodeFile     map[parser.Node]string // nil = single-file mode (pub not enforced)
	symbolOrigin map[string]string      // global name → defining file
	symbolPub    map[string]bool        // global name → pub status
	fileNamedImports     map[string]map[string]bool // file → imported symbol names
	fileModuleNamespaces map[string]map[string]bool // file → imported module namespace names
	preludeFiles         map[string]bool            // auto-imported prelude source files
	currentFile          string
	inTopLevel           bool
	context              context
	typeExtensions       map[string]map[string][]*FuncType // extended/builtin methods per type name
	extendSelfTypes      map[string]Type                 // self type for extension methods
	currentExtend        string                            // type name when checking extend method body
	loopDepth            int                               // nested loop/while/for for break/continue
	loopBreakTypes       []Type                            // accumulated break-value type per `loop` expr
}

type CheckResult struct {
	Errors              []TypeError
	Warnings            []TypeWarning
	NodeTypes           map[parser.Node]Type
	Specializations     map[parser.Node]*FuncType
	FuncVariants        map[string][]*parser.FuncDecl
	Captures            map[*parser.ArrowFunc][]string
	SynthCallArgs       map[*parser.CallExpr][]parser.Node
	CurriedCalls        map[*parser.CallExpr]*parser.ArrowFunc
	TypeExtensions      map[string]map[string][]*FuncType
}

type context struct {
	returnType   Type
	expectedType Type // hint for ambiguous literals (e.g. [] in generic class fields)
}

func New() *Checker {
	scope := NewScope(nil)

	errorIface := &InterfaceType{
		Name: "Error",
		Methods: map[string]*FuncType{
			"message": {Params: []Type{}, Return: StringType},
			"code":    {Params: []Type{}, Return: IntType},
		},
	}

	optionEnum := &EnumType{
		Name:     "Option",
		Generics: []string{"T"},
		Variants: map[string][]Type{
			"Some": {&TypeParameter{Name: "T"}},
			"None": {},
		},
	}
	scope.Define("Option", optionEnum, true)
	scope.Define("Some", optionEnum, true)
	scope.Define("None", optionEnum, true)

	resultEnum := &EnumType{
		Name:     "Result",
		Generics: []string{"T"},
		Variants: map[string][]Type{
			"Ok":  {&TypeParameter{Name: "T"}},
			"Err": {errorIface},
		},
	}
	scope.Define("Result", resultEnum, true)
	scope.Define("Ok", resultEnum, true)
	scope.Define("Err", resultEnum, true)
	scope.Define("Error", errorIface, true)

	listType := &ClassType{
		Name:     "List",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"size":    {{Params: []Type{}, Return: IntType}},
			"get":     {{Params: []Type{IntType}, Return: &TypeParameter{Name: "T"}}},
			"append":  {{Params: []Type{&TypeParameter{Name: "T"}}, Return: UnitType}},
			"add":     {{Params: []Type{&TypeParameter{Name: "T"}}, Return: UnitType}},
			"map":     {{Params: []Type{Unknown}, Return: Unknown}}, // handled specially in checkCallExpr
			"filter":  {{Params: []Type{Unknown}, Return: Unknown}},
			"reduce":  {{Params: []Type{Unknown, Unknown}, Return: Unknown}},
			"join":    {{Params: []Type{StringType}, Return: StringType}},
			"isEmpty": {{Params: []Type{}, Return: BoolType}},
			"set":     {{Params: []Type{IntType, &TypeParameter{Name: "T"}}, Return: UnitType}},
			"remove":  {{Params: []Type{IntType}, Return: &TypeParameter{Name: "T"}}},
			"pop":     {{Params: []Type{}, Return: &TypeParameter{Name: "T"}}},
			"prepend": {{Params: []Type{&TypeParameter{Name: "T"}}, Return: UnitType}},
			"clear":   {{Params: []Type{}, Return: UnitType}},
			"copy":    {{Params: []Type{}, Return: Unknown}},    // handled specially in checkCallExpr
			"concat":  {{Params: []Type{Unknown}, Return: Unknown}}, // handled specially in checkCallExpr
		},
	}
	scope.Define("List", listType, true)

	mapType := &ClassType{
		Name:     "Map",
		Generics: []string{"K", "V"},
		Methods: map[string][]*FuncType{
			"size":   {{Params: []Type{}, Return: IntType}},
			"get":    {{Params: []Type{&TypeParameter{Name: "K"}}, Return: &TypeParameter{Name: "V"}}},
			"set":    {{Params: []Type{&TypeParameter{Name: "K"}, &TypeParameter{Name: "V"}}, Return: UnitType}},
			"keys":   {{Params: []Type{}, Return: Unknown}}, // handled specially in checkCallExpr
			"values": {{Params: []Type{}, Return: Unknown}},
		},
	}
	scope.Define("Map", mapType, true)

	iteratorType := &ClassType{
		Name:     "Iterator",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"next": {
				{
					Params: []Type{},
					Return: &SpecializedType{
						Base: optionEnum,
						Params: []Type{&TypeParameter{Name: "T"}},
					},
				},
			},
			"isEmpty": {{Params: []Type{}, Return: BoolType}},
		},
	}
	scope.Define("Iterator", iteratorType, true)

	// iter() returns Iterator[T] for List and Iterator[K] for Map keys.
	listType.Methods["iter"] = []*FuncType{{Params: []Type{}, Return: Unknown}}
	mapType.Methods["iter"] = []*FuncType{{Params: []Type{}, Return: Unknown}}

	taskType := &ClassType{
		Name:     "Task",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"await":      {{Params: []Type{}, Return: &TypeParameter{Name: "T"}}},
			"detach":     {{Params: []Type{}, Return: UnitType}},
			"cancel":     {{Params: []Type{}, Return: UnitType}},
			"all":        {{Params: []Type{Unknown}, Return: Unknown}},
			"allSettled": {{Params: []Type{Unknown}, Return: Unknown}},
			"fan":        {{Params: []Type{Unknown}, Return: Unknown}},
			"pipe":       {{Params: []Type{Unknown}, Return: Unknown}},
			"gather":     {{Params: []Type{Unknown}, Return: Unknown}},
			"tap":        {{Params: []Type{Unknown}, Return: Unknown}},
			"always":     {{Params: []Type{Unknown}, Return: Unknown}},
		},
		MethodPub: map[string]bool{"await": true, "detach": true, "cancel": true, "all": true, "allSettled": true, "fan": true, "pipe": true, "gather": true, "tap": true, "always": true},
	}
	scope.Define("Task", taskType, true)

	taskHandleType := &ClassType{
		Name: "TaskHandle",
		Methods: map[string][]*FuncType{
			"current":   {{Params: []Type{}, Return: Unknown}}, // intercepted in checkCallExpr → Option[TaskHandle]
			"cancelled": {{Params: []Type{}, Return: BoolType}},
			"progress":  {{Params: []Type{FloatType}, Return: UnitType}},
		},
		MethodPub: map[string]bool{"current": true, "cancelled": true, "progress": true},
	}
	scope.Define("TaskHandle", taskHandleType, true)

	// ── M-08: stdlib/sync ────────────────────────────────────────────────────

	mutexGuardType := &ClassType{
		Name:     "MutexGuard",
		Generics: []string{"T"},
		Fields:   map[string]Type{"value": &TypeParameter{Name: "T"}},
		FieldPub: map[string]bool{"value": true},
	}
	scope.Define("MutexGuard", mutexGuardType, true)

	readGuardType := &ClassType{
		Name:     "ReadGuard",
		Generics: []string{"T"},
		Fields:   map[string]Type{"value": &TypeParameter{Name: "T"}},
		FieldPub: map[string]bool{"value": true},
	}
	scope.Define("ReadGuard", readGuardType, true)

	writeGuardType := &ClassType{
		Name:     "WriteGuard",
		Generics: []string{"T"},
		Fields:   map[string]Type{"value": &TypeParameter{Name: "T"}},
		FieldPub: map[string]bool{"value": true},
	}
	scope.Define("WriteGuard", writeGuardType, true)

	mutexType := &ClassType{
		Name:     "Mutex",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"new":  {{Params: []Type{Unknown}, Return: Unknown}}, // intercepted: Mutex.new(val) → Mutex[T]
			"lock": {{Params: []Type{}, Return: &SpecializedType{Base: mutexGuardType, Params: []Type{&TypeParameter{Name: "T"}}}}},
		},
		MethodPub: map[string]bool{"new": true, "lock": true},
	}
	scope.Define("Mutex", mutexType, true)

	rwlockType := &ClassType{
		Name:     "RwLock",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"new":   {{Params: []Type{Unknown}, Return: Unknown}}, // intercepted: RwLock.new(val) → RwLock[T]
			"read":  {{Params: []Type{}, Return: &SpecializedType{Base: readGuardType, Params: []Type{&TypeParameter{Name: "T"}}}}},
			"write": {{Params: []Type{}, Return: &SpecializedType{Base: writeGuardType, Params: []Type{&TypeParameter{Name: "T"}}}}},
		},
		MethodPub: map[string]bool{"new": true, "read": true, "write": true},
	}
	scope.Define("RwLock", rwlockType, true)

	atomicType := &ClassType{
		Name:     "Atomic",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"new":            {{Params: []Type{Unknown}, Return: Unknown}},                                         // intercepted
			"load":           {{Params: []Type{}, Return: &TypeParameter{Name: "T"}}},                              // intercepted
			"store":          {{Params: []Type{&TypeParameter{Name: "T"}}, Return: UnitType}},
			"add":            {{Params: []Type{&TypeParameter{Name: "T"}}, Return: &TypeParameter{Name: "T"}}},     // intercepted
			"compareAndSwap": {{Params: []Type{&TypeParameter{Name: "T"}, &TypeParameter{Name: "T"}}, Return: BoolType}},
		},
		MethodPub: map[string]bool{"new": true, "load": true, "store": true, "add": true, "compareAndSwap": true},
	}
	scope.Define("Atomic", atomicType, true)

	// ── M-14: stdlib/arc — Arc[T] com EBR ───────────────────────────────────

	arcType := &ClassType{
		Name:     "Arc",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"new":      {{Params: []Type{Unknown}, Return: Unknown}}, // intercepted → Arc[T]
			"clone":    {{Params: []Type{}, Return: Unknown}},         // intercepted → Arc[T]
			"get":      {{Params: []Type{}, Return: &TypeParameter{Name: "T"}}},
			"refcount": {{Params: []Type{}, Return: IntType}},
		},
		MethodPub: map[string]bool{"new": true, "clone": true, "get": true, "refcount": true},
	}
	scope.Define("Arc", arcType, true)

	// ── M-09: stdlib/channel ─────────────────────────────────────────────────

	channelType := &ClassType{
		Name:     "Channel",
		Generics: []string{"T"},
		Methods: map[string][]*FuncType{
			"new":      {{Params: []Type{IntType}, Return: Unknown}}, // intercepted → Channel[T]
			"send":     {{Params: []Type{&TypeParameter{Name: "T"}}, Return: UnitType}},
			"recv":     {{Params: []Type{}, Return: Unknown}},         // intercepted → Option[T]
			"tryRecv":  {{Params: []Type{}, Return: Unknown}},         // intercepted → Option[T]
			"close":    {{Params: []Type{}, Return: UnitType}},
			"isClosed": {{Params: []Type{}, Return: BoolType}},
		},
		MethodPub: map[string]bool{
			"new": true, "send": true, "recv": true,
			"tryRecv": true, "close": true, "isClosed": true,
		},
	}
	scope.Define("Channel", channelType, true)

	printFunc := &FuncType{Params: []Type{Unknown}, Return: UnitType}
	scope.Define("print", printFunc, true)

	typeExtensions := builtinTypeExtensions()

	return &Checker{
		scope:                scope,
		nodeTypes:            make(map[parser.Node]Type),
		specializations:      make(map[parser.Node]*FuncType),
		funcVariants:         make(map[string][]*parser.FuncDecl),
		captures:             make(map[*parser.ArrowFunc][]string),
		synthCallArgs:        make(map[*parser.CallExpr][]parser.Node),
		inferredBodies:       make(map[*parser.FuncDecl]bool),
		arrowFuncHints:       make(map[*parser.ArrowFunc][]Type),
		curriedCalls:         make(map[*parser.CallExpr]*parser.ArrowFunc),
		symbolOrigin:         make(map[string]string),
		symbolPub:            make(map[string]bool),
		fileNamedImports:     make(map[string]map[string]bool),
		fileModuleNamespaces: make(map[string]map[string]bool),
		typeExtensions:       typeExtensions,
		extendSelfTypes:      builtinExtendSelfTypes(),
	}
}

func builtinExtendSelfTypes() map[string]Type {
	return map[string]Type{
		"String": StringType,
		"Int":    IntType,
		"Float":  FloatType,
		"Bool":   BoolType,
		"Char":   CharType,
	}
}

func builtinTypeExtensions() map[string]map[string][]*FuncType {
	resultEnum := &EnumType{
		Name:     "Result",
		Generics: []string{"T"},
		Variants: map[string][]Type{
			"Ok":  {IntType},
			"Err": {&InterfaceType{Name: "Error", Methods: map[string]*FuncType{}}},
		},
	}
	resultInt := &SpecializedType{
		Base:   resultEnum,
		Params: []Type{IntType},
	}
	resultFloat := &SpecializedType{
		Base:   resultEnum,
		Params: []Type{FloatType},
	}
	optionBase := &EnumType{
		Name:     "Option",
		Generics: []string{"T"},
		Variants: map[string][]Type{
			"Some": {&TypeParameter{Name: "T"}},
			"None": {},
		},
	}
	optionInt := &SpecializedType{Base: optionBase, Params: []Type{IntType}}
	optionChar := &SpecializedType{Base: optionBase, Params: []Type{CharType}}
	listBase := &ClassType{Name: "List", Generics: []string{"T"}}
	listOfString := &SpecializedType{Base: listBase, Params: []Type{StringType}}
	return map[string]map[string][]*FuncType{
		"String": {
			"len":          {{Params: []Type{}, Return: IntType}},
			"isEmpty":      {{Params: []Type{}, Return: BoolType}},
			"toUpperCase":  {{Params: []Type{}, Return: StringType}},
			"toUpper":      {{Params: []Type{}, Return: StringType}},
			"toLowerCase":  {{Params: []Type{}, Return: StringType}},
			"toLower":      {{Params: []Type{}, Return: StringType}},
			"trim":         {{Params: []Type{}, Return: StringType}},
			"contains":     {{Params: []Type{StringType}, Return: BoolType}},
			"startsWith":   {{Params: []Type{StringType}, Return: BoolType}},
			"endsWith":     {{Params: []Type{StringType}, Return: BoolType}},
			"indexOf":      {{Params: []Type{StringType}, Return: optionInt}},
			"lastIndexOf":  {{Params: []Type{StringType}, Return: optionInt}},
			"replace":      {{Params: []Type{StringType, StringType}, Return: StringType}},
			"split":        {{Params: []Type{StringType}, Return: listOfString}},
			"substring":    {{Params: []Type{IntType, IntType}, Return: StringType}},
			"byteAt":       {{Params: []Type{IntType}, Return: optionInt}},
			"unicodeAt":    {{Params: []Type{IntType}, Return: optionChar}},
			"toInt":        {{Params: []Type{}, Return: resultInt}},
			"toFloat":      {{Params: []Type{}, Return: resultFloat}},
		},
		"Int": {
			"toString": {{Params: []Type{}, Return: StringType}},
			"toFloat":  {{Params: []Type{}, Return: FloatType}},
			"abs":      {{Params: []Type{}, Return: IntType}},
		},
		"Float": {
			"toString": {{Params: []Type{}, Return: StringType}},
			"toInt":    {{Params: []Type{}, Return: IntType}},
		},
		"Bool": {
			"toString": {{Params: []Type{}, Return: StringType}},
		},
		"Char": {
			"toString": {{Params: []Type{}, Return: StringType}},
		},
	}
}

// SetNodeFiles enables M8.1 cross-file pub enforcement.
// nodeFile maps each top-level AST node to the source file it was parsed from.
func (c *Checker) SetNodeFiles(nf map[parser.Node]string) {
	c.nodeFile = nf
}

// SetPreludeFiles marks files that are auto-imported via @soyuz/prelude.
func (c *Checker) SetPreludeFiles(files []string) {
	c.preludeFiles = make(map[string]bool, len(files))
	for _, f := range files {
		c.preludeFiles[f] = true
	}
}

func (c *Checker) Check(prog *parser.Program) *CheckResult {
	c.collectFileImports(prog)

	// Pass 1: group function variants, separate type decls from value nodes.
	var typeNodes []parser.Node
	var valueNodes []parser.Node
	for _, node := range prog.Body {
		if fd, ok := node.(*parser.FuncDecl); ok {
			c.funcVariants[fd.Name] = append(c.funcVariants[fd.Name], fd)
			continue
		}
		switch node.(type) {
		case *parser.RecordDecl, *parser.EnumDecl, *parser.InterfaceDecl, *parser.ClassDecl, *parser.ExternDecl, *parser.ExtendDecl:
			typeNodes = append(typeNodes, node)
		default:
			valueNodes = append(valueNodes, node)
		}
	}

	// Pass 2: register type declarations (records, enums, interfaces, classes).
	for _, node := range typeNodes {
		if c.nodeFile != nil {
			c.currentFile = c.nodeFile[node]
		}
		c.checkNode(node)
	}

	// Pass 3: register all function signatures so calls can resolve them.
	for name, variants := range c.funcVariants {
		if c.nodeFile != nil && len(variants) > 0 {
			c.currentFile = c.nodeFile[variants[0]]
		}
		c.registerFuncVariants(name, variants)
	}

	// Pass 3.5: create module namespaces for module imports.
	// Must run after Pass 3 so all function signatures are registered in scope.
	for _, node := range prog.Body {
		if imp, ok := node.(*parser.ImportDecl); ok && imp.IsModuleImport() {
			if c.nodeFile != nil {
				c.currentFile = c.nodeFile[node]
			}
			c.registerModuleNamespace(prog, imp)
		}
	}

	// Pass 3.7: check class method bodies that were deferred during Pass 2.
	// Running after Pass 3 ensures top-level functions are in scope so methods
	// can reference them (e.g. passing isDigit/isLetter as HOF arguments).
	prevClass := c.currentClass
	for _, pm := range c.pendingClassMethods {
		if c.nodeFile != nil {
			c.currentFile = pm.file
		}
		c.currentClass = pm.ct
		c.checkClassMethodBody(pm)
	}
	c.currentClass = prevClass

	prevExtend := c.currentExtend
	for _, pm := range c.pendingExtendMethods {
		if c.nodeFile != nil {
			c.currentFile = pm.file
		}
		c.currentExtend = pm.typeName
		c.checkExtendMethodBody(pm)
	}
	c.currentExtend = prevExtend

	// Pass 4: check value nodes (var decls, expressions) that may call functions.
	for _, node := range valueNodes {
		if c.nodeFile != nil {
			c.currentFile = c.nodeFile[node]
		}
		c.inTopLevel = true
		c.checkNode(node)
		c.inTopLevel = false
	}

	// Pass 5: type-check all function bodies.
	for name, variants := range c.funcVariants {
		if c.nodeFile != nil && len(variants) > 0 {
			c.currentFile = c.nodeFile[variants[0]]
		}
		c.checkFuncVariantsBody(name, variants)
	}

	return &CheckResult{
		Errors:          c.errors,
		Warnings:        c.warnings,
		NodeTypes:       c.nodeTypes,
		Specializations: c.specializations,
		FuncVariants:    c.funcVariants,
		Captures:        c.captures,
		SynthCallArgs:   c.synthCallArgs,
		CurriedCalls:    c.curriedCalls,
		TypeExtensions:  c.typeExtensions,
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
	case *parser.ExternDecl:
		return c.checkExternDecl(n)
	case *parser.ExtendDecl:
		return c.checkExtendDecl(n)
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
	case *parser.CharLiteral:
		return CharType
	case *parser.Identifier:
		return c.checkIdentifier(n)
	case *parser.NamedArg:
		return c.checkNode(n.Value)
	case *parser.OkExpr:
		valType := c.checkNode(n.Value)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Result"})
		return &SpecializedType{Base: base, Params: []Type{valType}}
	case *parser.ErrExpr:
		c.checkNode(n.Value)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Result"})
		return &SpecializedType{Base: base, Params: []Type{c.contextInnerType("Result")}}
	case *parser.SomeExpr:
		valType := c.checkNode(n.Value)
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
		return &SpecializedType{Base: base, Params: []Type{valType}}
	case *parser.NoneLiteral:
		base := c.resolveTypeExpr(&parser.NamedType{Name: "Option"})
		return &SpecializedType{Base: base, Params: []Type{c.contextInnerType("Option")}}
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
	case *parser.SpecializedExpr:
		return c.checkSpecializedExpr(n)
	case *parser.MatchExpr:
		return c.checkMatchExpr(n)
	case *parser.InterpolatedString:
		return c.checkInterpolatedString(n)
	case *parser.ArrowFunc:
		return c.checkArrowFunc(n)
	case *parser.PipeExpr:
		return c.checkPipeExpr(n)
	case *parser.PipeQuestExpr:
		return c.checkPipeQuestExpr(n)
	case *parser.AsyncPipeExpr:
		return c.checkAsyncPipeExpr(n)
	case *parser.ElvisExpr:
		return c.checkElvisExpr(n)
	case *parser.TupleExpr:
		return c.checkTupleExpr(n)
	case *parser.ListExpr:
		return c.checkListExpr(n)
	case *parser.MapExpr:
		return c.checkMapExpr(n)
	case *parser.MemberExpr:
		return c.checkMemberExpr(n)
	case *parser.SafeNavExpr:
		return c.checkSafeNavExpr(n)
	case *parser.SelfExpr:
		return c.checkSelfExpr(n)
	case *parser.TaskExpr:
		return c.checkTaskExpr(n)
	case *parser.SelectExpr:
		return c.checkSelectExpr(n)
	case *parser.ForStmt:
		return c.checkForStmt(n)
	case *parser.WhileStmt:
		return c.checkWhileStmt(n)
	case *parser.LoopStmt:
		return c.checkLoopStmt(n)
	case *parser.BreakStmt:
		return c.checkBreakStmt(n)
	case *parser.ContinueStmt:
		return c.checkContinueStmt(n)
	case *parser.IfStmt:
		c.checkNode(n.Condition)
		c.checkBlock(n.Consequent)
		if n.Alternate != nil {
			c.checkNode(n.Alternate)
		}
		return UnitType
	case *parser.ExprStmt:
		t := c.checkNode(n.Expr)
		// W0300: Task[T] must-use — emite warning se a task não for consumida.
		// Exceção: val _ = task ... não é ExprStmt, então não chega aqui.
		if c.isTaskType(t) {
			c.warnf(n.Expr.Pos(), "W0300", "Task não consumida — use .await() ou .detach()")
		}
		return t
	case *parser.BlockStmt:
		return c.checkBlock(n)
	default:
		return Unknown
	}
}

func (c *Checker) errorf(pos lexer.Position, format string, args ...any) {
	c.errors = append(c.errors, TypeError{
		Pos:     pos,
		End:     lexer.Position{Line: pos.Line, Column: pos.Column + 4},
		File:    c.currentFile,
		Code:    "E0200",
		Message: fmt.Sprintf(format, args...),
	})
}

func (c *Checker) warnf(pos lexer.Position, code string, format string, args ...any) {
	c.warnings = append(c.warnings, TypeWarning{
		Pos:     pos,
		End:     lexer.Position{Line: pos.Line, Column: pos.Column + 4},
		File:    c.currentFile,
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	})
}

// isTaskType retorna true se o tipo é Task[T] (SpecializedType com base ClassType{Name:"Task"}).
func (c *Checker) isTaskType(t Type) bool {
	if st, ok := t.(*SpecializedType); ok {
		if ct, ok2 := st.Base.(*ClassType); ok2 {
			return ct.Name == "Task"
		}
	}
	return false
}

// registerGlobalSymbol records the origin and pub status of a top-level symbol for M8.1.
func (c *Checker) registerGlobalSymbol(name string, node parser.Node, pub bool) {
	if c.nodeFile == nil {
		return
	}
	origin := c.nodeFile[node]
	if prev, exists := c.symbolOrigin[name]; exists {
		if c.preludeFiles[prev] && !c.preludeFiles[origin] {
			return
		}
	}
	c.symbolOrigin[name] = origin
	c.symbolPub[name] = pub
}

// collectFileImports builds per-file named import and module namespace sets.
func (c *Checker) collectFileImports(prog *parser.Program) {
	if c.nodeFile == nil {
		return
	}
	for _, node := range prog.Body {
		imp, ok := node.(*parser.ImportDecl)
		if !ok {
			continue
		}
		file := c.nodeFile[node]
		if file == "" {
			continue
		}
		if c.fileNamedImports[file] == nil {
			c.fileNamedImports[file] = make(map[string]bool)
		}
		if c.fileModuleNamespaces[file] == nil {
			c.fileModuleNamespaces[file] = make(map[string]bool)
		}
		if imp.IsModuleImport() {
			c.fileModuleNamespaces[file][imp.Namespace] = true
		}
		for _, n := range imp.Names {
			name := n.Name
			if n.Alias != "" {
				name = n.Alias
			}
			c.fileNamedImports[file][name] = true
		}
	}
}

// checkGlobalAccess emits an error if name is a global symbol from a different file
// than currentFile and is not marked pub or not imported.
func (c *Checker) checkGlobalAccess(name string, pos lexer.Position) {
	if c.nodeFile == nil {
		return
	}
	origin, isGlobal := c.symbolOrigin[name]
	if !isGlobal || origin == "" || origin == c.currentFile {
		return
	}
	if !c.symbolPub[name] {
		c.errorf(pos, "símbolo '%s' não é público (defina com 'pub' em %s)", name, filepath.Base(origin))
		return
	}
	if c.preludeFiles[origin] {
		return
	}
	if c.fileNamedImports[c.currentFile][name] {
		return
	}
	c.errorf(pos, "símbolo '%s' não foi importado em %s", name, filepath.Base(c.currentFile))
}

// checkModuleNamespaceAccess verifies the current file imported the module namespace.
func (c *Checker) checkModuleNamespaceAccess(ns string, pos lexer.Position) {
	if c.nodeFile == nil {
		return
	}
	if c.fileModuleNamespaces[c.currentFile][ns] {
		return
	}
	c.errorf(pos, "namespace '%s' não foi importado em %s", ns, filepath.Base(c.currentFile))
}

// contextInnerType returns the type parameter T when c.context.returnType is EnumName[T]
// with a concrete T (not Unknown). Used to propagate the expected Ok/Some type into Err/None.
func (c *Checker) contextInnerType(enumName string) Type {
	st, ok := c.context.returnType.(*SpecializedType)
	if !ok || len(st.Params) == 0 {
		return Unknown
	}
	et, ok := st.Base.(*EnumType)
	if !ok || et.Name != enumName {
		return Unknown
	}
	if st.Params[0] == Unknown {
		return Unknown
	}
	return st.Params[0]
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
