package codegen

import (
	"strings"
	"testing"
)

// ── M-08: Mutex[T] ───────────────────────────────────────────────────────────

func TestMutexNewEmitsSrtMutexNew(t *testing.T) {
	src := `
fn main() {
  val a = Atomic.new(0)
  a.store(1)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_atomic_new") {
		t.Error("expected srt_atomic_new in IR")
	}
}

func TestMutexNewAndLockEmitsCalls(t *testing.T) {
	src := `
fn main() {
  val m = Mutex.new(42)
  val guard = m.lock()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_mutex_new") {
		t.Error("expected srt_mutex_new in IR")
	}
	if !strings.Contains(ir, "srt_mutex_lock") {
		t.Error("expected srt_mutex_lock in IR")
	}
}

func TestMutexGuardUnlockedOnScopeExit(t *testing.T) {
	src := `
fn main() {
  val m = Mutex.new(0)
  val guard = m.lock()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_mutex_unlock") {
		t.Error("expected srt_mutex_unlock called at scope exit for MutexGuard")
	}
}

func TestMutexGuardValueRead(t *testing.T) {
	src := `
fn readMutex(m: Mutex[Int]) -> Int {
  val guard = m.lock()
  return guard.value
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_mutex_guard_get") {
		t.Error("expected srt_mutex_guard_get for guard.value read")
	}
}

func TestMutexGuardValueWrite(t *testing.T) {
	src := `
fn increment(m: Mutex[Int]) {
  var guard = m.lock()
  guard.value = guard.value + 1
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_mutex_guard_get") {
		t.Error("expected srt_mutex_guard_get for guard.value read in += 1")
	}
	if !strings.Contains(ir, "srt_mutex_guard_set") {
		t.Error("expected srt_mutex_guard_set for guard.value write in += 1")
	}
}

// ── M-08: RwLock[T] ──────────────────────────────────────────────────────────

func TestRwLockReadEmitsSrtRwlockRead(t *testing.T) {
	src := `
fn main() {
  val rw = RwLock.new(0)
  val rg = rw.read()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_rwlock_new") {
		t.Error("expected srt_rwlock_new in IR")
	}
	if !strings.Contains(ir, "srt_rwlock_read") {
		t.Error("expected srt_rwlock_read in IR")
	}
}

func TestRwLockWriteGuardUnlockedOnScopeExit(t *testing.T) {
	src := `
fn main() {
  val rw = RwLock.new(0)
  val wg = rw.write()
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_rwlock_unlock") {
		t.Error("expected srt_rwlock_unlock called at scope exit for WriteGuard")
	}
}

// ── M-08: Atomic[T] ──────────────────────────────────────────────────────────

func TestAtomicNewEmitsSrtAtomicNew(t *testing.T) {
	src := `
fn main() {
  val a = Atomic.new(0)
  a.store(1)
}
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_atomic_new") {
		t.Error("expected srt_atomic_new in IR")
	}
	if !strings.Contains(ir, "srt_atomic_store") {
		t.Error("expected srt_atomic_store in IR")
	}
}

func TestAtomicLoadEmitsSrtAtomicLoad(t *testing.T) {
	src := `
fn loadAtomic(a: Atomic[Int]) -> Int = a.load()
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_atomic_load") {
		t.Error("expected srt_atomic_load in IR")
	}
}

func TestAtomicAddEmitsSrtAtomicAdd(t *testing.T) {
	src := `
fn addToAtomic(a: Atomic[Int]) -> Int = a.add(1)
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_atomic_add") {
		t.Error("expected srt_atomic_add in IR")
	}
}

func TestAtomicCasEmitsSrtAtomicCas(t *testing.T) {
	src := `
fn casAtomic(a: Atomic[Int]) -> Bool = a.compareAndSwap(0, 1)
`
	ir := compileTask(t, src)
	if !strings.Contains(ir, "srt_atomic_cas") {
		t.Error("expected srt_atomic_cas in IR")
	}
}
