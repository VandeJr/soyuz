package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"soyuz/internal/checker"
	"soyuz/internal/codegen"
	"soyuz/internal/lexer"
	"soyuz/internal/module"
	"soyuz/internal/parser"
	soyuzruntime "soyuz/internal/runtime"
	soyuzstdlib "soyuz/std"
)

func main() {
	buildCommand := flag.NewFlagSet("build", flag.ExitOnError)
	outputFile := buildCommand.String("o", "output", "Output executable name")

	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "build":
		buildCommand.Parse(os.Args[2:])
		args := buildCommand.Args()
		if len(args) == 0 {
			fmt.Println("Erro: nenhum arquivo de entrada especificado.")
			usage()
			os.Exit(1)
		}
		build(args[0], *outputFile)
	default:
		usage()
	}
}

func usage() {
	fmt.Println("Uso: soyuz <comando> [argumentos]")
	fmt.Println("Comandos:")
	fmt.Println("  build [-o saída] <arquivo.sy>  Compila um arquivo Soyuz em executável")
}

func build(inputFile, outputFile string) {
	absInput, err := filepath.Abs(inputFile)
	if err != nil {
		fmt.Printf("Erro ao resolver caminho: %v\n", err)
		os.Exit(1)
	}

	// 0. tmpDir — tudo vai para /tmp e é apagado após o build.
	tmpDir, err := os.MkdirTemp("", "soyuz-")
	if err != nil {
		fmt.Printf("Erro ao criar diretório temporário: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// Extrair stdlib embutida para tmpDir/stdlib/
	stdlibDir := filepath.Join(tmpDir, "stdlib")
	if err = os.MkdirAll(stdlibDir, 0755); err != nil {
		fmt.Printf("Erro ao criar stdlib dir: %v\n", err)
		os.Exit(1)
	}
	for name, data := range soyuzstdlib.Files {
		// Arquivos stdlib são embutidos como .sy.
		// Preserva estrutura de diretórios: "collections/list.sy" → stdlibDir/collections/list.sy
		dest := filepath.Join(stdlibDir, name)
		if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			fmt.Printf("Erro ao criar diretório stdlib (%s): %v\n", name, err)
			os.Exit(1)
		}
		if err = os.WriteFile(dest, data, 0644); err != nil {
			fmt.Printf("Erro ao extrair stdlib (%s): %v\n", name, err)
			os.Exit(1)
		}
	}

	// 1. Resolver imports — coleta todos os arquivos em ordem topológica.
	resolver := module.NewResolverWithStdlib(absInput, stdlibDir)
	files, err := module.Collect(absInput, resolver)
	if err != nil {
		fmt.Printf("Erro ao resolver imports: %v\n", err)
		os.Exit(1)
	}

	// 2. Parsear todos os arquivos e mesclar num único programa.
	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("Erro ao ler %s: %v\n", file, err)
			os.Exit(1)
		}

		tokens := lexer.Tokenize(string(data))
		p := parser.New(tokens)
		prog := p.Parse()
		if p.HasErrors() {
			fmt.Printf("Erros de parse em %s:\n", file)
			for _, e := range p.Errors() {
				fmt.Printf("  %s\n", e.Error())
			}
			os.Exit(1)
		}

		for _, node := range prog.Body {
			if imp, isImport := node.(*parser.ImportDecl); isImport {
				if resolved, rerr := resolver.Resolve(imp); rerr == nil {
					imp.ResolvedFiles = resolved
				}
				nodeFile[node] = file
				allNodes = append(allNodes, node)
				continue
			}
			nodeFile[node] = file
			allNodes = append(allNodes, node)
		}
	}

	mergedProg := &parser.Program{}
	mergedProg.Body = allNodes

	// 3. Checagem de tipos (com enforcement de pub cross-file quando multi-arquivo).
	c := checker.New()
	if len(files) > 1 {
		c.SetNodeFiles(nodeFile)
	}
	result := c.Check(mergedProg)
	if len(result.Errors) > 0 {
		fmt.Println("Erros de tipo encontrados:")
		for _, e := range result.Errors {
			if e.File != "" {
				fmt.Printf("  [%s %v]: %s\n", filepath.Base(e.File), e.Pos, e.Message)
			} else {
				fmt.Printf("  %v: %s\n", e.Pos, e.Message)
			}
		}
		os.Exit(1)
	}

	// 4. Geração de código (LLVM IR)
	g := codegen.New(result)
	mod, err := g.Generate(mergedProg)
	if err != nil {
		fmt.Printf("Erro no codegen: %v\n", err)
		os.Exit(1)
	}

	llFile := filepath.Join(tmpDir, "out.ll")
	err = os.WriteFile(llFile, []byte(mod.String()), 0644)
	if err != nil {
		fmt.Printf("Erro ao escrever IR LLVM: %v\n", err)
		os.Exit(1)
	}

	// 5. Linkar com Clang (inclui runtime RC e stdlib embutidos no binário)
	cSources := []struct {
		name string
		data []byte
	}{
		{"soyuz.h", soyuzruntime.SoyuzHeader},
		{"rc.c", soyuzruntime.Source},
		{"std_io.c", soyuzruntime.StdIOSource},
		{"std_string.c", soyuzruntime.StdStringSource},
		{"std_fs.c", soyuzruntime.StdFSSource},
		{"std_os.c", soyuzruntime.StdOSSource},
		{"std_collections.c", soyuzruntime.StdCollectionsSource},
	}
	clangArgs := []string{llFile}
	for _, src := range cSources {
		path := filepath.Join(tmpDir, src.name)
		if err = os.WriteFile(path, src.data, 0644); err != nil {
			fmt.Printf("Erro ao extrair runtime (%s): %v\n", src.name, err)
			os.Exit(1)
		}
		// Only pass .c files to clang; headers (.h) are found via the tmp dir.
		if strings.HasSuffix(src.name, ".c") {
			clangArgs = append(clangArgs, path)
		}
	}
	// 5b. Incluir arquivos C do diretório runtime/ do projeto (se existir).
	// O soyuz.h embutido já está em tmpDir, portanto acessível via -I do clang.
	projectRuntimeDir := filepath.Join(filepath.Dir(absInput), "runtime")
	if entries, rerr := os.ReadDir(projectRuntimeDir); rerr == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".c") {
				continue
			}
			srcPath := filepath.Join(projectRuntimeDir, entry.Name())
			data, rerr2 := os.ReadFile(srcPath)
			if rerr2 != nil {
				fmt.Printf("Erro ao ler runtime do projeto (%s): %v\n", entry.Name(), rerr2)
				os.Exit(1)
			}
			dst := filepath.Join(tmpDir, entry.Name())
			if werr := os.WriteFile(dst, data, 0644); werr != nil {
				fmt.Printf("Erro ao copiar runtime do projeto (%s): %v\n", entry.Name(), werr)
				os.Exit(1)
			}
			clangArgs = append(clangArgs, dst)
		}
	}

	clangArgs = append(clangArgs, "-I", tmpDir, "-o", outputFile)
	cmd := exec.Command("clang", clangArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Erro de linkagem (verifique se 'clang' está instalado): %v\n", err)
		os.Exit(1)
	}

	if len(files) > 1 {
		fmt.Printf("Build concluído: %s (%d arquivos compilados)\n", outputFile, len(files))
	} else {
		fmt.Printf("Build concluído: %s\n", outputFile)
	}
}
