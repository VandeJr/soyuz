package codegen

import (
	"testing"
)

func TestDumpIRB5(t *testing.T) {
	src := `
fn dobrar(n: Int) -> Int = n * 2
fn quadrado(n: Int) -> Int = n * n
fn cubo(n: Int) -> Int = n * n * n
fn produtor(ch: Channel[Int]) { ch.send(10); ch.close() }
fn consumidor(ch: Channel[Int]) {
    val m1 = ch.recv()
    match m1 { Some(v) => print("recv") None => print("fechado") }
}
fn validar(n: Int) -> Result[Int] = Ok(n)
fn incrementarOk(n: Int) -> Int = n + 1

fn main() {
    val ch = Channel.new(8); val tp = task produtor(ch); val tc = task consumidor(ch)
    tp.await(); tc.await()
    val t9 = 5 ~> validar ~?> incrementarOk; t9.detach()
    val (tf1, tf2) = 4 |> Task.fan(quadrado, cubo)
    val rf1 = tf1.await(); val rf2 = tf2.await()
}
`
	ir := compileTask(t, src)
	t.Log(ir)
}
