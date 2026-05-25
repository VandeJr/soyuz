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
	Root          string // fallback: diretório do entry file
	ProjectRoot   string // diretório do .soyuz-root / soyuz.toml
	StdlibDir     string
	Packages      map[string]string // alias → path relativo ao ProjectRoot
	ImportingFile string            // set during Resolve for relative imports
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
	if cfg, err := LoadProjectConfig(entryFile); err == nil {
		r.ProjectRoot = cfg.Root
		r.Packages = cfg.Packages
	}
	return r
}

// Resolve retorna os arquivo(s) .sy que satisfazem imp.
func (r *Resolver) Resolve(imp *parser.ImportDecl) ([]string, error) {
	kind := imp.PathKind
	if kind == parser.ImportPathLegacy && imp.IsStdlib {
		kind = parser.ImportPathStdlib
	}

	switch kind {
	case parser.ImportPathStdlib:
		if r.StdlibDir == "" {
			return nil, fmt.Errorf("import %q: stdlib não disponível", imp.Path)
		}
		return r.resolveIn(r.StdlibDir, imp.PathSegments())

	case parser.ImportPathProjectRoot:
		base := r.projectBase()
		return r.resolveIn(base, imp.PathSegments())

	case parser.ImportPathPackageAlias:
		if r.ProjectRoot == "" && len(r.Packages) == 0 {
			return nil, fmt.Errorf("import %q: project root não encontrado (crie .soyuz-root ou soyuz.toml)", imp.Path)
		}
		cfg := &ProjectConfig{Root: r.ProjectRoot, Packages: r.Packages}
		segs, err := cfg.ResolveAliasPath(imp.PackageAlias, imp.PathSegments())
		if err != nil {
			return nil, err
		}
		return r.resolveIn(r.projectBase(), segs)

	case parser.ImportPathRelative:
		if r.ImportingFile == "" {
			return nil, fmt.Errorf("import relativo %q sem arquivo importador", imp.Path)
		}
		dir := filepath.Dir(r.ImportingFile)
		rel := filepath.FromSlash(imp.Path)
		target := filepath.Clean(filepath.Join(dir, rel))
		return r.resolveFileOrDir(target)

	default:
		base := r.projectBase()
		return r.resolveIn(base, imp.PathSegments())
	}
}

func (r *Resolver) projectBase() string {
	if r.ProjectRoot != "" {
		return r.ProjectRoot
	}
	return r.Root
}

func (r *Resolver) resolveFileOrDir(basePath string) ([]string, error) {
	singleFile := basePath + ".sy"
	if _, err := os.Stat(singleFile); err == nil {
		return []string{singleFile}, nil
	}
	if info, err := os.Stat(basePath); err == nil && info.IsDir() {
		return r.readDirSy(basePath)
	}
	return nil, fmt.Errorf("import não resolvido: %q", basePath)
}

func (r *Resolver) resolveIn(base string, path []string) ([]string, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("declaração de import vazia")
	}
	parts := append([]string{base}, path...)
	basePath := filepath.Join(parts...)
	return r.resolveFileOrDir(basePath)
}

func (r *Resolver) readDirSy(basePath string) ([]string, error) {
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
	return nil, fmt.Errorf("import não resolvido: nenhum .sy em %s", basePath)
}
