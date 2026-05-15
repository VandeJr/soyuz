package lsp

import (
	"sync"
	"time"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

type AnalysisResult struct {
	AST   *parser.Program
	Check *checker.CheckResult
	Text  string
}

// Engine re-analyzes documents on change with a 300 ms debounce and maintains
// a cross-file SymbolIndex for workspace-wide navigation.
type Engine struct {
	mu      sync.RWMutex
	results map[string]*AnalysisResult
	texts   map[string]string
	timers  map[string]*time.Timer
	open    map[string]bool // files currently open in the editor
	notify  func(uri string, result *AnalysisResult)
	index   *SymbolIndex
}

func NewEngine(notify func(uri string, result *AnalysisResult)) *Engine {
	return &Engine{
		results: make(map[string]*AnalysisResult),
		texts:   make(map[string]string),
		timers:  make(map[string]*time.Timer),
		open:    make(map[string]bool),
		notify:  notify,
		index:   NewSymbolIndex(),
	}
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
	e.mu.RLock()
	text, ok := e.texts[uri]
	e.mu.RUnlock()
	if !ok {
		return
	}

	tokens := lexer.Tokenize(text)
	p := parser.New(tokens)
	prog := p.Parse()
	result := checker.New().Check(prog)

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
