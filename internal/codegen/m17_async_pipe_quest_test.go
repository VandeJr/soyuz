package codegen

import (
	"strings"
	"testing"
)

// ── M-17: ~?> async pipe-quest ────────────────────────────────────────────────

// TestAsyncPipeQuestEmitsTagCheck verifies that a ~?> step emits a tag check
// (conditional branch) for short-circuit on Err/None.
func TestAsyncPipeQuestEmitsTagCheck(t *testing.T) {
	src := `
fn validate(n: Int) -> Result[Int] = Ok(n * 2)
fn double(n: Int) -> Int = n + 1

fn main() {
  val n = 5
  val t = n ~> validate ~?> double
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("esperado srt_enqueue no IR")
	}
	if !strings.Contains(ir, "srt_await") {
		t.Error("esperado srt_await no IR")
	}
	// The short-circuit branch should be in the chain wrapper.
	if !strings.Contains(ir, "__async_chain_") {
		t.Error("esperado __async_chain_ no IR")
	}
}

// TestAsyncPipeQuestShortCircuitBranch verifies that ~?> emits a conditional branch
// on the enum tag (chain_err / chain_ok blocks).
func TestAsyncPipeQuestShortCircuitBranch(t *testing.T) {
	src := `
fn tryParse(n: Int) -> Result[Int] = Ok(n)
fn inc(n: Int) -> Int = n + 1

fn main() {
  val x = 10
  val t = x ~> tryParse ~?> inc
  t.detach()
}
`
	ir := compileTask(t, src)
	// The short-circuit blocks should be present.
	if !strings.Contains(ir, "chain_err") {
		t.Error("esperado bloco chain_err no IR (short-circuit ~?>)")
	}
	if !strings.Contains(ir, "chain_ok") {
		t.Error("esperado bloco chain_ok no IR (short-circuit ~?>)")
	}
}

// TestAsyncPipeQuestMixed verifies ~> and ~?> can be mixed in a single chain.
func TestAsyncPipeQuestMixed(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn validate(n: Int) -> Result[Int] = Ok(n)
fn inc(n: Int) -> Int = n + 1

fn main() {
  val n = 3
  val t = n ~> double ~> validate ~?> inc
  t.detach()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__async_chain_") {
		t.Error("esperado __async_chain_ no IR para chain mista")
	}
}
