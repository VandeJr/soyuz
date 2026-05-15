package module_test

import (
	"os"
	"path/filepath"
	"testing"

	"soyuz/internal/module"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSingleFile verifica que um projeto sem imports retorna apenas o arquivo de entrada.
func TestSingleFile(t *testing.T) {
	dir := t.TempDir()
	entry := writeFile(t, dir, "main.soyuz", `fn hello() = "oi"`)

	resolver := module.NewResolver(entry)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(files) != 1 || files[0] != entry {
		t.Errorf("esperado [%s], obtido %v", entry, files)
	}
}

// TestSingleFileImport verifica import de módulo single-file.
func TestSingleFileImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "math.soyuz", `pub fn dobrar(x: Int) = x * 2`)
	entry := writeFile(t, dir, "main.soyuz", `import math.{ dobrar }
fn main() = dobrar(5)`)

	resolver := module.NewResolver(entry)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// math.soyuz deve vir antes de main.soyuz (ordem topológica)
	if len(files) != 2 {
		t.Fatalf("esperado 2 arquivos, obtido %d: %v", len(files), files)
	}
	if filepath.Base(files[0]) != "math.soyuz" {
		t.Errorf("esperado math.soyuz primeiro, obtido %s", filepath.Base(files[0]))
	}
	if filepath.Base(files[1]) != "main.soyuz" {
		t.Errorf("esperado main.soyuz por último, obtido %s", filepath.Base(files[1]))
	}
}

// TestDirectoryImport verifica import de módulo de diretório.
func TestDirectoryImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "math/ops.soyuz", `pub fn dobrar(x: Int) = x * 2`)
	writeFile(t, dir, "math/trig.soyuz", `pub fn cos(x: Float) = x`)
	entry := writeFile(t, dir, "main.soyuz", `import math.{ dobrar }
fn main() = dobrar(3)`)

	resolver := module.NewResolver(entry)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// math/ tem 2 arquivos + main.soyuz = 3
	if len(files) != 3 {
		t.Fatalf("esperado 3 arquivos, obtido %d: %v", len(files), files)
	}
	if filepath.Base(files[len(files)-1]) != "main.soyuz" {
		t.Errorf("esperado main.soyuz por último, obtido %s", filepath.Base(files[len(files)-1]))
	}
}

// TestTransitiveDependencies verifica que dependências transitivas são resolvidas.
func TestTransitiveDependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "base.soyuz", `pub fn id(x: Int) = x`)
	writeFile(t, dir, "mid.soyuz", `import base.{ id }
pub fn double(x: Int) = id(x) * 2`)
	entry := writeFile(t, dir, "main.soyuz", `import mid.{ double }
fn main() = double(4)`)

	resolver := module.NewResolver(entry)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("esperado 3 arquivos, obtido %d: %v", len(files), files)
	}
	// base → mid → main
	if filepath.Base(files[0]) != "base.soyuz" {
		t.Errorf("esperado base.soyuz primeiro, obtido %s", filepath.Base(files[0]))
	}
	if filepath.Base(files[2]) != "main.soyuz" {
		t.Errorf("esperado main.soyuz último, obtido %s", filepath.Base(files[2]))
	}
}

// TestCycleDetection verifica que ciclos de import geram erro.
func TestCycleDetection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.soyuz", `import b.{ foo }`)
	writeFile(t, dir, "b.soyuz", `import a.{ bar }`)
	entry := writeFile(t, dir, "a.soyuz", `import b.{ foo }`)

	resolver := module.NewResolver(entry)
	_, err := module.Collect(entry, resolver)
	if err == nil {
		t.Fatal("esperado erro de ciclo, mas não houve erro")
	}
}

// TestUnresolvedImport verifica que imports não encontrados retornam erro.
func TestUnresolvedImport(t *testing.T) {
	dir := t.TempDir()
	entry := writeFile(t, dir, "main.soyuz", `import inexistente.{ foo }
fn main() = foo()`)

	resolver := module.NewResolver(entry)
	_, err := module.Collect(entry, resolver)
	if err == nil {
		t.Fatal("esperado erro de import não resolvido, mas não houve erro")
	}
}

// TestStdlibImport verifica que @soyuz/mock resolve a partir do StdlibDir.
func TestStdlibImport(t *testing.T) {
	dir := t.TempDir()
	stdlibDir := t.TempDir()

	// Escrever um arquivo stdlib fake no stdlibDir
	writeFile(t, stdlibDir, "mock.soyuz", `pub fn assert_eq(a: Int, b: Int, name: String) {}`)

	// Arquivo principal importa via @soyuz.mock
	entry := writeFile(t, dir, "main.soyuz", `import @soyuz.mock.{assert_eq}
fn main() { assert_eq(1, 1, "ok") }`)

	resolver := module.NewResolverWithStdlib(entry, stdlibDir)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// Deve ter resolvido: mock.soyuz + main.soyuz
	if len(files) != 2 {
		t.Errorf("esperado 2 arquivos, obtido %d: %v", len(files), files)
	}
}

// TestStdlibImportSemStdlibDir verifica erro quando stdlib não está configurada.
func TestStdlibImportSemStdlibDir(t *testing.T) {
	dir := t.TempDir()
	entry := writeFile(t, dir, "main.soyuz", `import @soyuz/mock.{assert_eq}`)

	resolver := module.NewResolver(entry) // sem StdlibDir
	_, err := module.Collect(entry, resolver)
	if err == nil {
		t.Fatal("esperado erro, mas resolveu sem stdlib configurada")
	}
}
