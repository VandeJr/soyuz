package codegen

import (
	"strings"
	"testing"
)

// ── M-20: select { ch.recv() => body } ───────────────────────────────────────

func TestSelectEmitsSrtSelect(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    msg = ch.recv() => print(msg)
  }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_select") {
		t.Error("expected srt_select in IR")
	}
}

func TestSelectWithDefaultEmitsSrtSelectTry(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    msg = ch.recv() => print(msg)
    default         => print("nada")
  }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_select_try") {
		t.Error("expected srt_select_try in IR for select with default arm")
	}
}

func TestSelectEmitsArmBlocks(t *testing.T) {
	src := `
fn doSelect(chA: Channel[Int], chB: Channel[Int]) {
  select {
    a = chA.recv() => print(a)
    b = chB.recv() => print(b)
  }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "sel_arm0") {
		t.Error("expected sel_arm0 block in IR")
	}
	if !strings.Contains(ir, "sel_arm1") {
		t.Error("expected sel_arm1 block in IR")
	}
}

func TestSelectEmitsMergeBlock(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    msg = ch.recv() => print(msg)
  }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "sel_merge") {
		t.Error("expected sel_merge block in IR")
	}
}

func TestSelectDefaultEmitsDefaultBlock(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    msg = ch.recv() => print(msg)
    default         => print("default")
  }
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "sel_default") {
		t.Error("expected sel_default block in IR")
	}
}
