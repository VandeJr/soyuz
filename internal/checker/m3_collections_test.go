package checker

import (
	"testing"

	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

func checkCollections(t *testing.T, src string) *CheckResult {
	t.Helper()
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	return New().Check(prog)
}

func TestListMap(t *testing.T) {
	src := `fn main() -> List[Int] {
        val xs = [1, 2, 3]
        return xs.map(fn(x) => x * 2)
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestListFilter(t *testing.T) {
	src := `fn main() -> List[Int] {
        val xs = [1, 2, 3, 4]
        return xs.filter(fn(x) => x > 2)
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestListReduce(t *testing.T) {
	src := `fn main() -> Int {
        val xs = [1, 2, 3]
        return xs.reduce(fn(acc, x) => acc + x, 0)
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestListJoin(t *testing.T) {
	src := `fn main() -> String {
        val words = ["hello", "world"]
        return words.join(", ")
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestListIsEmpty(t *testing.T) {
	src := `fn main() -> Bool {
        val xs = [1, 2, 3]
        return xs.isEmpty()
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestMapKeys(t *testing.T) {
	src := `fn main() -> List[String] {
        val m = ["a": 1, "b": 2]
        return m.keys()
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}

func TestMapValues(t *testing.T) {
	src := `fn main() -> List[Int] {
        val m = ["a": 1, "b": 2]
        return m.values()
    }`
	res := checkCollections(t, src)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
}
