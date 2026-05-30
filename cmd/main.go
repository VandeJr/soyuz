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
	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "new":
		cmdNew(os.Args[2:])
	case "build":
		cmdBuild(os.Args[2:])
	case "run":
		cmdRun(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Println("Uso: soyuz <comando> [argumentos]")
	fmt.Println("Comandos:")
	fmt.Println("  new [--lib] <nome>               Cria um novo projeto Soyuz")
	fmt.Println("  build [--release] [arquivo.sy]   Compila o projeto ou um arquivo")
	fmt.Println("  run   [--release] [arquivo.sy] [-- args...]  Compila e executa")
}

// cmdNew implements `soyuz new [--lib] <nome>`.
func cmdNew(args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	isLib := fs.Bool("lib", false, "Cria um projeto biblioteca em vez de binário")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Println("Erro: nome do projeto não especificado.")
		fmt.Println("Uso: soyuz new [--lib] <nome>")
		os.Exit(1)
	}
	name := rest[0]

	if err := os.MkdirAll(name, 0755); err != nil {
		fmt.Printf("Erro ao criar diretório '%s': %v\n", name, err)
		os.Exit(1)
	}

	projectType := "binary"
	if *isLib {
		projectType = "library"
	}

	tomlContent := fmt.Sprintf(`[project]
name    = "%s"
version = "0.1.0"
type    = "%s"
`, name, projectType)
	if !*isLib {
		tomlContent += `entry   = "main.sy"
`
	}
	tomlContent += `
[packages]
`
	if err := os.WriteFile(filepath.Join(name, "soyuz.toml"), []byte(tomlContent), 0644); err != nil {
		fmt.Printf("Erro ao criar soyuz.toml: %v\n", err)
		os.Exit(1)
	}

	if *isLib {
		libContent := fmt.Sprintf("// Biblioteca %s\n", name)
		if err := os.WriteFile(filepath.Join(name, "lib.sy"), []byte(libContent), 0644); err != nil {
			fmt.Printf("Erro ao criar lib.sy: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Projeto biblioteca '%s' criado em ./%s/\n", name, name)
	} else {
		mainContent := `import @soyuz/prelude

fn main() {
    print("Olá, mundo!")
}
`
		if err := os.WriteFile(filepath.Join(name, "main.sy"), []byte(mainContent), 0644); err != nil {
			fmt.Printf("Erro ao criar main.sy: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Projeto binário '%s' criado em ./%s/\n", name, name)
	}
}

// cmdBuild implements `soyuz build [--release] [-o output] [arquivo.sy]`.
func cmdBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outputFile := fs.String("o", "", "Nome do executável de saída")
	release := fs.Bool("release", false, "Build de release com otimizações (-O2)")
	fs.Parse(args)
	positional := fs.Args()

	if len(positional) > 0 {
		// Modo legado: soyuz build [-o saída] <arquivo.sy>
		out := *outputFile
		if out == "" {
			out = "output"
		}
		if err := buildInternal(positional[0], out, *release, ""); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return
	}

	// Modo project-aware: lê soyuz.toml do diretório atual (ou pai mais próximo).
	// LoadProjectConfig only uses filepath.Dir(arg) so any filename works as anchor.
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Erro ao obter diretório atual: %v\n", err)
		os.Exit(1)
	}
	cfg, err := module.LoadProjectConfig(filepath.Join(cwd, "_"))
	if err != nil {
		fmt.Printf("Erro ao carregar soyuz.toml: %v\n", err)
		os.Exit(1)
	}
	// Verificar se há soyuz.toml de fato no root encontrado.
	if _, serr := os.Stat(filepath.Join(cfg.Root, "soyuz.toml")); serr != nil {
		fmt.Println("Erro: nenhum arquivo especificado e soyuz.toml não encontrado.")
		fmt.Println("Uso: soyuz build [--release] <arquivo.sy>  ou execute dentro de um projeto Soyuz.")
		os.Exit(1)
	}

	entryFile := filepath.Join(cfg.Root, cfg.Meta.Entry)

	if cfg.Meta.Type == "library" {
		// Para bibliotecas: apenas valida tipos, não produz executável.
		if err := typeCheckOnly(entryFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Biblioteca '%s' v%s verificada com sucesso.\n", cfg.Meta.Name, cfg.Meta.Version)
		return
	}

	// Binary: output em target/debug/<name> ou target/release/<name>
	mode := "debug"
	if *release {
		mode = "release"
	}
	targetDir := filepath.Join(cfg.Root, "target", mode)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		fmt.Printf("Erro ao criar target dir: %v\n", err)
		os.Exit(1)
	}
	out := *outputFile
	if out == "" {
		out = filepath.Join(targetDir, cfg.Meta.Name)
	}
	if err := buildInternal(entryFile, out, *release, cfg.Root); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// cmdRun implements `soyuz run [--release] [arquivo.sy] [-- args...]`.
func cmdRun(args []string) {
	// Separar args do soyuz run dos args do programa compilado (depois de "--").
	runArgs := args
	var programArgs []string
	for i, a := range args {
		if a == "--" {
			runArgs = args[:i]
			programArgs = args[i+1:]
			break
		}
	}

	fs := flag.NewFlagSet("run", flag.ExitOnError)
	release := fs.Bool("release", false, "Build de release com otimizações (-O2)")
	fs.Parse(runArgs)
	positional := fs.Args()

	var inputFile string
	var projectRoot string

	if len(positional) > 0 {
		inputFile = positional[0]
	} else {
		// Modo project-aware.
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Printf("Erro ao obter diretório atual: %v\n", err)
			os.Exit(1)
		}
		cfg, err := module.LoadProjectConfig(filepath.Join(cwd, "_"))
		if err != nil {
			fmt.Printf("Erro ao carregar soyuz.toml: %v\n", err)
			os.Exit(1)
		}
		if _, serr := os.Stat(filepath.Join(cfg.Root, "soyuz.toml")); serr != nil {
			fmt.Println("Erro: nenhum arquivo especificado e soyuz.toml não encontrado.")
			fmt.Println("Uso: soyuz run [arquivo.sy] [-- args...]")
			os.Exit(1)
		}
		if cfg.Meta.Type == "library" {
			fmt.Println("Erro: projetos do tipo 'library' não podem ser executados.")
			os.Exit(1)
		}
		inputFile = filepath.Join(cfg.Root, cfg.Meta.Entry)
		projectRoot = cfg.Root
	}

	// Compila para um executável temporário.
	tmpBin, err := os.CreateTemp("", "soyuz-run-*")
	if err != nil {
		fmt.Printf("Erro ao criar executável temporário: %v\n", err)
		os.Exit(1)
	}
	tmpBin.Close()
	tmpBinPath := tmpBin.Name()
	defer os.Remove(tmpBinPath) // runs on normal return; explicit removes below cover os.Exit paths

	if err := buildInternal(inputFile, tmpBinPath, *release, projectRoot); err != nil {
		os.Remove(tmpBinPath)
		fmt.Println(err)
		os.Exit(1)
	}

	cmd := exec.Command(tmpBinPath, programArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Remove(tmpBinPath)
			os.Exit(exitErr.ExitCode())
		}
		fmt.Printf("Erro ao executar: %v\n", err)
		os.Exit(1)
	}
}

// typeCheckOnly runs the full pipeline up to type checking and reports errors, without codegen.
func typeCheckOnly(inputFile string) error {
	absInput, err := filepath.Abs(inputFile)
	if err != nil {
		return fmt.Errorf("erro ao resolver caminho: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "soyuz-")
	if err != nil {
		return fmt.Errorf("erro ao criar diretório temporário: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	stdlibDir, err := extractStdlib(tmpDir)
	if err != nil {
		return err
	}

	resolver := module.NewResolverWithStdlib(absInput, stdlibDir)
	files, err := module.Collect(absInput, resolver)
	if err != nil {
		return fmt.Errorf("erro ao resolver imports: %v", err)
	}

	allNodes, nodeFile, err := parseFiles(files, resolver)
	if err != nil {
		return err
	}

	mergedProg := &parser.Program{Body: allNodes}
	c := checker.New()
	if len(files) > 0 {
		c.SetNodeFiles(nodeFile)
	}
	if preludeFiles, perr := module.ResolvePrelude(resolver); perr == nil {
		c.SetPreludeFiles(preludeFiles)
	}
	result := c.Check(mergedProg)
	for _, w := range result.Warnings {
		if w.File != "" {
			fmt.Fprintf(os.Stderr, "aviso [%s %v]: %s (%s)\n", filepath.Base(w.File), w.Pos, w.Message, w.Code)
		} else {
			fmt.Fprintf(os.Stderr, "aviso %v: %s (%s)\n", w.Pos, w.Message, w.Code)
		}
	}
	if len(result.Errors) > 0 {
		fmt.Println("Erros de tipo encontrados:")
		for _, e := range result.Errors {
			if e.File != "" {
				fmt.Printf("  [%s %v]: %s\n", filepath.Base(e.File), e.Pos, e.Message)
			} else {
				fmt.Printf("  %v: %s\n", e.Pos, e.Message)
			}
		}
		return fmt.Errorf("verificação de tipos falhou")
	}
	return nil
}

// buildInternal is the core compilation pipeline. projectRoot is used for the runtime/ dir lookup;
// pass "" to fall back to the entry file's directory (legacy behaviour).
func buildInternal(inputFile, outputFile string, release bool, projectRoot string) error {
	absInput, err := filepath.Abs(inputFile)
	if err != nil {
		return fmt.Errorf("erro ao resolver caminho: %v", err)
	}

	tmpDir, err := os.MkdirTemp("", "soyuz-")
	if err != nil {
		return fmt.Errorf("erro ao criar diretório temporário: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	stdlibDir, err := extractStdlib(tmpDir)
	if err != nil {
		return err
	}

	resolver := module.NewResolverWithStdlib(absInput, stdlibDir)
	files, err := module.Collect(absInput, resolver)
	if err != nil {
		return fmt.Errorf("erro ao resolver imports: %v", err)
	}

	allNodes, nodeFile, err := parseFiles(files, resolver)
	if err != nil {
		return err
	}

	mergedProg := &parser.Program{Body: allNodes}

	c := checker.New()
	if len(files) > 0 {
		c.SetNodeFiles(nodeFile)
	}
	if preludeFiles, perr := module.ResolvePrelude(resolver); perr == nil {
		c.SetPreludeFiles(preludeFiles)
	}
	result := c.Check(mergedProg)
	for _, w := range result.Warnings {
		if w.File != "" {
			fmt.Fprintf(os.Stderr, "aviso [%s %v]: %s (%s)\n", filepath.Base(w.File), w.Pos, w.Message, w.Code)
		} else {
			fmt.Fprintf(os.Stderr, "aviso %v: %s (%s)\n", w.Pos, w.Message, w.Code)
		}
	}
	if len(result.Errors) > 0 {
		fmt.Println("Erros de tipo encontrados:")
		for _, e := range result.Errors {
			if e.File != "" {
				fmt.Printf("  [%s %v]: %s\n", filepath.Base(e.File), e.Pos, e.Message)
			} else {
				fmt.Printf("  %v: %s\n", e.Pos, e.Message)
			}
		}
		return fmt.Errorf("compilação abortada por erros de tipo")
	}

	g := codegen.New(result)
	mod, err := g.Generate(mergedProg)
	if err != nil {
		return fmt.Errorf("erro no codegen: %v", err)
	}

	llFile := filepath.Join(tmpDir, "out.ll")
	if err = os.WriteFile(llFile, []byte(mod.String()), 0644); err != nil {
		return fmt.Errorf("erro ao escrever IR LLVM: %v", err)
	}

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
		{"soyuz_rt.h", soyuzruntime.SoyuzRTHeader},
		{"soyuz_rt.c", soyuzruntime.SoyuzRTSource},
		{"std_sync.c", soyuzruntime.StdSyncSource},
		{"std_channel.c", soyuzruntime.StdChannelSource},
		{"std_arc.c", soyuzruntime.StdArcSource},
	}
	clangArgs := []string{llFile}
	for _, src := range cSources {
		path := filepath.Join(tmpDir, src.name)
		if err = os.WriteFile(path, src.data, 0644); err != nil {
			return fmt.Errorf("erro ao extrair runtime (%s): %v", src.name, err)
		}
		if strings.HasSuffix(src.name, ".c") {
			clangArgs = append(clangArgs, path)
		}
	}

	// Incluir arquivos C do diretório runtime/ do projeto (se existir).
	runtimeDir := projectRoot
	if runtimeDir == "" {
		runtimeDir = filepath.Dir(absInput)
	}
	projectRuntimeDir := filepath.Join(runtimeDir, "runtime")
	if entries, rerr := os.ReadDir(projectRuntimeDir); rerr == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".c") {
				continue
			}
			srcPath := filepath.Join(projectRuntimeDir, entry.Name())
			data, rerr2 := os.ReadFile(srcPath)
			if rerr2 != nil {
				return fmt.Errorf("erro ao ler runtime do projeto (%s): %v", entry.Name(), rerr2)
			}
			dst := filepath.Join(tmpDir, entry.Name())
			if werr := os.WriteFile(dst, data, 0644); werr != nil {
				return fmt.Errorf("erro ao copiar runtime do projeto (%s): %v", entry.Name(), werr)
			}
			clangArgs = append(clangArgs, dst)
		}
	}

	clangArgs = append(clangArgs, "-I", tmpDir, "-pthread")
	if release {
		clangArgs = append(clangArgs, "-O2")
	}
	clangArgs = append(clangArgs, "-o", outputFile)

	cmd := exec.Command("clang", clangArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("erro de linkagem (verifique se 'clang' está instalado): %v", err)
	}

	if len(files) > 1 {
		fmt.Printf("Build concluído: %s (%d arquivos compilados)\n", outputFile, len(files))
	} else {
		fmt.Printf("Build concluído: %s\n", outputFile)
	}
	return nil
}

// extractStdlib writes the embedded stdlib to tmpDir/stdlib/ and returns the path.
func extractStdlib(tmpDir string) (string, error) {
	stdlibDir := filepath.Join(tmpDir, "stdlib")
	if err := os.MkdirAll(stdlibDir, 0755); err != nil {
		return "", fmt.Errorf("erro ao criar stdlib dir: %v", err)
	}
	for name, data := range soyuzstdlib.Files {
		dest := filepath.Join(stdlibDir, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return "", fmt.Errorf("erro ao criar diretório stdlib (%s): %v", name, err)
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return "", fmt.Errorf("erro ao extrair stdlib (%s): %v", name, err)
		}
	}
	return stdlibDir, nil
}

// parseFiles lexes and parses all files, returning the merged node list and file map.
func parseFiles(files []string, resolver *module.Resolver) ([]parser.Node, map[parser.Node]string, error) {
	var allNodes []parser.Node
	nodeFile := make(map[parser.Node]string)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("erro ao ler %s: %v", file, err)
		}
		tokens := lexer.Tokenize(string(data))
		p := parser.New(tokens)
		prog := p.Parse()
		if p.HasErrors() {
			fmt.Printf("Erros de parse em %s:\n", file)
			for _, e := range p.Errors() {
				fmt.Printf("  %s\n", e.Error())
			}
			return nil, nil, fmt.Errorf("erros de parse em %s", file)
		}
		for _, node := range prog.Body {
			if imp, isImport := node.(*parser.ImportDecl); isImport {
				if resolved, rerr := resolver.Resolve(imp); rerr == nil {
					imp.ResolvedFiles = resolved
				}
			}
			nodeFile[node] = file
			allNodes = append(allNodes, node)
		}
	}
	return allNodes, nodeFile, nil
}
