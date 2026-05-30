package codegen

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEnumMatchMultiBinding(t *testing.T) {
	src := `
enum Forma { Retangulo(w: Float, h: Float), Circulo(raio: Float) }
fn area(f: Forma) -> Float {
    return match f {
        Retangulo(w, h) => w * h
        Circulo(r) => 3.14 * r * r
    }
}
fn main() -> Int {
    print("$(area(Forma.Retangulo(w: 2.0, h: 3.0)))")
    return 0
}`
	ir := compileCheck(t, src)
	if strings.Contains(ir, "undefined") {
		t.Fatalf("codegen failed for multi-binding enum match")
	}
}

func TestRecordDeepEquality(t *testing.T) {
	src := `
record Ponto { x: Float, y: Float }
fn main() -> Int {
    val p1 = Ponto { x: 1.0, y: 2.0 }
    val p2 = Ponto { x: 1.0, y: 2.0 }
    val same = p1 == p2
    if same { print("eq") } else { print("ne") }
    return 0
}`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "rec_eq_") {
		t.Error("expected deep record equality blocks in IR")
	}
	if strings.Contains(ir, "br i32") {
		t.Fatalf("record equality must produce i1 condition, got br i32 in IR")
	}
	if err := os.WriteFile("/tmp/eq3_test.ll", []byte(ir), 0644); err == nil {
		if out, err := exec.Command("llvm-as", "/tmp/eq3_test.ll", "-o", "/dev/null").CombinedOutput(); err != nil {
			t.Fatalf("llvm-as: %v\n%s", err, out)
		}
	}
}

func TestPipeQuestSync(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn wrap(n: Int) -> Result[Int] = Ok(n)
fn main() -> Int {
    val r = wrap(5) |?> double
    return match r { Ok(v) => v, Err(_) => 0 }
}`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "pq_ok") {
		t.Error("expected pipe-quest blocks in IR")
	}
	if strings.Contains(ir, "phi i64") && strings.Contains(ir, "pq_fail") {
		t.Error("pipe-quest phi must use consistent Result pointer types")
	}
}

func TestExtendIntMethod(t *testing.T) {
	src := `
extend Int { pub fn dobro(self) -> Int = self * 2 }
fn main() -> Int { return 42.dobro() }`
	ir := compileCheck(t, src)
	if !strings.Contains(ir, "Int_dobro") {
		t.Error("expected Int extension method in IR")
	}
}

func TestRawStringLiteral(t *testing.T) {
	src := `fn main() -> Int { val s = r"\d+"; print(s); return 0 }`
	ir := compileCheck(t, src)
	if strings.Contains(ir, "undefined identifier: r") {
		t.Fatal("raw string prefix must not be parsed as identifier")
	}
	if !strings.Contains(ir, `d+`) {
		t.Error("expected raw string content in IR")
	}
}

func TestMultilineMethodChain(t *testing.T) {
	src := `
fn main() {
    val xs = [1, 2, 3]
        .filter(fn(x: Int) => x > 1)
    print("$(xs.size())")
}`
	ir := compileCheck(t, src)
	if ir == "" {
		t.Fatal("expected IR for multiline chain")
	}
}
