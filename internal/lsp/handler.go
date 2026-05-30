package lsp

import (
	"fmt"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"soyuz/internal/checker"
	"soyuz/internal/diag"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// Handler holds the engine and implements all LSP methods.
type Handler struct {
	protocol.Handler
	engine        *Engine
	workspaceRoot string // filesystem path, set during initialize
}

func NewHandler() *Handler {
	h := &Handler{}
	h.engine = NewEngine(func(uri string, result *AnalysisResult) {
		h.publishDiagnostics(uri, result)
	})
	h.registerHandlers()
	return h
}

// NotifyFunc is kept as a field so we can call textDocument/publishDiagnostics.
var serverNotify glsp.NotifyFunc

func (h *Handler) registerHandlers() {
	h.Initialize = h.handleInitialize
	h.Initialized = h.handleInitialized
	h.Shutdown = h.handleShutdown
	h.TextDocumentDidOpen = h.handleDidOpen
	h.TextDocumentDidChange = h.handleDidChange
	h.TextDocumentDidClose = h.handleDidClose
	h.TextDocumentHover = h.handleHover
	h.TextDocumentDefinition = h.handleDefinition
	h.TextDocumentReferences = h.handleReferences
	h.TextDocumentCompletion = h.handleCompletion
	h.TextDocumentPrepareRename = h.handlePrepareRename
	h.TextDocumentRename = h.handleRename
	h.TextDocumentCodeLens = h.handleCodeLens
	h.TextDocumentFormatting = h.handleFormatting
}

// ─── General ─────────────────────────────────────────────────────────────────

func (h *Handler) handleInitialize(ctx *glsp.Context, params *protocol.InitializeParams) (any, error) {
	serverNotify = ctx.Notify

	// Resolve workspace root from whichever field the client provides.
	if params.RootURI != nil && *params.RootURI != "" {
		h.workspaceRoot = uriToPath(*params.RootURI)
	} else if len(params.WorkspaceFolders) > 0 {
		h.workspaceRoot = uriToPath(params.WorkspaceFolders[0].URI)
	}

	syncKind := protocol.TextDocumentSyncKindFull
	// InitializeResult is returned as `any` so we can embed the LSP 3.17
	// inlayHintProvider field that glsp's typed struct does not expose.
	return map[string]any{
		"capabilities": map[string]any{
			"textDocumentSync": map[string]any{
				"openClose": true,
				"change":    syncKind,
			},
			"hoverProvider":      true,
			"definitionProvider": true,
			"referencesProvider": true,
			"completionProvider": map[string]any{
				"triggerCharacters": []string{"."},
			},
			"renameProvider": map[string]any{
				"prepareProvider": true,
			},
			"codeLensProvider":             map[string]any{},
			"documentFormattingProvider":   true,
			"inlayHintProvider":            map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "soyuz-lsp",
			"version": "0.1.0",
		},
	}, nil
}

func (h *Handler) handleInitialized(ctx *glsp.Context, params *protocol.InitializedParams) error {
	if h.workspaceRoot != "" {
		// Set root immediately so files opened before IndexWorkspace completes
		// (e.g. didOpen arriving right after initialized) resolve imports correctly.
		h.engine.SetWorkspaceRoot(h.workspaceRoot)
		go h.engine.IndexWorkspace(h.workspaceRoot)
	}
	return nil
}

func (h *Handler) handleShutdown(ctx *glsp.Context) error {
	return nil
}

// ─── Text Document Sync ───────────────────────────────────────────────────────

func (h *Handler) handleDidOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	serverNotify = ctx.Notify
	h.engine.Open(params.TextDocument.URI, params.TextDocument.Text)
	return nil
}

func (h *Handler) handleDidChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	serverNotify = ctx.Notify
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// We use full sync, so the last change carries the full text.
	last := params.ContentChanges[len(params.ContentChanges)-1]
	switch c := last.(type) {
	case protocol.TextDocumentContentChangeEventWhole:
		h.engine.Update(params.TextDocument.URI, c.Text)
	case *protocol.TextDocumentContentChangeEventWhole:
		h.engine.Update(params.TextDocument.URI, c.Text)
	}
	return nil
}

func (h *Handler) handleDidClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	h.engine.Close(params.TextDocument.URI)
	// Clear diagnostics on close.
	if serverNotify != nil {
		serverNotify(string(protocol.ServerTextDocumentPublishDiagnostics),
			protocol.PublishDiagnosticsParams{URI: params.TextDocument.URI, Diagnostics: []protocol.Diagnostic{}})
	}
	return nil
}

func (h *Handler) publishDiagnostics(uri string, result *AnalysisResult) {
	if serverNotify == nil {
		return
	}
	file := filepathBase(uriToPath(uri))
	parseDiags := diag.FromParseErrors(file, result.ParseErrors)
	typeDiags := diag.FromTypeErrors(result.Check.Errors)
	warnDiags := diag.FromTypeWarnings(result.Check.Warnings)
	allDiags := diag.Merge(parseDiags, typeDiags, warnDiags)

	serverNotify(string(protocol.ServerTextDocumentPublishDiagnostics),
		protocol.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: toDiagnostics(allDiags),
		})
}

func filepathBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func toDiagnostics(errors []diag.Diagnostic) []protocol.Diagnostic {
	diags := make([]protocol.Diagnostic, len(errors))
	for i, e := range errors {
		start := toLSPPosition(e.Start)
		end := toLSPPosition(e.End)
		if end.Line == start.Line && end.Character <= start.Character {
			end.Character = start.Character + 1
		}
		msg := e.Message
		if e.Code != "" {
			msg = e.Code + ": " + msg
		}
		sev := protocol.DiagnosticSeverityError
		if e.Severity == diag.SeverityWarning {
			sev = protocol.DiagnosticSeverityWarning
		}
		diags[i] = protocol.Diagnostic{
			Range: protocol.Range{
				Start: start,
				End:   end,
			},
			Severity: severityPtr(sev),
			Message:  msg,
			Source:   strPtr("soyuz"),
		}
	}
	return diags
}

// ─── Hover ────────────────────────────────────────────────────────────────────

func (h *Handler) handleHover(ctx *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	pos := fromLSPPosition(params.Position)
	node := findNodeAt(result.AST, pos)
	if node == nil {
		return nil, nil
	}

	t, ok := result.Check.NodeTypes[node]
	if !ok {
		return nil, nil
	}

	content := formatHover(node, t, result)

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content,
		},
	}, nil
}

func formatHover(node parser.Node, t checker.Type, _ *AnalysisResult) string {
	switch n := node.(type) {
	case *parser.TaskExpr:
		return fmt.Sprintf("```soyuz\ntask — %s\n```", t.String())
	case *parser.AsyncPipeExpr:
		return fmt.Sprintf("```soyuz\nasync pipe — %s\n```", t.String())
	case *parser.SelectExpr:
		return "```soyuz\nselect — multiplexação de canais\n```"
	case *parser.Identifier:
		if ft, ok := t.(*checker.FuncType); ok {
			return fmt.Sprintf("```soyuz\nfn %s%s\n```", n.Name, ft.String())
		}
		return fmt.Sprintf("```soyuz\n%s: %s\n```", n.Name, t.String())
	case *parser.MemberExpr:
		return fmt.Sprintf("```soyuz\n.%s: %s\n```", n.Property, t.String())
	case *parser.FuncDecl:
		if ft, ok := t.(*checker.FuncType); ok {
			return fmt.Sprintf("```soyuz\nfn %s%s\n```", n.Name, ft.String())
		}
	case *parser.VarDecl:
		return fmt.Sprintf("```soyuz\n%s %s: %s\n```", n.Kind, n.Name, t.String())
	case *parser.ExternDecl:
		if ft, ok := t.(*checker.FuncType); ok {
			return fmt.Sprintf("```soyuz\nextern fn %s%s\n```", n.Name, ft.String())
		}
	case *parser.ImportDecl:
		if len(n.Names) > 0 {
			parts := make([]string, len(n.Names))
			for i, nm := range n.Names {
				parts[i] = nm.Name
			}
			return fmt.Sprintf("```soyuz\n{ %s } from %s\n```", strings.Join(parts, ", "), formatImportPath(n))
		}
		return fmt.Sprintf("```soyuz\n\"%s\"\n```", n.Path)
	}
	return fmt.Sprintf("```soyuz\n%s\n```", t.String())
}

// ─── Go-to-definition ─────────────────────────────────────────────────────────

func (h *Handler) handleDefinition(ctx *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	pos := fromLSPPosition(params.Position)
	node := findNodeAt(result.AST, pos)
	id, ok := node.(*parser.Identifier)
	if !ok {
		return nil, nil
	}

	// Functions: navigate to first variant (current file).
	if variants, ok := result.Check.FuncVariants[id.Name]; ok && len(variants) > 0 {
		return toLSPLocation(params.TextDocument.URI, variants[0].Pos()), nil
	}

	// Records, enums, classes, interfaces: scan top-level AST (current file).
	for _, decl := range result.AST.Body {
		if name, ok := topLevelName(decl); ok && name == id.Name {
			return toLSPLocation(params.TextDocument.URI, decl.Pos()), nil
		}
	}

	// Fall back to workspace index for pub symbols from other files.
	if syms := h.engine.index.Lookup(id.Name); len(syms) > 0 {
		return toLSPLocation(syms[0].URI, syms[0].Pos), nil
	}

	return nil, nil
}

func topLevelName(node parser.Node) (string, bool) {
	switch n := node.(type) {
	case *parser.FuncDecl:
		return n.Name, true
	case *parser.RecordDecl:
		return n.Name, true
	case *parser.EnumDecl:
		return n.Name, true
	case *parser.ClassDecl:
		return n.Name, true
	case *parser.InterfaceDecl:
		return n.Name, true
	case *parser.VarDecl:
		return n.Name, true
	case *parser.ExternDecl:
		return n.Name, true
	}
	return "", false
}

// ─── Find References ──────────────────────────────────────────────────────────

func (h *Handler) handleReferences(ctx *glsp.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	pos := fromLSPPosition(params.Position)
	node := findNodeAt(result.AST, pos)
	id, ok := node.(*parser.Identifier)
	if !ok {
		return nil, nil
	}

	var locs []protocol.Location

	// Search current file.
	walkAST(result.AST, func(n parser.Node) {
		if ref, ok := n.(*parser.Identifier); ok && ref.Name == id.Name {
			locs = append(locs, toLSPLocation(params.TextDocument.URI, ref.Pos()))
		}
	})

	// Search all other indexed workspace files.
	currentURI := params.TextDocument.URI
	for uri, ar := range h.engine.GetAll() {
		if uri == currentURI {
			continue
		}
		// Identifier usages.
		walkAST(ar.AST, func(n parser.Node) {
			if ref, ok := n.(*parser.Identifier); ok && ref.Name == id.Name {
				locs = append(locs, toLSPLocation(uri, ref.Pos()))
			}
		})
		// Declaration sites (FuncDecl / VarDecl names are strings, not Identifiers).
		for _, decl := range ar.AST.Body {
			switch n := decl.(type) {
			case *parser.FuncDecl:
				if n.Name == id.Name {
					locs = append(locs, toLSPLocation(uri, n.NamePos))
				}
			case *parser.VarDecl:
				if n.Name == id.Name {
					locs = append(locs, toLSPLocation(uri, n.NamePos))
				}
			}
		}
	}

	return locs, nil
}

// ─── Rename ───────────────────────────────────────────────────────────────────

// handlePrepareRename validates that the cursor is on a renameable identifier
// and returns the current name as placeholder so the editor can pre-fill it.
func (h *Handler) handlePrepareRename(ctx *glsp.Context, params *protocol.PrepareRenameParams) (any, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	pos := fromLSPPosition(params.Position)
	name, namePos, ok := renameTargetAt(result.AST, pos)
	if !ok {
		return nil, nil
	}

	return protocol.RangeWithPlaceholder{
		Range:       identRange(namePos, name),
		Placeholder: name,
	}, nil
}

// handleRename finds all occurrences of the identifier under the cursor and
// replaces them with the new name in a single WorkspaceEdit.
func (h *Handler) handleRename(ctx *glsp.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	pos := fromLSPPosition(params.Position)
	targetName, _, ok := renameTargetAt(result.AST, pos)
	if !ok {
		return nil, nil
	}

	edits := collectRenameEdits(result.AST, targetName, params.NewName)
	if len(edits) == 0 {
		return nil, nil
	}

	uri := params.TextDocument.URI
	return &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentUri][]protocol.TextEdit{uri: edits},
	}, nil
}

// renameTargetAt resolves what identifier the cursor is pointing to.
// Returns (name, namePos, true) for identifiers and declaration names,
// or ("", {}, false) when the cursor is not on a renameable symbol.
func renameTargetAt(prog *parser.Program, pos lexer.Position) (string, lexer.Position, bool) {
	// First check direct identifiers (call sites, variable uses).
	node := findNodeAt(prog, pos)
	if id, ok := node.(*parser.Identifier); ok {
		return id.Name, id.Pos(), true
	}

	// Also check declaration names (fn, val, var, const).
	// FuncDecl.pos points to `fn`; the name token follows immediately.
	for _, decl := range prog.Body {
		switch n := decl.(type) {
		case *parser.FuncDecl:
			if n.NamePos.Line == pos.Line && containsCol(n.NamePos, n.Name, pos.Column) {
				return n.Name, n.NamePos, true
			}
		case *parser.VarDecl:
			if n.NamePos.Line == pos.Line && containsCol(n.NamePos, n.Name, pos.Column) {
				return n.Name, n.NamePos, true
			}
		}
	}
	return "", lexer.Position{}, false
}

// containsCol reports whether the column is within [namePos.Column, namePos.Column+len(name)).
func containsCol(namePos lexer.Position, name string, col int) bool {
	return col >= namePos.Column && col < namePos.Column+len(name)
}

// collectRenameEdits walks the AST and emits a TextEdit for every occurrence
// of targetName (both usage sites and declaration names).
func collectRenameEdits(prog *parser.Program, targetName, newName string) []protocol.TextEdit {
	var edits []protocol.TextEdit

	// Declaration sites (fn, val/var/const).
	for _, decl := range prog.Body {
		switch n := decl.(type) {
		case *parser.FuncDecl:
			if n.Name == targetName {
				edits = append(edits, protocol.TextEdit{
					Range:   identRange(n.NamePos, n.Name),
					NewText: newName,
				})
			}
		case *parser.VarDecl:
			if n.Name == targetName {
				edits = append(edits, protocol.TextEdit{
					Range:   identRange(n.NamePos, n.Name),
					NewText: newName,
				})
			}
		}
	}

	// Usage sites: all *parser.Identifier nodes in the entire tree.
	walkAST(prog, func(n parser.Node) {
		if ref, ok := n.(*parser.Identifier); ok && ref.Name == targetName {
			edits = append(edits, protocol.TextEdit{
				Range:   identRange(ref.Pos(), ref.Name),
				NewText: newName,
			})
		}
	})

	return edits
}

// identRange builds a Range that spans exactly the identifier token.
func identRange(pos lexer.Position, name string) protocol.Range {
	start := toLSPPosition(pos)
	return protocol.Range{
		Start: start,
		End: protocol.Position{
			Line:      start.Line,
			Character: start.Character + protocol.UInteger(len(name)),
		},
	}
}

// ─── Completion ───────────────────────────────────────────────────────────────

var soyuzKeywords = []string{
	"val", "var", "fn", "extern", "return", "pub", "extend",
	"record", "class", "interface", "enum",
	"if", "else", "when", "match", "for", "while", "loop", "break", "continue", "in",
	"import", "from", "self",
	"true", "false", "None",
	"Ok", "Err", "Some",
	"task", "select",
}

func (h *Handler) handleCompletion(ctx *glsp.Context, params *protocol.CompletionParams) (any, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return keywordCompletions(), nil
	}

	text := h.engine.GetText(params.TextDocument.URI)
	line := getLine(text, int(params.Position.Line))
	prefix := line
	if int(params.Position.Character) <= len(line) {
		prefix = line[:params.Position.Character]
	}

	// Member completion: text before cursor ends with "."
	if strings.HasSuffix(strings.TrimSpace(prefix), ".") {
		pos := fromLSPPosition(params.Position)
		pos.Column -= 2 // step back past the dot
		node := findNodeAt(result.AST, pos)
		if node != nil {
			if t, ok := result.Check.NodeTypes[node]; ok {
				items := memberCompletions(t)
				if items != nil {
					return items, nil
				}
			}
		}
	}

	return keywordCompletions(), nil
}

func keywordCompletions() []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, len(soyuzKeywords))
	for i, kw := range soyuzKeywords {
		kw := kw
		kind := protocol.CompletionItemKindKeyword
		items[i] = protocol.CompletionItem{
			Label: kw,
			Kind:  &kind,
		}
	}
	return items
}

func memberCompletions(t checker.Type) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	switch tt := t.(type) {
	case *checker.RecordType:
		for name, ft := range tt.Fields {
			items = append(items, fieldItem(name, ft))
		}
	case *checker.ClassType:
		for name, ft := range tt.Fields {
			items = append(items, fieldItem(name, ft))
		}
		for name, variants := range tt.Methods {
			if len(variants) > 0 {
				items = append(items, methodItem(name, variants[0]))
			}
		}
	case *checker.SpecializedType:
		return memberCompletions(tt.Base)
	}
	return items
}

func fieldItem(name string, t checker.Type) protocol.CompletionItem {
	kind := protocol.CompletionItemKindField
	detail := t.String()
	return protocol.CompletionItem{
		Label:  name,
		Kind:   &kind,
		Detail: &detail,
	}
}

func methodItem(name string, ft *checker.FuncType) protocol.CompletionItem {
	kind := protocol.CompletionItemKindMethod
	detail := ft.String()
	return protocol.CompletionItem{
		Label:  name,
		Kind:   &kind,
		Detail: &detail,
	}
}

// ─── Code lens ────────────────────────────────────────────────────────────────

func (h *Handler) handleCodeLens(_ *glsp.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	// Pre-compute identifier counts once for the whole file.
	counts := make(map[string]int)
	walkAST(result.AST, func(n parser.Node) {
		if id, ok := n.(*parser.Identifier); ok {
			counts[id.Name]++
		}
	})

	var lenses []protocol.CodeLens
	for _, decl := range result.AST.Body {
		fd, ok := decl.(*parser.FuncDecl)
		if !ok {
			continue
		}
		title := fmt.Sprintf("%d uso(s)", counts[fd.Name])
		lenses = append(lenses, protocol.CodeLens{
			Range: identRange(fd.NamePos, fd.Name),
			Command: &protocol.Command{
				Title:   title,
				Command: "soyuz.references",
			},
		})
	}
	return lenses, nil
}

// ─── Formatting ───────────────────────────────────────────────────────────────

func (h *Handler) handleFormatting(_ *glsp.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	result := h.engine.Get(params.TextDocument.URI)
	if result == nil {
		return nil, nil
	}

	// O formatter reconstrói o fonte a partir do AST e perderia comentários,
	// pois o lexer os descarta antes de chegarem ao AST. Pula se houver qualquer comentário.
	if strings.Contains(result.Text, "//") {
		return nil, nil
	}

	formatted := formatProgram(result.AST, result.Text)
	if formatted == result.Text {
		return nil, nil
	}

	// Single edit that replaces the entire document.
	lineCount := protocol.UInteger(strings.Count(result.Text, "\n") + 1)
	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: lineCount, Character: 0},
		},
		NewText: formatted,
	}}, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func getLine(text string, line int) string {
	start := 0
	for i := 0; i < line; i++ {
		idx := strings.IndexByte(text[start:], '\n')
		if idx < 0 {
			return ""
		}
		start += idx + 1
	}
	end := strings.IndexByte(text[start:], '\n')
	if end < 0 {
		return text[start:]
	}
	return text[start : start+end]
}

func boolPtr(b bool) *bool       { return &b }
func strPtr(s string) *string    { return &s }
func severityPtr(s protocol.DiagnosticSeverity) *protocol.DiagnosticSeverity { return &s }
