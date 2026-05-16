package codegen

import (
	"strings"
	"testing"

	"soyuz/internal/checker"
	"soyuz/internal/lexer"
	"soyuz/internal/parser"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func collIR(t *testing.T, src string) string {
	t.Helper()
	tokens := lexer.Tokenize(src)
	prog := parser.New(tokens).Parse()
	res := checker.New().Check(prog)
	if len(res.Errors) > 0 {
		t.Fatalf("checker errors: %v", res.Errors)
	}
	mod, err := New(res).Generate(prog)
	if err != nil {
		t.Fatalf("codegen error: %v", err)
	}
	return mod.String()
}

// ─── range / rangeInclusive / rangeStep ───────────────────────────────────────

func TestCollectionsRangeExternDecl(t *testing.T) {
	src := `
extern fn soyuz_range(from: Int, to: Int) -> List[Int]
pub fn range(from: Int, to: Int) -> List[Int] = soyuz_range(from, to)
fn main() {
  val r = range(0, 5)
  print(r.size())
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "@soyuz_range") {
		t.Fatalf("esperado @soyuz_range no IR, obteve:\n%s", ir)
	}
	if !strings.Contains(ir, "SoyuzList") {
		t.Fatalf("esperado referência a SoyuzList no IR, obteve:\n%s", ir)
	}
}

func TestCollectionsRangeStepExternDecl(t *testing.T) {
	src := `
extern fn soyuz_range_step(from: Int, to: Int, step: Int) -> List[Int]
pub fn rangeStep(from: Int, to: Int, step: Int) -> List[Int] = soyuz_range_step(from, to, step)
fn main() {
  val r = rangeStep(0, 10, 2)
  print(r.size())
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "@soyuz_range_step") {
		t.Fatalf("esperado @soyuz_range_step no IR, obteve:\n%s", ir)
	}
}

// ─── built-in List FP methods ─────────────────────────────────────────────────

func TestCollectionsListMap(t *testing.T) {
	src := `
fn main() {
  val xs = [1, 2, 3]
  val ys = xs.map(fn(x: Int) => x * 2)
  print(ys.size())
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "soyuz_list_new") {
		t.Fatalf("esperado soyuz_list_new no IR (resultado do map), obteve:\n%s", ir)
	}
}

func TestCollectionsListFilter(t *testing.T) {
	src := `
fn main() {
  val xs = [1, 2, 3, 4]
  val evens = xs.filter(fn(x: Int) => x == 0)
  print(evens.size())
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "soyuz_list_new") {
		t.Fatalf("esperado soyuz_list_new no IR (resultado do filter), obteve:\n%s", ir)
	}
}

func TestCollectionsListReduce(t *testing.T) {
	src := `
fn main() {
  val xs = [1, 2, 3]
  val total = xs.reduce(fn(acc: Int, x: Int) => acc + x, 0)
  print(total)
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "soyuz_list_get") {
		t.Fatalf("esperado soyuz_list_get no IR (iteração do reduce), obteve:\n%s", ir)
	}
}

func TestCollectionsListJoin(t *testing.T) {
	src := `
fn main() {
  val xs = ["a", "b", "c"]
  val s = xs.join(", ")
  print(s)
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "soyuz_str_concat") {
		t.Fatalf("esperado soyuz_str_concat no IR (join), obteve:\n%s", ir)
	}
}

func TestCollectionsListIsEmpty(t *testing.T) {
	src := `
fn main() {
  val xs = [1]
  print(xs.isEmpty())
}
`
	ir := collIR(t, src)
	if !strings.Contains(ir, "icmp") {
		t.Fatalf("esperado icmp (comparação de isEmpty) no IR, obteve:\n%s", ir)
	}
}

// ─── path.sy: extern fn usados sem import de string.sy ───────────────────────

func TestPathExternFnsPresent(t *testing.T) {
	src := `
extern fn soyuz_str_concat(s1: String, s2: String) -> String
extern fn soyuz_str_last_index_of(s: String, sub: String) -> Int
extern fn soyuz_str_substring(s: String, start: Int, end: Int) -> String
extern fn soyuz_str_len(s: String) -> Int
extern fn soyuz_str_starts_with(s: String, prefix: String) -> Bool
extern fn soyuz_str_ends_with(s: String, suffix: String) -> Bool

pub class Path {
    pub val _path: String

    pub fn join(self, part: String) -> Path {
        if soyuz_str_ends_with(self._path, "/") {
            return Path { _path: soyuz_str_concat(self._path, part) }
        }
        return Path { _path: soyuz_str_concat(soyuz_str_concat(self._path, "/"), part) }
    }

    pub fn name(self) -> String {
        val idx = soyuz_str_last_index_of(self._path, "/")
        if idx == -1 { return self._path }
        return soyuz_str_substring(self._path, idx + 1, soyuz_str_len(self._path))
    }

    pub fn isAbsolute(self) -> Bool = soyuz_str_starts_with(self._path, "/")
}

pub fn path(p: String) -> Path = Path { _path: p }

fn main() {
  val p = path("/home/user/file.txt")
  print(p.name())
}
`
	ir := collIR(t, src)
	for _, fn := range []string{
		"@soyuz_str_concat",
		"@soyuz_str_last_index_of",
		"@soyuz_str_substring",
		"@soyuz_str_len",
		"@soyuz_str_starts_with",
	} {
		if !strings.Contains(ir, fn) {
			t.Fatalf("esperado %s declarado no IR, obteve:\n%s", fn, ir)
		}
	}
}
