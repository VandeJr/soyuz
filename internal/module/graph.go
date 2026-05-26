package module

import (
	"fmt"
	"os"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// Collect retorna todos os arquivos .sy necessários para compilar entryFile,
// em ordem topológica (dependências antes dos arquivos que as importam).
// O prelude @soyuz/prelude é incluído automaticamente quando a stdlib está disponível.
// Detecta ciclos de import e retorna erro quando encontrado.
func Collect(entryFile string, resolver *Resolver) ([]string, error) {
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	var order []string

	var visit func(file string) error
	visit = func(file string) error {
		if visiting[file] {
			return fmt.Errorf("ciclo de import detectado em %s", file)
		}
		if visited[file] {
			return nil
		}
		visiting[file] = true

		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("erro ao ler %s: %w", file, err)
		}

		tokens := lexer.Tokenize(string(data))
		p := parser.New(tokens)
		prog := p.Parse()

		for _, node := range prog.Body {
			imp, ok := node.(*parser.ImportDecl)
			if !ok {
				continue
			}
			resolver.ImportingFile = file
			files, err := resolver.Resolve(imp)
			resolver.ImportingFile = ""
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			imp.ResolvedFiles = files // disponível no checker para namespace
			for _, f := range files {
				if err := visit(f); err != nil {
					return err
				}
			}
		}

		delete(visiting, file)
		visited[file] = true
		order = append(order, file)
		return nil
	}

	if preludeFiles, err := ResolvePrelude(resolver); err != nil {
		return nil, err
	} else {
		for _, f := range preludeFiles {
			if err := visit(f); err != nil {
				return nil, err
			}
		}
	}

	if err := visit(entryFile); err != nil {
		return nil, err
	}
	return order, nil
}

// ResolvePrelude returns stdlib prelude files, or nil when stdlib/prelude is unavailable.
func ResolvePrelude(resolver *Resolver) ([]string, error) {
	if resolver.StdlibDir == "" {
		return nil, nil
	}
	imp := &parser.ImportDecl{
		Path:     "@soyuz/prelude",
		PathKind: parser.ImportPathStdlib,
	}
	files, err := resolver.Resolve(imp)
	if err != nil {
		return nil, nil
	}
	return files, nil
}

// PreludeFiles is a convenience alias for ResolvePrelude.
func PreludeFiles(resolver *Resolver) ([]string, error) {
	return ResolvePrelude(resolver)
}
