package checker

import "testing"

func TestMatchArmAssignBlockIsUnit(t *testing.T) {
	src := `
fn bump(opt: Option[Int]) -> Int {
    var total = 0
    match opt {
        Some(v) => { total = total + v }
        None    => { }
    }
    return total
}`
	res := checkSrc(src)
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
}
