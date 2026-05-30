# Soyuz

A compiled, statically typed programming language with LLVM as its backend. Soyuz combines functional and object-oriented features — pattern matching, generics, immutability by default, `Result`/`Option` error handling, and structured concurrency — while compiling to native binaries via LLVM IR + Clang.

```
fn main() {
    val nums = [1, 2, 3, 4, 5]
    val pares = nums.filter(fn(n) => n % 2 == 0)
    for n in pares {
        print("par: $(n)")
    }
}
```

## Requirements

- **Go 1.21+** — to build the compiler
- **Clang** — to link the generated LLVM IR into a native binary

```sh
# Ubuntu / Debian
sudo apt install clang

# Arch Linux
sudo pacman -S clang

# macOS
brew install llvm
```

## Installation

### Pre-built binary (Linux x64)

Download the latest `soyuz-linux-x64` from the [Releases](../../releases) page, then:

```sh
chmod +x soyuz-linux-x64
sudo mv soyuz-linux-x64 /usr/local/bin/soyuz
```

### Build from source

```sh
git clone https://github.com/VandeJr/soyuz
cd soyuz
make build        # produces ./soyuz and ./soyuz-lsp
make install      # installs to ~/.local/bin by default
```

## Quick Start

```sh
# Create a new project
soyuz new hello
cd hello

# Run it directly
soyuz run

# Or build first, then execute
soyuz build
./target/debug/hello

# Optimized release build
soyuz build --release
./target/release/hello
```

### Commands

| Command | Description |
|---|---|
| `soyuz new <name>` | Create a binary project |
| `soyuz new --lib <name>` | Create a library project |
| `soyuz build` | Build using `soyuz.toml` → `target/debug/<name>` |
| `soyuz build --release` | Optimized build → `target/release/<name>` |
| `soyuz build <file.sy>` | Compile a single file (legacy mode) |
| `soyuz run` | Build and run the project |
| `soyuz run <file.sy>` | Build and run a single file |
| `soyuz run -- arg1 arg2` | Pass arguments to the program |

## Project Layout

```
my-app/
  soyuz.toml    # project manifest
  main.sy       # entry point
  lib/          # local packages
    utils/
      utils.sy
```

**soyuz.toml**

```toml
[project]
name    = "my-app"
version = "0.1.0"
type    = "binary"   # or "library"
entry   = "main.sy"

[packages]
utils = "lib/utils"  # @utils/... maps to lib/utils/
```

## Language Tour

### Variables

```soyuz
val x = 42          // immutable
var y = 0           // mutable
const PI = 3.14     // compile-time constant
```

### Functions

```soyuz
fn soma(a: Int, b: Int) -> Int = a + b

// Expression body (single-expression functions)
fn quadrado(n: Int) -> Int = n * n

// Block body
fn saudar(nome: String, saudacao: String = "Olá") {
    print("$(saudacao), $(nome)!")
}

// Generics
fn identidade[T](valor: T) -> T = valor
```

### Function overloading & when guards

```soyuz
fn processar(n: Int) when n > 0 {
    print("positivo: $(n)")
}

fn processar(n: Int) {
    print("zero ou negativo: $(n)")
}
```

### Records, Enums & Classes

```soyuz
// Records — immutable value types
pub record Ponto {
    x: Float,
    y: Float
}

// Enums — algebraic data types
pub enum Forma {
    Circulo(Float),
    Retangulo(Ponto),
    Invisivel
}

// Classes — reference types with methods
pub class Usuario {
    pub val nome: String
    val _id: Int

    pub fn id(self) -> Int = self._id
}
```

### Interfaces

```soyuz
pub interface Identificavel {
    pub fn id() -> Int
}

pub class Produto : Identificavel {
    val _id: Int
    pub fn id(self) -> Int = self._id
}
```

### extend — add methods to existing types

```soyuz
extend String {
    pub fn exclamar(self) -> String = "$(self)!"
    pub fn repetir(self, n: Int) -> String { ... }
}

print("Soyuz".exclamar())   // "Soyuz!"
```

### Pattern Matching

```soyuz
fn area(f: Forma) -> Float = match f {
    Circulo(r)   => 3.14 * r * r
    Retangulo(p) => p.x * p.y
    Invisivel    => 0.0
}

// Match with guards
fn classificar(n: Int) -> String = match n {
    x when x > 0 => "positivo"
    x when x < 0 => "negativo"
    _            => "zero"
}
```

### Error Handling — Result & Option

```soyuz
fn dividir(a: Int, b: Int) -> Result[Int] {
    if b == 0 {
        return Err(noneError("divisão por zero"))
    }
    return Ok(a / b)
}

// Option shorthand: T?
fn buscar(id: Int) -> String? {
    if id == 1 { return Some("Alice") }
    return None
}

// Pattern match on results
match dividir(10, 2) {
    Ok(v)  => print("resultado: $(v)")
    Err(e) => print("erro: $(e.message())")
}

// Safe navigation and Elvis operator
val nome = buscar(id)?.toUpper() ?: "ANON"
```

### Pipes

```soyuz
// |> synchronous pipe
val r = 5 |> dobrar |> incrementar    // incrementar(dobrar(5))

// |?> pipe-quest: short-circuits on None/Err
val resultado = buscar(1) |?> validarNome |?> exclamar

// Lambda expressions
val dobro = fn(x: Int) => x * 2
val nums  = [1, 2, 3].map(fn(n) => n * n)
```

### Collections

```soyuz
val lista: List[Int]       = [1, 2, 3, 4, 5]
val mapa: Map[String, Int] = ["a": 1, "b": 2]

// Iteration
for n in lista { print(n) }
for k in mapa  { print(k) }
for i in 0..5  { print(i) }   // exclusive range
for i in 1..=5 { print(i) }   // inclusive range

// HOF methods
val pares   = lista.filter(fn(n) => n % 2 == 0)
val dobros  = lista.map(fn(n) => n * 2)
val soma    = lista.reduce(0, fn(acc, n) => acc + n)
```

### Concurrency

Soyuz uses a work-stealing M:N scheduler with cooperative coroutines.

```soyuz
fn calcular(n: Int) -> Int = n * 2

fn main() {
    // Spawn a task
    val t = task calcular(21)
    val r = t.await()          // r == 42

    // Detach (fire-and-forget)
    task calcular(99).detach()

    // Await multiple tasks
    val ta = task calcular(5)
    val tb = task calcular(10)
    val (a, b) = Task.all(ta, tb)    // a=10, b=20

    // Fan-out: same input, different functions
    val (sq, cb) = 4 |> Task.fan(quadrado, cubo)

    // Parallel map over a list
    val results = Task.gather([1, 2, 3], calcular)
}
```

**Channels**

```soyuz
val ch = Channel.new(8)        // buffered channel
val sync = Channel.new(0)      // rendezvous (unbuffered)

val t = task {
    ch.send(42)
    ch.close()
}

match ch.recv() {
    Some(v) => print("received: $(v)")
    None    => print("channel closed")
}
t.await()
```

**select**

```soyuz
select {
    msg = chA.recv() => print("from A: $(msg)")
    msg = chB.recv() => print("from B: $(msg)")
    default          => print("nothing ready")
}
```

**Async pipes**

```soyuz
val t = n ~> dobrar ~> triplicar   // ~> spawns each step as a task
t.await()

// ~?> short-circuits on Err
val t2 = n ~> validar ~?> processar
```

**Callbacks**

```soyuz
val t = task calcular(10)
    .tap(fn(r)   => print("side-effect: $(r)"))   // observe without changing result
    .always(fn() => print("cleanup"))              // always runs
val r = t.await()
```

### Synchronization

```soyuz
val mutex = Mutex.new(0)

fn incrementar(m: Mutex[Int]) {
    var guard = m.lock()
    guard.value = guard.value + 1
}   // guard dropped here → mutex released

val atom = Atomic.new(0)
atom.add(1)
val trocou = atom.compareAndSwap(1, 100)
```

### FFI — calling C from Soyuz

```soyuz
extern fn strlen(s: String) -> Int

fn main() {
    print("len: $(strlen("hello"))")
}
```

Place any `.c` files in a `runtime/` directory at the project root — they will be compiled and linked automatically.

## Standard Library

| Module | Import | Contents |
|---|---|---|
| Prelude | `@soyuz/prelude` | `print`, `range`, `args`, `noneError` — auto-imported |
| String | `@soyuz/string` | `len`, `substring`, `trim`, `toUpper`, `toLower`, `contains`, `replace`, ... |
| Collections | `@soyuz/collections` | `List[T]`, `Map[K,V]` with `map`, `filter`, `reduce`, `join` |
| FS | `@soyuz/fs` | `readFile`, `writeFile`, `exists`, `isDir` — returns `Result[T]` |
| OS | `@soyuz/os` | `getenv`, `hasEnv`, `args`, `exec` |
| Async | `@soyuz/async` | `Task[T]`, `Channel[T]`, `TaskHandle` |
| Sync | `@soyuz/sync` | `Mutex[T]`, `RwLock[T]`, `Atomic[T]` |
| Arc | `@soyuz/arc` | `Arc[T]` — atomic reference counting with EBR |

## Multi-file Projects

```soyuz
// lib/math/math.sy
pub fn soma(a: Int, b: Int) -> Int = a + b

// main.sy
import { soma } from @math/math

fn main() {
    print(soma(1, 2))
}
```

Define aliases in `soyuz.toml`:

```toml
[packages]
math = "lib/math"
```

## Language Server (LSP)

`soyuz-lsp` provides diagnostics, hover types, completions, inlay hints, and formatting for any LSP-compatible editor.

```sh
# The binary is published alongside soyuz in each release.
# VS Code / Neovim — point your LSP client to the soyuz-lsp binary.
```

## Architecture

The compiler is written in Go and follows a classic pipeline:

```
Source (.sy)
  → Lexer     (internal/lexer)
  → Parser    (internal/parser)     — Pratt + recursive-descent
  → Resolver  (internal/module)     — topological import order
  → Checker   (internal/checker)    — 4-pass type system
  → Codegen   (internal/codegen)    — LLVM IR via github.com/llir/llvm
  → Clang     — links LLVM IR + embedded C runtime → native binary
```

The C runtime (`internal/runtime/src/`) handles reference counting, the async task scheduler (work-stealing M:N with cooperative coroutines via `ucontext`), channels, synchronization primitives, and the standard library.

## License

MIT
