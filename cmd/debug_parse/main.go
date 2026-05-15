//go:build ignore

package main

import (
	"fmt"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func main() {
	src := "enum Tree[T] {\n\tLeaf { val: T }\n\tNode { left: Tree[T], right: Tree[T] }\n}\nfn main() -> Int { return 0 }"
	tokens := lexer.Tokenize(src)
	for _, t := range tokens {
		fmt.Printf("%-20v %q\n", t.Type, t.Lexeme)
	}
	fmt.Println("---")
	p := parser.New(tokens)
	prog := p.Parse()
	fmt.Printf("parse errors: %v\n", p.Errors())
	fmt.Printf("body: %d nodes\n", len(prog.Body))
}
