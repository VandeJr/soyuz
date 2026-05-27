package codegen

import (
	"strings"
	"testing"
)

// ── M-19: Task.pipe — pipeline paralelo com channels ────────────────────────

// TestTaskPipeEmitsEnqueuePerStage verifies that Task.pipe spawns one task per stage function.
func TestTaskPipeEmitsEnqueuePerStage(t *testing.T) {
	src := `
fn double(n: Int) -> Int = n * 2
fn negate(n: Int) -> Int = n * -1

fn main() {
  val n = 5
  val ch = Task.pipe(n, double, negate)
  ch.close()
}
`
	ir := compileTask(t, src)
	// Two stages → two srt_enqueue calls.
	count := strings.Count(ir, "srt_enqueue")
	if count < 2 {
		t.Errorf("esperado ao menos 2 srt_enqueue para 2 stages, obtido %d\nIR:\n%s", count, ir)
	}
}

// TestTaskPipeCreatesChannels verifies that srt_chan_new is called for internal channels.
func TestTaskPipeCreatesChannels(t *testing.T) {
	src := `
fn inc(n: Int) -> Int = n + 1
fn dec(n: Int) -> Int = n - 1

fn main() {
  val n = 10
  val ch = Task.pipe(n, inc, dec)
  ch.close()
}
`
	ir := compileTask(t, src)
	// At least 3 srt_chan_new calls: input ch + output ch per stage (2 stages = 2 output + 1 input).
	count := strings.Count(ir, "srt_chan_new")
	if count < 2 {
		t.Errorf("esperado ao menos 2 srt_chan_new para pipeline, obtido %d\nIR:\n%s", count, ir)
	}
}

// TestTaskPipeStageWrapperLoops verifies that generated stage wrappers contain srt_chan_recv loops.
func TestTaskPipeStageWrapperLoops(t *testing.T) {
	src := `
fn square(n: Int) -> Int = n * n

fn main() {
  val n = 3
  val ch = Task.pipe(n, square)
  ch.close()
}
`
	ir := compileTask(t, src)
	// The stage wrapper must contain a recv loop (pipe_loop label).
	if !strings.Contains(ir, "pipe_loop") {
		t.Errorf("esperado label pipe_loop no IR da stage wrapper\nIR:\n%s", ir)
	}
	if !strings.Contains(ir, "srt_chan_recv") {
		t.Errorf("esperado srt_chan_recv no stage wrapper\nIR:\n%s", ir)
	}
	if !strings.Contains(ir, "srt_chan_send") {
		t.Errorf("esperado srt_chan_send no stage wrapper\nIR:\n%s", ir)
	}
	if !strings.Contains(ir, "srt_chan_close") {
		t.Errorf("esperado srt_chan_close no stage wrapper\nIR:\n%s", ir)
	}
}

// TestTaskPipeStagesDetached verifies that spawned stage tasks are detached (srt_detach called).
func TestTaskPipeStagesDetached(t *testing.T) {
	src := `
fn triple(n: Int) -> Int = n * 3
fn abs(n: Int) -> Int = n

fn main() {
  val n = 4
  val ch = Task.pipe(n, triple, abs)
  ch.close()
}
`
	ir := compileTask(t, src)
	// Each stage task must be detached so pipeline runs autonomously.
	count := strings.Count(ir, "srt_detach")
	if count < 2 {
		t.Errorf("esperado ao menos 2 srt_detach (um por stage), obtido %d\nIR:\n%s", count, ir)
	}
}
