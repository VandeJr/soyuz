package lsp

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// ─── URI helpers ──────────────────────────────────────────────────────────────

func uriToPath(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

func pathToURI(path string) string {
	return "file://" + path
}

// ─── SymbolKind ───────────────────────────────────────────────────────────────

type SymbolKind int

const (
	SymbolFunction SymbolKind = iota
	SymbolType                // record, enum, class, interface
	SymbolVariable            // pub val / var / const
)

// ─── IndexedSymbol ────────────────────────────────────────────────────────────

type IndexedSymbol struct {
	URI  string
	Pos  lexer.Position
	Kind SymbolKind
	Name string
	Type checker.Type // may be nil for type declarations
}

// ─── SymbolIndex ──────────────────────────────────────────────────────────────

// SymbolIndex tracks pub symbols across all workspace files.
// It is updated incrementally: each analyzed file replaces its prior entries.
type SymbolIndex struct {
	mu     sync.RWMutex
	byName map[string][]IndexedSymbol
}

func NewSymbolIndex() *SymbolIndex {
	return &SymbolIndex{byName: make(map[string][]IndexedSymbol)}
}

// Update replaces all symbols contributed by uri with the pub declarations
// found in result. Call-safe from multiple goroutines.
func (idx *SymbolIndex) Update(uri string, result *AnalysisResult) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove stale entries for this URI.
	for name, syms := range idx.byName {
		var kept []IndexedSymbol
		for _, s := range syms {
			if s.URI != uri {
				kept = append(kept, s)
			}
		}
		if len(kept) == 0 {
			delete(idx.byName, name)
		} else {
			idx.byName[name] = kept
		}
	}

	// Add pub symbols found in this file.
	for _, node := range result.AST.Body {
		var name string
		var pos lexer.Position
		var kind SymbolKind
		var isPub bool

		switch n := node.(type) {
		case *parser.FuncDecl:
			name, pos, kind, isPub = n.Name, n.NamePos, SymbolFunction, n.Pub
		case *parser.VarDecl:
			if n.Name != "" {
				name, pos, kind, isPub = n.Name, n.NamePos, SymbolVariable, n.Pub
			}
		case *parser.RecordDecl:
			name, pos, kind, isPub = n.Name, n.Pos(), SymbolType, n.Pub
		case *parser.EnumDecl:
			name, pos, kind, isPub = n.Name, n.Pos(), SymbolType, n.Pub
		case *parser.ClassDecl:
			name, pos, kind, isPub = n.Name, n.Pos(), SymbolType, n.Pub
		case *parser.InterfaceDecl:
			name, pos, kind, isPub = n.Name, n.Pos(), SymbolType, n.Pub
		}

		if !isPub || name == "" {
			continue
		}

		var t checker.Type
		if nt, ok := result.Check.NodeTypes[node]; ok {
			t = nt
		}
		idx.byName[name] = append(idx.byName[name], IndexedSymbol{
			URI: uri, Pos: pos, Kind: kind, Name: name, Type: t,
		})
	}
}

// Remove drops all symbols contributed by uri (e.g. on file deletion).
func (idx *SymbolIndex) Remove(uri string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	for name, syms := range idx.byName {
		var kept []IndexedSymbol
		for _, s := range syms {
			if s.URI != uri {
				kept = append(kept, s)
			}
		}
		if len(kept) == 0 {
			delete(idx.byName, name)
		} else {
			idx.byName[name] = kept
		}
	}
}

// Lookup returns all indexed symbols with the given name.
func (idx *SymbolIndex) Lookup(name string) []IndexedSymbol {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return append([]IndexedSymbol(nil), idx.byName[name]...)
}

// ─── Workspace scanner ────────────────────────────────────────────────────────

// IndexWorkspace scans root for .soyuz files and analyzes each one that is not
// already open in the editor. Safe to call in a goroutine.
func (e *Engine) IndexWorkspace(root string) {
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Skip hidden directories (e.g. .git).
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".soyuz" {
			return nil
		}

		uri := pathToURI(path)

		// Skip files already open in the editor (editor version is authoritative).
		e.mu.RLock()
		alreadyOpen := e.open[uri]
		e.mu.RUnlock()
		if alreadyOpen {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		e.mu.Lock()
		e.texts[uri] = string(data)
		e.mu.Unlock()

		e.analyze(uri)
		return nil
	})
}
