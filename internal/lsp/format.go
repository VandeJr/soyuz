package lsp

import (
	"fmt"
	"strings"

	"soyuz/internal/parser"
)

// formatProgram pretty-prints prog according to Soyuz style:
//   - 4-space indentation
//   - spaces around binary operators and arrows
//   - blank line between top-level declarations
//   - no trailing whitespace
//
// src is the original source text (used to recover literals unchanged).
func formatProgram(prog *parser.Program, src string) string {
	p := &printer{src: src}
	p.printProgram(prog)
	return strings.TrimRight(p.buf.String(), "\n") + "\n"
}

// ─── Printer ─────────────────────────────────────────────────────────────────

type printer struct {
	buf   strings.Builder
	depth int
	src   string
}

func (p *printer) indent() string { return strings.Repeat("    ", p.depth) }

func (p *printer) write(s string)  { p.buf.WriteString(s) }
func (p *printer) writeln(s string) { p.buf.WriteString(p.indent() + s + "\n") }
func (p *printer) nl()              { p.buf.WriteByte('\n') }

// ─── Program ─────────────────────────────────────────────────────────────────

func (p *printer) printProgram(prog *parser.Program) {
	i := 0
	for i < len(prog.Body) {
		if _, ok := prog.Body[i].(*parser.ImportDecl); ok {
			p.printImportBlock(prog, i)
			for i < len(prog.Body) {
				if _, ok := prog.Body[i].(*parser.ImportDecl); !ok {
					break
				}
				i++
			}
			if i < len(prog.Body) {
				p.nl()
			}
			continue
		}
		if i > 0 {
			p.nl()
		}
		p.printTopLevel(prog.Body[i])
		i++
	}
}

func (p *printer) printImportBlock(prog *parser.Program, start int) {
	p.writeln("import (")
	end := start
	for end < len(prog.Body) {
		if _, ok := prog.Body[end].(*parser.ImportDecl); !ok {
			break
		}
		end++
	}
	for j := start; j < end; j++ {
		p.write("    ")
		p.printImportSpec(prog.Body[j].(*parser.ImportDecl))
		if j < end-1 {
			p.writeln("")
		}
	}
	p.writeln("")
	p.writeln(")")
}

func (p *printer) printImportSpec(n *parser.ImportDecl) {
	path := formatImportPath(n)
	if len(n.Names) > 0 {
		parts := make([]string, len(n.Names))
		for i, nm := range n.Names {
			parts[i] = nm.Name
		}
		p.write("{ " + strings.Join(parts, ", ") + " } from " + path)
		return
	}
	p.write(path)
}

func formatImportPath(n *parser.ImportDecl) string {
	switch n.PathKind {
	case parser.ImportPathStdlib, parser.ImportPathProjectRoot, parser.ImportPathPackageAlias, parser.ImportPathRelative:
		return n.Path
	default:
		if strings.HasPrefix(n.Path, "@") {
			return n.Path
		}
		return "\"" + n.Path + "\""
	}
}

func (p *printer) printTopLevel(node parser.Node) {
	switch n := node.(type) {
	case *parser.ImportDecl:
		p.printImportBlockSingle(n)
	case *parser.ExternDecl:
		p.printExternDecl(n)
	case *parser.FuncDecl:
		p.printFuncDecl(n)
	case *parser.VarDecl:
		p.printVarDecl(n)
	case *parser.RecordDecl:
		p.printRecordDecl(n)
	case *parser.EnumDecl:
		p.printEnumDecl(n)
	case *parser.ClassDecl:
		p.printClassDecl(n)
	case *parser.InterfaceDecl:
		p.printInterfaceDecl(n)
	case *parser.ExtendDecl:
		p.printExtendDecl(n)
	case *parser.ExprStmt:
		p.writeln(p.expr(n.Expr))
	default:
		p.writeln(p.expr(node))
	}
}

func (p *printer) printImportBlockSingle(n *parser.ImportDecl) {
	p.writeln("import (")
	p.write("    ")
	p.printImportSpec(n)
	p.writeln("")
	p.writeln(")")
}

func (p *printer) printImportDecl(n *parser.ImportDecl) {
	p.printImportBlockSingle(n)
}

func (p *printer) printExternDecl(n *parser.ExternDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	params := p.funcParams(n.Params)
	ret := ""
	if n.ReturnType != nil {
		ret = " -> " + p.typeExpr(n.ReturnType)
	}
	p.writeln(fmt.Sprintf("%sextern fn %s(%s)%s", prefix, n.Name, params, ret))
}

// ─── Declarations ─────────────────────────────────────────────────────────────

func (p *printer) printFuncDecl(n *parser.FuncDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	generics := p.genericParams(n.Generics)
	params := p.funcParams(n.Params)
	whenClause := ""
	if n.WhenGuard != nil {
		whenClause = " when " + p.expr(n.WhenGuard)
	}
	ret := ""
	if n.ReturnType != nil {
		ret = " -> " + p.typeExpr(n.ReturnType)
	}
	sig := fmt.Sprintf("%sfn %s%s(%s)%s%s", prefix, n.Name, generics, params, whenClause, ret)
	if n.IsExprBody {
		p.writeln(sig + " = " + p.expr(n.Body))
	} else {
		p.writeln(sig + " {")
		p.depth++
		p.printBlock(n.Body)
		p.depth--
		p.writeln("}")
	}
}

func (p *printer) printVarDecl(n *parser.VarDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	name := n.Name
	if name == "" && n.Pattern != nil {
		name = p.pattern(n.Pattern)
	}
	typeAnno := ""
	if n.Type != nil {
		typeAnno = ": " + p.typeExpr(n.Type)
	}
	init := ""
	if n.Init != nil {
		init = " = " + p.expr(n.Init)
	}
	p.writeln(fmt.Sprintf("%s%s %s%s%s", prefix, n.Kind, name, typeAnno, init))
}

func (p *printer) printRecordDecl(n *parser.RecordDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	generics := p.genericParamsFull(n.Generics)
	p.writeln(fmt.Sprintf("%srecord %s%s {", prefix, n.Name, generics))
	p.depth++
	for i, f := range n.Fields {
		comma := ","
		if i == len(n.Fields)-1 {
			comma = ""
		}
		p.writeln(fmt.Sprintf("%s: %s%s", f.Name, p.typeExpr(f.Type), comma))
	}
	p.depth--
	p.writeln("}")
}

func (p *printer) printEnumDecl(n *parser.EnumDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	generics := p.genericParamsFull(n.Generics)
	p.writeln(fmt.Sprintf("%senum %s%s {", prefix, n.Name, generics))
	p.depth++
	for i, v := range n.Variants {
		fields := ""
		if len(v.Fields) > 0 {
			parts := make([]string, len(v.Fields))
			for j, f := range v.Fields {
				if f.Name != "" {
					parts[j] = f.Name + ": " + p.typeExpr(f.Type)
				} else {
					parts[j] = p.typeExpr(f.Type)
				}
			}
			fields = "(" + strings.Join(parts, ", ") + ")"
		}
		comma := ","
		if i == len(n.Variants)-1 {
			comma = ""
		}
		p.writeln(v.Name + fields + comma)
	}
	p.depth--
	p.writeln("}")
}

func (p *printer) printClassDecl(n *parser.ClassDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	generics := p.genericParamsFull(n.Generics)
	ifaces := ""
	if len(n.Interfaces) > 0 {
		parts := make([]string, len(n.Interfaces))
		for i, iface := range n.Interfaces {
			parts[i] = p.typeExpr(iface)
		}
		ifaces = " : " + strings.Join(parts, ", ")
	}
	p.writeln(fmt.Sprintf("%sclass %s%s%s {", prefix, n.Name, generics, ifaces))
	p.depth++
	for i, member := range n.Body {
		if i > 0 {
			p.nl()
		}
		switch m := member.(type) {
		case *parser.FuncDecl:
			p.printFuncDecl(m)
		case *parser.VarDecl:
			p.printVarDecl(m)
		}
	}
	p.depth--
	p.writeln("}")
}

func (p *printer) printExtendDecl(n *parser.ExtendDecl) {
	p.writeln("extend " + n.TypeName + " {")
	p.depth++
	for i, m := range n.Methods {
		if i > 0 {
			p.nl()
		}
		p.printFuncDecl(m)
	}
	p.depth--
	p.writeln("}")
}

func (p *printer) printInterfaceDecl(n *parser.InterfaceDecl) {
	prefix := ""
	if n.Pub {
		prefix = "pub "
	}
	generics := p.genericParamsFull(n.Generics)
	p.writeln(fmt.Sprintf("%sinterface %s%s {", prefix, n.Name, generics))
	p.depth++
	for _, m := range n.Methods {
		params := make([]string, len(m.Params))
		for i, param := range m.Params {
			params[i] = p.funcParamStr(param)
		}
		ret := ""
		if m.ReturnType != nil {
			ret = " -> " + p.typeExpr(m.ReturnType)
		}
		prefix := "fn "
		if m.Pub {
			prefix = "pub fn "
		}
		p.writeln(fmt.Sprintf("%s%s(%s)%s", prefix, m.Name, strings.Join(params, ", "), ret))
	}
	p.depth--
	p.writeln("}")
}

// ─── Block & Statements ───────────────────────────────────────────────────────

func (p *printer) printBlock(body parser.Node) {
	block, ok := body.(*parser.BlockStmt)
	if !ok {
		p.writeln(p.expr(body))
		return
	}
	for _, stmt := range block.Statements {
		p.printStmt(stmt)
	}
}

func (p *printer) printStmt(node parser.Node) {
	switch n := node.(type) {
	case *parser.VarDecl:
		p.printVarDecl(n)
	case *parser.ReturnStmt:
		if n.Value != nil {
			p.writeln("return " + p.expr(n.Value))
		} else {
			p.writeln("return")
		}
	case *parser.BreakStmt:
		if n.Value != nil {
			p.writeln("break " + p.expr(n.Value))
		} else {
			p.writeln("break")
		}
	case *parser.ContinueStmt:
		p.writeln("continue")
	case *parser.IfStmt:
		p.printIfStmt(n)
	case *parser.ForStmt:
		p.writeln("for " + n.Binding + " in " + p.expr(n.Iterable) + " {")
		p.depth++
		p.printBlock(n.Body)
		p.depth--
		p.writeln("}")
	case *parser.WhileStmt:
		p.writeln("while " + p.expr(n.Condition) + " {")
		p.depth++
		p.printBlock(n.Body)
		p.depth--
		p.writeln("}")
	case *parser.LoopStmt:
		p.writeln("loop {")
		p.depth++
		p.printBlock(n.Body)
		p.depth--
		p.writeln("}")
	case *parser.ExprStmt:
		p.writeln(p.expr(n.Expr))
	default:
		p.writeln(p.expr(node))
	}
}

func (p *printer) printIfStmt(n *parser.IfStmt) {
	p.writeln("if " + p.expr(n.Condition) + " {")
	p.depth++
	p.printBlock(n.Consequent)
	p.depth--
	switch alt := n.Alternate.(type) {
	case nil:
		p.writeln("}")
	case *parser.IfStmt:
		p.write(p.indent() + "} else ")
		p.depth = 0 // printIfStmt writes its own indent
		// Trick: write inline by hijacking the indent system.
		// Actually easier: just handle it specially.
		altStr := p.captureIfStmt(alt)
		p.buf.WriteString(altStr)
	case *parser.BlockStmt:
		p.writeln("} else {")
		p.depth++
		p.printBlock(alt)
		p.depth--
		p.writeln("}")
	}
}

// captureIfStmt prints an IfStmt and returns the string starting after the indent.
func (p *printer) captureIfStmt(n *parser.IfStmt) string {
	sub := &printer{src: p.src, depth: p.depth}
	sub.printIfStmt(n)
	s := sub.buf.String()
	// Strip leading indent (already written by the caller as "} else ").
	s = strings.TrimLeft(s, " ")
	return s
}

// ─── Expressions ─────────────────────────────────────────────────────────────

func (p *printer) expr(node parser.Node) string {
	if node == nil {
		return ""
	}
	switch n := node.(type) {
	case *parser.Identifier:
		return n.Name
	case *parser.SelfExpr:
		return "self"
	case *parser.IntLiteral:
		return n.Value
	case *parser.FloatLiteral:
		return n.Value
	case *parser.BoolLiteral:
		if n.Value {
			return "true"
		}
		return "false"
	case *parser.CharLiteral:
		return "'" + formatCharRune(n.Value) + "'"
	case *parser.NoneLiteral:
		return "None"
	case *parser.StringLiteral:
		return `"` + n.Value + `"`
	case *parser.InterpolatedString:
		return p.interpString(n)
	case *parser.BinaryExpr:
		return p.binaryExpr(n)
	case *parser.UnaryExpr:
		return n.Operator + p.exprParen(n.Operand, 12)
	case *parser.AssignExpr:
		return p.expr(n.Left) + " = " + p.expr(n.Right)
	case *parser.PipeExpr:
		return p.expr(n.Left) + " |> " + p.expr(n.Right)
	case *parser.PipeQuestExpr:
		return p.expr(n.Left) + " |?> " + p.expr(n.Right)
	case *parser.CallExpr:
		return p.callExpr(n)
	case *parser.MemberExpr:
		return p.expr(n.Object) + "." + n.Property
	case *parser.SafeNavExpr:
		return p.expr(n.Object) + "?." + n.Property
	case *parser.ElvisExpr:
		return p.expr(n.Left) + " ?: " + p.expr(n.Right)
	case *parser.IndexExpr:
		return p.expr(n.Object) + "[" + p.expr(n.Index) + "]"
	case *parser.RangeExpr:
		op := ".."
		if n.Inclusive {
			op = "..="
		}
		return p.expr(n.From) + op + p.expr(n.To)
	case *parser.TupleExpr:
		parts := make([]string, len(n.Elements))
		for i, e := range n.Elements {
			parts[i] = p.expr(e)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *parser.ListExpr:
		parts := make([]string, len(n.Elements))
		for i, e := range n.Elements {
			parts[i] = p.expr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *parser.MapExpr:
		parts := make([]string, len(n.Entries))
		for i, e := range n.Entries {
			parts[i] = p.expr(e.Key) + ": " + p.expr(e.Value)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *parser.OkExpr:
		return "Ok(" + p.expr(n.Value) + ")"
	case *parser.ErrExpr:
		return "Err(" + p.expr(n.Value) + ")"
	case *parser.SomeExpr:
		return "Some(" + p.expr(n.Value) + ")"
	case *parser.RecordLiteral:
		return p.recordLiteral(n)
	case *parser.MatchExpr:
		return p.matchExpr(n)
	case *parser.ArrowFunc:
		return p.arrowFunc(n)
	case *parser.BlockStmt:
		return p.blockExpr(n)
	case *parser.ExprStmt:
		return p.expr(n.Expr)
	}
	return "<?>"
}

func (p *printer) exprParen(node parser.Node, minPrec int) string {
	s := p.expr(node)
	// Add parens for binary/pipe expressions that may have lower precedence.
	switch node.(type) {
	case *parser.BinaryExpr, *parser.PipeExpr, *parser.PipeQuestExpr, *parser.ElvisExpr, *parser.AssignExpr:
		s = "(" + s + ")"
	}
	return s
}

func (p *printer) binaryExpr(n *parser.BinaryExpr) string {
	return p.expr(n.Left) + " " + n.Operator + " " + p.expr(n.Right)
}

func (p *printer) callExpr(n *parser.CallExpr) string {
	args := make([]string, len(n.Args))
	for i, a := range n.Args {
		args[i] = p.expr(a)
	}
	return p.expr(n.Callee) + "(" + strings.Join(args, ", ") + ")"
}

func (p *printer) interpString(n *parser.InterpolatedString) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, part := range n.Parts {
		switch v := part.(type) {
		case *parser.StringLiteral:
			sb.WriteString(v.Value)
		default:
			sb.WriteString("$(")
			sb.WriteString(p.expr(part))
			sb.WriteByte(')')
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func (p *printer) recordLiteral(n *parser.RecordLiteral) string {
	if len(n.Fields) == 0 {
		return n.Name + " {}"
	}
	parts := make([]string, len(n.Fields))
	for i, f := range n.Fields {
		parts[i] = f.Name + ": " + p.expr(f.Value)
	}
	return n.Name + " { " + strings.Join(parts, ", ") + " }"
}

func (p *printer) matchExpr(n *parser.MatchExpr) string {
	var sb strings.Builder
	sb.WriteString("match " + p.expr(n.Subject) + " {\n")
	p.depth++
	for _, arm := range n.Arms {
		guard := ""
		if arm.Guard != nil {
			guard = " if " + p.expr(arm.Guard)
		}
		body := p.expr(arm.Body)
		sb.WriteString(p.indent() + p.pattern(arm.Pattern) + guard + " => " + body + "\n")
	}
	p.depth--
	sb.WriteString(p.indent() + "}")
	return sb.String()
}

func (p *printer) arrowFunc(n *parser.ArrowFunc) string {
	params := p.funcParams(n.Params)
	ret := ""
	if n.ReturnType != nil {
		ret = " -> " + p.typeExpr(n.ReturnType)
	}
	return "fn(" + params + ")" + ret + " => " + p.expr(n.Body)
}

func (p *printer) blockExpr(n *parser.BlockStmt) string {
	if len(n.Statements) == 0 {
		return "{}"
	}
	sub := &printer{src: p.src, depth: p.depth + 1}
	for _, stmt := range n.Statements {
		sub.printStmt(stmt)
	}
	inner := strings.TrimRight(sub.buf.String(), "\n")
	return "{\n" + inner + "\n" + p.indent() + "}"
}

// ─── Type expressions ─────────────────────────────────────────────────────────

func (p *printer) typeExpr(te parser.TypeExpr) string {
	if te == nil {
		return ""
	}
	switch t := te.(type) {
	case *parser.NamedType:
		return t.Name
	case *parser.GenericType:
		params := make([]string, len(t.Params))
		for i, param := range t.Params {
			params[i] = p.typeExpr(param)
		}
		return t.Name + "[" + strings.Join(params, ", ") + "]"
	case *parser.OptionalType:
		return p.typeExpr(t.Inner) + "?"
	case *parser.TupleType:
		parts := make([]string, len(t.Elements))
		for i, e := range t.Elements {
			parts[i] = p.typeExpr(e)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *parser.FuncType:
		params := make([]string, len(t.ParamTypes))
		for i, pt := range t.ParamTypes {
			params[i] = p.typeExpr(pt)
		}
		ret := ""
		if t.ReturnType != nil {
			ret = " -> " + p.typeExpr(t.ReturnType)
		}
		return "(" + strings.Join(params, ", ") + ")" + ret
	}
	return "<?>"
}

// ─── Patterns ────────────────────────────────────────────────────────────────

func (p *printer) pattern(pat parser.Pattern) string {
	if pat == nil {
		return "_"
	}
	switch n := pat.(type) {
	case *parser.WildcardPattern:
		return "_"
	case *parser.BindingPattern:
		return n.Name
	case *parser.LiteralPattern:
		return p.expr(n.Value)
	case *parser.ConstructorPattern:
		if len(n.Args) == 0 {
			return n.Name
		}
		args := make([]string, len(n.Args))
		for i, a := range n.Args {
			args[i] = p.pattern(a)
		}
		return n.Name + "(" + strings.Join(args, ", ") + ")"
	case *parser.RecordPattern:
		fields := make([]string, len(n.Fields))
		for i, f := range n.Fields {
			if f.Pattern != nil {
				fields[i] = f.Name + ": " + p.pattern(f.Pattern)
			} else {
				fields[i] = f.Name
			}
		}
		return n.Name + " { " + strings.Join(fields, ", ") + " }"
	case *parser.TuplePattern:
		parts := make([]string, len(n.Elements))
		for i, e := range n.Elements {
			parts[i] = p.pattern(e)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *parser.RangePattern:
		op := ".."
		if n.Inclusive {
			op = "..="
		}
		return p.expr(n.From) + op + p.expr(n.To)
	}
	return "_"
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (p *printer) genericParams(gs []parser.GenericParam) string {
	if len(gs) == 0 {
		return ""
	}
	return "[" + p.genericParamList(gs) + "]"
}

func (p *printer) genericParamsFull(gs []parser.GenericParam) string {
	if len(gs) == 0 {
		return ""
	}
	return "[" + p.genericParamList(gs) + "]"
}

func (p *printer) genericParamList(gs []parser.GenericParam) string {
	parts := make([]string, len(gs))
	for i, g := range gs {
		s := g.Name
		if len(g.Constraints) > 0 {
			cs := make([]string, len(g.Constraints))
			for j, c := range g.Constraints {
				cs[j] = p.typeExpr(c)
			}
			s += " : " + strings.Join(cs, " + ")
		}
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}

func (p *printer) funcParams(params []parser.FuncParam) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = p.funcParamStr(param)
	}
	return strings.Join(parts, ", ")
}

func formatCharRune(r rune) string {
	switch r {
	case '\n':
		return `\n`
	case '\t':
		return `\t`
	case '\'':
		return `\'`
	case '\\':
		return `\\`
	case 0:
		return `\0`
	default:
		return string(r)
	}
}

func (p *printer) funcParamStr(param parser.FuncParam) string {
	name := p.pattern(param.Pattern)
	if param.Type != nil {
		s := name + ": " + p.typeExpr(param.Type)
		if param.Default != nil {
			s += " = " + p.expr(param.Default)
		}
		return s
	}
	if param.Default != nil {
		return name + " = " + p.expr(param.Default)
	}
	return name
}
