package checker

import "testing"

// ── M-08: Mutex[T] ───────────────────────────────────────────────────────────

func TestMutexNewReturnsSpecializedType(t *testing.T) {
	src := `
fn main() {
  val m = Mutex.new(0)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Mutex.new(0) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestMutexLockReturnsMutexGuard(t *testing.T) {
	src := `
fn main() {
  val m = Mutex.new(0)
  val guard = m.lock()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("mutex.lock() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestMutexGuardValueType(t *testing.T) {
	src := `
fn getGuardValue(guard: MutexGuard[Int]) -> Int = guard.value
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("guard.value deve ter tipo Int, obtido erros: %v", result.Errors)
	}
}

func TestMutexGuardValueAssign(t *testing.T) {
	src := `
fn increment(m: Mutex[Int]) {
  var guard = m.lock()
  guard.value = guard.value + 1
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("guard.value = expr não deve gerar erros, obtido: %v", result.Errors)
	}
}

// ── M-08: RwLock[T] ──────────────────────────────────────────────────────────

func TestRwLockNewReturnsSpecializedType(t *testing.T) {
	src := `
fn main() {
  val rw = RwLock.new(0)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("RwLock.new(0) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestRwLockReadReturnsReadGuard(t *testing.T) {
	src := `
fn main() {
  val rw = RwLock.new(42)
  val rg = rw.read()
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("rwlock.read() não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestRwLockWriteReturnsWriteGuard(t *testing.T) {
	src := `
fn main() {
  val rw = RwLock.new(0)
  var wg = rw.write()
  wg.value = 99
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("rwlock.write() e guard.value = ... não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestReadGuardValueType(t *testing.T) {
	src := `
fn readValue(rg: ReadGuard[Int]) -> Int = rg.value
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("ReadGuard.value deve ter tipo Int, obtido: %v", result.Errors)
	}
}

// ── M-08: Atomic[T] ──────────────────────────────────────────────────────────

func TestAtomicNewReturnsSpecializedType(t *testing.T) {
	src := `
fn main() {
  val a = Atomic.new(0)
}
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Atomic.new(0) não deve gerar erros, obtido: %v", result.Errors)
	}
}

func TestAtomicLoadReturnsInnerType(t *testing.T) {
	src := `
fn readAtomic(a: Atomic[Int]) -> Int = a.load()
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Atomic.load() deve retornar Int, obtido: %v", result.Errors)
	}
}

func TestAtomicAddReturnsInnerType(t *testing.T) {
	src := `
fn addAtomic(a: Atomic[Int]) -> Int = a.add(1)
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Atomic.add(1) deve retornar Int, obtido: %v", result.Errors)
	}
}

func TestAtomicCasReturnsBool(t *testing.T) {
	src := `
fn casAtomic(a: Atomic[Int]) -> Bool = a.compareAndSwap(0, 1)
`
	result := checkSrc(src)
	if len(result.Errors) > 0 {
		t.Fatalf("Atomic.compareAndSwap deve retornar Bool, obtido: %v", result.Errors)
	}
}
