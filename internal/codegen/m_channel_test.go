package codegen

import (
	"strings"
	"testing"
)

// ── M-09: Channel[T] ─────────────────────────────────────────────────────────

func TestChannelNewEmitsSrtChanNew(t *testing.T) {
	src := `
fn main() {
  val ch = Channel.new(4)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_new") {
		t.Error("expected srt_chan_new in IR")
	}
}

func TestChannelSendEmitsSrtChanSend(t *testing.T) {
	src := `
fn sendInt(ch: Channel[Int]) {
  ch.send(42)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_send") {
		t.Error("expected srt_chan_send in IR")
	}
}

func TestChannelRecvEmitsSrtChanRecv(t *testing.T) {
	src := `
fn recvInt(ch: Channel[Int]) -> Option[Int] = ch.recv()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_recv") {
		t.Error("expected srt_chan_recv in IR")
	}
}

func TestChannelTryRecvEmitsSrtChanTryRecv(t *testing.T) {
	src := `
fn tryRecvInt(ch: Channel[Int]) -> Option[Int] = ch.tryRecv()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_try_recv") {
		t.Error("expected srt_chan_try_recv in IR")
	}
}

func TestChannelRecvWrapsOptionBranches(t *testing.T) {
	src := `
fn recvInt(ch: Channel[Int]) -> Option[Int] = ch.recv()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "chan_some") {
		t.Error("expected chan_some block in IR")
	}
	if !strings.Contains(ir, "chan_none") {
		t.Error("expected chan_none block in IR")
	}
}

func TestChannelCloseEmitsSrtChanClose(t *testing.T) {
	src := `
fn closeChannel(ch: Channel[Int]) {
  ch.close()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_close") {
		t.Error("expected srt_chan_close in IR")
	}
}

func TestChannelIsClosedEmitsSrtChanIsClosed(t *testing.T) {
	src := `
fn checkClosed(ch: Channel[Int]) -> Bool = ch.isClosed()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_is_closed") {
		t.Error("expected srt_chan_is_closed in IR")
	}
}

// Channel.new(0) = rendezvous (M-27: SyncChannel eliminado)

func TestChannelZeroCapacityEmitsSrtChanNew(t *testing.T) {
	src := `
fn main() {
  val sc = Channel.new(0)
  sc.close()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_new") {
		t.Error("expected srt_chan_new for Channel.new(0)")
	}
}

func TestChannelZeroCapacitySendEmitsSrtChanSend(t *testing.T) {
	src := `
fn sendRendezvous(ch: Channel[Int]) {
  ch.send(42)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_chan_send") {
		t.Error("expected srt_chan_send for Channel[T].send on rendezvous channel")
	}
}
