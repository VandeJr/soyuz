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

// Channel.new(0) = rendezvous (M-27: SyncChannel eliminado)

func TestChannelZeroCapacityIsRendezvous(t *testing.T) {
	src := `
fn main() {
  val sc = Channel.new(0)
  val _ = task fn() => sc.send(42)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Channel.new(0) não deve gerar erros: %v", result.Errors)
	}
}

func TestSyncChannelIsUndefined(t *testing.T) {
	src := `
fn main() {
  val sc = SyncChannel.new()
}
`
	result := checkSrc(src)
	if len(result.Errors) == 0 {
		t.Fatal("SyncChannel deve ser undefined após M-27")
	}
}
