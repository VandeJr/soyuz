package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"soyuz/internal/checker"
	"soyuz/internal/codegen"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
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
		// Flag parsing expects flags BEFORE positional arguments.
		// e.g., build -o output hello.soyuz
		buildCommand.Parse(os.Args[2:])
		args := buildCommand.Args()
		if len(args) == 0 {
			fmt.Println("Error: No input file specified.")
			usage()
			os.Exit(1)
		}
		build(args[0], *outputFile)
	default:
		usage()
	}
}

func usage() {
	fmt.Println("Usage: soyuz <command> [arguments]")
	fmt.Println("Commands:")
	fmt.Println("  build [-o output] <file.soyuz>  Compile a Soyuz file into an executable")
}

func build(inputFile, outputFile string) {
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	// 1. Lexing
	tokens := lexer.Tokenize(string(data))

	// 2. Parsing
	p := parser.New(tokens)
	prog := p.Parse()
	if p.HasErrors() {
		fmt.Println("Parse errors found:")
		for _, e := range p.Errors() {
			fmt.Printf("  %s\n", e.Error())
		}
		os.Exit(1)
	}

	// 3. Type Checking
	c := checker.New()
	result := c.Check(prog)
	if len(result.Errors) > 0 {
		fmt.Println("Type errors found:")
		for _, e := range result.Errors {
			fmt.Printf("  %v: %s\n", e.Pos, e.Message)
		}
		os.Exit(1)
	}

	// 4. Codegen (LLVM IR)
	g := codegen.New(result)
	mod, err := g.Generate(prog)
	if err != nil {
		fmt.Printf("Codegen error: %v\n", err)
		os.Exit(1)
	}

	// Debug: print IR
	// fmt.Println(mod.String())

	// Write LLVM IR to a temporary file
	tmpDir, err := os.MkdirTemp("", "soyuz-")
	if err != nil {
		fmt.Printf("Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	llFile := filepath.Join(tmpDir, "out.ll")
	err = os.WriteFile(llFile, []byte(mod.String()), 0644)
	if err != nil {
		fmt.Printf("Error writing LLVM IR: %v\n", err)
		os.Exit(1)
	}

	// 5. Link with Clang (include RC runtime)
	rcSrc := filepath.Join(filepath.Dir(os.Args[0]), "..", "runtime", "rc.c")
	if _, serr := os.Stat(rcSrc); os.IsNotExist(serr) {
		// Fall back to path relative to working directory.
		rcSrc = "runtime/rc.c"
	}
	cmd := exec.Command("clang", llFile, rcSrc, "-o", outputFile)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()
	if err != nil {
		fmt.Printf("Linking error (ensure 'clang' is installed): %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully built %s\n", outputFile)
}
