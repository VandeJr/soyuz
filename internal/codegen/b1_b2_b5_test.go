package codegen

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestSelectDefaultBoolReturn(t *testing.T) {
	src := `
fn pick() -> Bool {
    return select {
        default => true
    }
}`
	ir := compileCheck(t, src)
	if err := os.WriteFile("/tmp/sel_bool.ll", []byte(ir), 0644); err == nil {
		if out, err := exec.Command("llvm-as", "/tmp/sel_bool.ll", "-o", "/dev/null").CombinedOutput(); err != nil {
			t.Fatalf("llvm-as: %v\n%s", err, out)
		}
	}
}

func TestGenericEnumVariantAsCtorArg(t *testing.T) {
	src := `
enum Tree[T] {
    Leaf { val: T }
    Node { left: Tree[T], right: Tree[T] }
}
fn main() -> Int {
    val left = Tree.Leaf(1)
    val right = Tree.Leaf(2)
    val t = Tree.Node(left, right)
    return 0
}`
	ir := compileCheck(t, src)
	if strings.Contains(ir, "undefined identifier: Tree") {
		t.Fatalf("generic enum variant value must resolve: %s", ir)
	}
}

func TestGenericEnumVariantThreeFieldCtor(t *testing.T) {
	src := `
enum Arvore[T] {
    Folha { val: T }
    No { val: T, left: Arvore[T], right: Arvore[T] }
}
fn main() -> Int {
    val t = Arvore.No(1, Arvore.Folha(0), Arvore.Folha(0))
    return 0
}`
	ir := compileCheck(t, src)
	if strings.Contains(ir, "undefined identifier") {
		t.Fatalf("recursive generic enum ctor args: %s", ir)
	}
	if err := os.WriteFile("/tmp/arvore.ll", []byte(ir), 0644); err == nil {
		if out, err := exec.Command("llvm-as", "/tmp/arvore.ll", "-o", "/dev/null").CombinedOutput(); err != nil {
			t.Fatalf("llvm-as: %v\n%s", err, out)
		}
	}
}
