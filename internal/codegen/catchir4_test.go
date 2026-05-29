package codegen

import (
	"fmt"
	"strings"
	"testing"
)

func TestCatchIRDump4(t *testing.T) {
	src := `
fn tentativa(n: Int) -> Result[Int] {
    if n > 0 { return Ok(n) }
    return Err("neg")
}

fn main() {
    val t1a = task tentativa(-5)
    val t1 = t1a.catch(fn(e) => Ok(99))
    val r1 = t1.await()
    match r1 {
        Ok(v)  => print("ok: $(v)")
        Err(e) => print("err")
    }
}
`
	ir := compileTask(t, src)
	lines := strings.Split(ir, "\n")
	inMain := false
	for _, l := range lines {
		if strings.HasPrefix(l, "define void @main") || strings.HasPrefix(l, "define i64 @main") || strings.HasPrefix(l, "define %Result") {
			// skip non-main
		}
		if strings.HasPrefix(l, "define") && strings.Contains(l, "@main") {
			inMain = true
		}
		if inMain {
			fmt.Println(l)
		}
		if inMain && strings.TrimSpace(l) == "}" {
			break
		}
	}
}
