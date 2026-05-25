package lsp

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
	soyuzstdlib "soyuz/std"
)

type AnalysisResult struct {
	AST   *parser.Program
	Check *checker.CheckResult
	Text  string
}

// Engine re-analyzes documents on change with a 300 ms debounce and maintains
// a cross-file SymbolIndex for workspace-wide navigation.
type Engine struct {
	mu            sync.RWMutex
	results       map[string]*AnalysisResult
	texts         map[string]string
	timers        map[string]*time.Timer
	open          map[string]bool // files currently open in the editor
	notify        func(uri string, result *AnalysisResult)
	index         *SymbolIndex
	stdlibDir     string // temp dir with extracted stdlib; empty = no stdlib
	workspaceRoot string // filesystem path of the workspace root; used as resolver root
}

func NewEngine(notify func(uri string, result *AnalysisResult)) *Engine {
	e := &Engine{
		results: make(map[string]*AnalysisResult),
		texts:   make(map[string]string),
		timers:  make(map[string]*time.Timer),
		open:    make(map[string]bool),
		notify:  notify,
		index:   NewSymbolIndex(),
	}
	e.stdlibDir = extractStdlibToTemp()
	return e
}

func extractStdlibToTemp() string {
	dir, err := os.MkdirTemp("", "soyuz-lsp-stdlib-")
	if err != nil {
		return ""
	}
	for name, data := range soyuzstdlib.Files {
		dest := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			continue
		}
		os.WriteFile(dest, data, 0644)
	}
	return dir
}

// Open records the editor-provided text and triggers immediate analysis.
func (e *Engine) Open(uri, text string) {
	e.mu.Lock()
	e.texts[uri] = text
	e.open[uri] = true
	e.mu.Unlock()
	e.analyze(uri)
}

// Update stores new text and schedules a debounced re-analysis (300 ms).
func (e *Engine) Update(uri, text string) {
	e.mu.Lock()
	e.texts[uri] = text
	if t, ok := e.timers[uri]; ok {
		t.Stop()
	}
	e.timers[uri] = time.AfterFunc(300*time.Millisecond, func() {
		e.analyze(uri)
	})
	e.mu.Unlock()
}

// Close removes the file from the "open" set so diagnostics are no longer
// published. The analysis result and text are kept for cross-file navigation
// until IndexWorkspace re-reads the file from disk on the next startup.
func (e *Engine) Close(uri string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.open, uri)
	if t, ok := e.timers[uri]; ok {
		t.Stop()
		delete(e.timers, uri)
	}
}

// Get returns the latest analysis result for uri, or nil if not yet analyzed.
func (e *Engine) Get(uri string) *AnalysisResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.results[uri]
}

// GetText returns the current source text for uri.
func (e *Engine) GetText(uri string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.texts[uri]
}

// SetWorkspaceRoot sets the workspace root used by the import resolver.
// Call this before spawning IndexWorkspace so files opened before the
// background scan completes still resolve imports from the correct root.
func (e *Engine) SetWorkspaceRoot(root string) {
	e.mu.Lock()
	e.workspaceRoot = root
	e.mu.Unlock()
}

// GetAll returns a shallow copy of all analyzed results, keyed by URI.
func (e *Engine) GetAll() map[string]*AnalysisResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := make(map[string]*AnalysisResult, len(e.results))
	for k, v := range e.results {
		m[k] = v
	}
	return m
}

func (e *Engine) analyze(uri string) {
	defer func() { recover() }() //nolint — impede que panics em goroutines de timer derrubem o servidor

	e.mu.RLock()
	text, ok := e.texts[uri]
	e.mu.RUnlock()
	if !ok {
		return
	}

	tokens := lexer.Tokenize(text)
	p := parser.New(tokens)
	prog := p.Parse()

	var result *checker.CheckResult
	if e.stdlibDir != "" {
		result = e.analyzeWithStdlib(uriToPath(uri), text, prog)
	}
	if result == nil {
		result = checker.New().Check(prog)
	}

	ar := &AnalysisResult{
		AST:   prog,
		Check: result,
		Text:  text,
	}

	e.mu.Lock()
	e.results[uri] = ar
	isOpen := e.open[uri]
	e.mu.Unlock()

	// Update the workspace symbol index for this file.
	e.index.Update(uri, ar)

	// Publish diagnostics only for files the editor has open.
	if isOpen && e.notify != nil {
		e.notify(uri, ar)
	}
}

// analyzeWithStdlib runs stdlib-aware type checking for the file at filePath.
// For each import found (stdlib or local), dependencies are resolved and merged recursively.
func (e *Engine) analyzeWithStdlib(filePath, text string, prog *parser.Program) *checker.CheckResult {
	resolver := module.NewResolverWithStdlib(filePath, e.stdlibDir)
	// Use workspace root so local imports resolve correctly for any file in the project,
	// not just the entry file.
	e.mu.RLock()
	if e.workspaceRoot != "" {
		resolver.Root = e.workspaceRoot
	}
	e.mu.RUnlock()

	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	visited := make(map[string]bool)
	visited[filePath] = true

	var loadRecursive func(file string, p *parser.Program)
	loadRecursive = func(file string, p *parser.Program) {
		for _, node := range p.Body {
			imp, isImp := node.(*parser.ImportDecl)
			if !isImp {
				if nodeFile[node] == "" {
					nodeFile[node] = file
					allNodes = append(allNodes, node)
				}
				continue
			}

			// Resolve import
			resolved, err := resolver.Resolve(imp)
			if err != nil {
				// Unresolved: keep for checker error
				if nodeFile[node] == "" {
					nodeFile[node] = file
					allNodes = append(allNodes, node)
				}
				continue
			}
			imp.ResolvedFiles = resolved

			for _, f := range resolved {
				if visited[f] {
					continue
				}
				visited[f] = true
				data, rerr := os.ReadFile(f)
				if rerr != nil {
					continue
				}
				subProg := parser.New(lexer.Tokenize(string(data))).Parse()
				loadRecursive(f, subProg)
			}

			// Add the import node itself for namespace/wildcard handling in checker
			if nodeFile[node] == "" {
				nodeFile[node] = file
				allNodes = append(allNodes, node)
			}
		}
	}

	loadRecursive(filePath, prog)

	merged := &parser.Program{Body: allNodes}
	c := checker.New()
	c.SetNodeFiles(nodeFile)
	return c.Check(merged)
}
