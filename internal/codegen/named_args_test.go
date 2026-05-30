package codegen

import (
	"testing"
)

func TestNamedEnumConstructorRun(t *testing.T) {
	src := `
enum Forma {
    Circulo(raio: Float)
    Retangulo(w: Float, h: Float)
}
fn main() -> Int {
    val c = Forma.Circulo(raio: 5.0)
    val r = Forma.Retangulo(w: 10.0, h: 4.0)
    return 0
}`
	ir := compileCheck(t, src)
	if ir == "" {
		t.Fatal("IR vazio")
	}
}
