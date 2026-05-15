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
	fmt.Println("  build [-o saída] <arquivo.soyuz>  Compila um arquivo Soyuz em executável")
}

func build(inputFile, outputFile string) {
	absInput, err := filepath.Abs(inputFile)
	if err != nil {
		fmt.Printf("Erro ao resolver caminho: %v\n", err)
		os.Exit(1)
	}

	// 0. Criar tmpDir cedo para extrair stdlib e runtime juntos.
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
		// Arquivos stdlib são embutidos como .sy; o resolver procura .soyuz.
		// Preserva estrutura de diretórios: "collections/list.sy" → stdlibDir/collections/list.soyuz
		dest := filepath.Join(stdlibDir, strings.TrimSuffix(name, ".sy")+".soyuz")
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
				// Bare stdlib import (@soyuz.mock sem nomes): incluir para o checker
				// criar o namespace mock.* após o registro de assinaturas.
				// Re-resolve aqui para popular ResolvedFiles nesta instância de AST.
				if imp.IsStdlib && len(imp.Names) == 0 && !imp.Wildcard {
					if resolved, rerr := resolver.Resolve(imp); rerr == nil {
						imp.ResolvedFiles = resolved
					}
					nodeFile[node] = file
					allNodes = append(allNodes, node)
				}
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
			fmt.Printf("  %v: %s\n", e.Pos, e.Message)
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

	// Escrever IR LLVM em arquivo temporário (tmpDir já criado no passo 0).
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
		{"rc.c", soyuzruntime.Source},
		{"std_io.c", soyuzruntime.StdIOSource},
	}
	clangArgs := []string{llFile}
	for _, src := range cSources {
		path := filepath.Join(tmpDir, src.name)
		if err = os.WriteFile(path, src.data, 0644); err != nil {
			fmt.Printf("Erro ao extrair runtime (%s): %v\n", src.name, err)
			os.Exit(1)
		}
		clangArgs = append(clangArgs, path)
	}
	clangArgs = append(clangArgs, "-o", outputFile)
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
