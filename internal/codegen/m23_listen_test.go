package codegen

import (
	"strings"
	"testing"
)

// ── M-23: Task.listen(t: Task[T], ch: Channel[T]) -> Unit ─────────────────────

func TestTaskListenEmitsListenWrapper(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "__listen_wrapper_") {
		t.Error("expected __listen_wrapper_ function in IR")
	}
}

func TestTaskListenEmitsSrtEnqueue(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_enqueue") {
		t.Error("expected srt_enqueue in IR for Task.listen")
	}
}

func TestTaskListenEmitsSrtDetach(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_detach") {
		t.Error("expected srt_detach in IR — listener must be fire-and-forget")
	}
}

func TestTaskListenWrapperCallsSrtChanSend(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_send") {
		t.Error("expected srt_chan_send in IR — listener wrapper must forward to channel")
	}
}

func TestTaskListenWrapperCallsSrtAwait(t *testing.T) {
	src := `
fn work(x: Int) -> Int { x * 2 }

fn run(n: Int) {
  val t = task work(n)
  val ch = Channel.new(1)
  Task.listen(t, ch)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_await") {
		t.Error("expected srt_await in IR — listener wrapper must await source task")
	}
}
