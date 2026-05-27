package checker

import "testing"

// ── M-20: select { ch.recv() => body } ───────────────────────────────────────

func TestSelectRecvArmTypeChecks(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    msg = ch.recv() => print(msg)
  }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("select recv arm não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestSelectDefaultArmAllowed(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    msg = ch.recv() => print(msg)
    default         => print("nada")
  }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("select com default não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestSelectMultipleRecvArms(t *testing.T) {
	src := `
fn doSelect(chA: Channel[Int], chB: Channel[Int]) {
  select {
    a = chA.recv() => print(a)
    b = chB.recv() => print(b)
  }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("select com múltiplos arms não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestSelectArmWithoutBinding(t *testing.T) {
	src := `
fn doSelect(ch: Channel[Int]) {
  select {
    ch.recv() => print("recebido")
  }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("select arm sem binding não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestSelectOnlyDefault(t *testing.T) {
	src := `
fn doDefault() {
  select {
    default => print("sem canais")
  }
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("select só com default não deve gerar erros, obtido: %v", result.Errors)
	}
}
