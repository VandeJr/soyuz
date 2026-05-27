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

// ── M-09: SyncChannel[T] ─────────────────────────────────────────────────────

func TestSyncChannelNewEmitsSrtSyncChanNew(t *testing.T) {
	src := `
fn main() {
  val sc = SyncChannel.new()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_sync_chan_new") {
		t.Error("expected srt_sync_chan_new in IR")
	}
}

func TestSyncChannelSendEmitsSrtSyncChanSend(t *testing.T) {
	src := `
fn sendSync(sc: SyncChannel[Int]) {
  sc.send(99)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_sync_chan_send") {
		t.Error("expected srt_sync_chan_send in IR")
	}
}

func TestSyncChannelRecvEmitsSrtSyncChanRecv(t *testing.T) {
	src := `
fn recvSync(sc: SyncChannel[Int]) -> Option[Int] = sc.recv()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_sync_chan_recv") {
		t.Error("expected srt_sync_chan_recv in IR")
	}
}

func TestSyncChannelCloseEmitsSrtSyncChanClose(t *testing.T) {
	src := `
fn closeSync(sc: SyncChannel[Int]) {
  sc.close()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_sync_chan_close") {
		t.Error("expected srt_sync_chan_close in IR")
	}
}
