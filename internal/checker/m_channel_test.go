package checker

import "testing"

// ── M-09: Channel[T] ─────────────────────────────────────────────────────────

func TestChannelNewReturnsSpecializedType(t *testing.T) {
	src := `
fn main() {
  val ch = Channel.new(4)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Channel.new(4) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestChannelSendAcceptsValue(t *testing.T) {
	src := `
fn sendInt(ch: Channel[Int]) {
  ch.send(42)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("ch.send(42) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestChannelRecvReturnsOptionT(t *testing.T) {
	src := `
fn recvInt(ch: Channel[Int]) -> Option[Int] = ch.recv()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("ch.recv() deve retornar Option[Int], obtido erros: %v", result.Errors)
	}
}

func TestChannelTryRecvReturnsOptionT(t *testing.T) {
	src := `
fn tryRecvInt(ch: Channel[Int]) -> Option[Int] = ch.tryRecv()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("ch.tryRecv() deve retornar Option[Int], obtido erros: %v", result.Errors)
	}
}

func TestChannelCloseIsUnit(t *testing.T) {
	src := `
fn closeChannel(ch: Channel[Int]) {
  ch.close()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("ch.close() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestChannelIsClosedReturnsBool(t *testing.T) {
	src := `
fn checkClosed(ch: Channel[Int]) -> Bool = ch.isClosed()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("ch.isClosed() deve retornar Bool, obtido: %v", result.Errors)
	}
}

// ── M-09: SyncChannel[T] ─────────────────────────────────────────────────────

func TestSyncChannelNewReturnsSpecializedType(t *testing.T) {
	src := `
fn main() {
  val sc = SyncChannel.new()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("SyncChannel.new() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestSyncChannelSendAcceptsValue(t *testing.T) {
	src := `
fn sendSync(sc: SyncChannel[Int]) {
  sc.send(99)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("sc.send(99) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestSyncChannelRecvReturnsOptionT(t *testing.T) {
	src := `
fn recvSync(sc: SyncChannel[Int]) -> Option[Int] = sc.recv()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("sc.recv() deve retornar Option[Int], obtido: %v", result.Errors)
	}
}

func TestSyncChannelCloseIsUnit(t *testing.T) {
	src := `
fn closeSync(sc: SyncChannel[Int]) {
  sc.close()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("sc.close() não deve gerar erros, obtido: %v", result.Errors)
	}
}
