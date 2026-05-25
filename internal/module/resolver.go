package module

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"soyuz/internal/parser"
)

// Resolver mapeia declarações de import Soyuz para caminhos de arquivo .sy.
type Resolver struct {
	Root      string // caminho absoluto do diretório raiz do projeto
	StdlibDir string // diretório onde a stdlib embutida foi extraída; vazio = sem stdlib
}

// NewResolver cria um Resolver com raiz no diretório do arquivo de entrada.
func NewResolver(entryFile string) *Resolver {
	abs, _ := filepath.Abs(entryFile)
	return &Resolver{Root: filepath.Dir(abs)}
}

// NewResolverWithStdlib é como NewResolver mas também conhece o diretório da stdlib.
func NewResolverWithStdlib(entryFile, stdlibDir string) *Resolver {
	r := NewResolver(entryFile)
	r.StdlibDir = stdlibDir
	return r
}

// Resolve retorna os arquivo(s) .sy que satisfazem imp.
//
// Regras de resolução (verificadas em ordem):
//  1. Import stdlib (@soyuz/X): busca em StdlibDir/X.sy ou StdlibDir/X/
//  2. <root>/<seg0>/.../<segN>.sy — módulo de arquivo único
//  3. <root>/<seg0>/.../<segN>/      — diretório: todos os *.sy dentro
func (r *Resolver) Resolve(imp *parser.ImportDecl) ([]string, error) {
	segments := imp.PathSegments()
	if len(segments) == 0 {
		return nil, fmt.Errorf("declaração de import vazia")
	}

	if imp.IsStdlib {
		if r.StdlibDir == "" {
			return nil, fmt.Errorf("import %q: stdlib não disponível (soyuz build não configurado com stdlib)", imp.Path)
		}
		return r.resolveIn(r.StdlibDir, segments)
	}

	return r.resolveIn(r.Root, segments)
}

func (r *Resolver) resolveIn(base string, path []string) ([]string, error) {
	parts := append([]string{base}, path...)
	basePath := filepath.Join(parts...)

	// Módulo de arquivo único: <base>.sy
	singleFile := basePath + ".sy"
	if _, err := os.Stat(singleFile); err == nil {
		return []string{singleFile}, nil
	}

	// Módulo de diretório: <base>/
	if info, err := os.Stat(basePath); err == nil && info.IsDir() {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			return nil, fmt.Errorf("erro ao ler diretório %s: %w", basePath, err)
		}
		var files []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".sy") {
				files = append(files, filepath.Join(basePath, e.Name()))
			}
		}
		if len(files) > 0 {
			return files, nil
		}
	}

	return nil, fmt.Errorf("import não resolvido: %q — nenhum arquivo .sy encontrado em %s",
		strings.Join(path, "."), basePath)
}
