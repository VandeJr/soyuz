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
	entry := writeFile(t, dir, "main.sy", `fn hello() = "oi"`)

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
	writeFile(t, dir, "math.sy", `pub fn dobrar(x: Int) = x * 2`)
	entry := writeFile(t, dir, "main.sy", `import math.{ dobrar }
fn main() = dobrar(5)`)

	resolver := module.NewResolver(entry)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// math.sy deve vir antes de main.sy (ordem topológica)
	if len(files) != 2 {
		t.Fatalf("esperado 2 arquivos, obtido %d: %v", len(files), files)
	}
	if filepath.Base(files[0]) != "math.sy" {
		t.Errorf("esperado math.sy primeiro, obtido %s", filepath.Base(files[0]))
	}
	if filepath.Base(files[1]) != "main.sy" {
		t.Errorf("esperado main.sy por último, obtido %s", filepath.Base(files[1]))
	}
}

// TestDirectoryImport verifica import de módulo de diretório.
func TestDirectoryImport(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "math/ops.sy", `pub fn dobrar(x: Int) = x * 2`)
	writeFile(t, dir, "math/trig.sy", `pub fn cos(x: Float) = x`)
	entry := writeFile(t, dir, "main.sy", `import math.{ dobrar }
fn main() = dobrar(3)`)

	resolver := module.NewResolver(entry)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// math/ tem 2 arquivos + main.sy = 3
	if len(files) != 3 {
		t.Fatalf("esperado 3 arquivos, obtido %d: %v", len(files), files)
	}
	if filepath.Base(files[len(files)-1]) != "main.sy" {
		t.Errorf("esperado main.sy por último, obtido %s", filepath.Base(files[len(files)-1]))
	}
}

// TestTransitiveDependencies verifica que dependências transitivas são resolvidas.
func TestTransitiveDependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "base.sy", `pub fn id(x: Int) = x`)
	writeFile(t, dir, "mid.sy", `import base.{ id }
pub fn double(x: Int) = id(x) * 2`)
	entry := writeFile(t, dir, "main.sy", `import mid.{ double }
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
	if filepath.Base(files[0]) != "base.sy" {
		t.Errorf("esperado base.sy primeiro, obtido %s", filepath.Base(files[0]))
	}
	if filepath.Base(files[2]) != "main.sy" {
		t.Errorf("esperado main.sy último, obtido %s", filepath.Base(files[2]))
	}
}

// TestCycleDetection verifica que ciclos de import geram erro.
func TestCycleDetection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.sy", `import b.{ foo }`)
	writeFile(t, dir, "b.sy", `import a.{ bar }`)
	entry := writeFile(t, dir, "a.sy", `import b.{ foo }`)

	resolver := module.NewResolver(entry)
	_, err := module.Collect(entry, resolver)
	if err == nil {
		t.Fatal("esperado erro de ciclo, mas não houve erro")
	}
}

// TestUnresolvedImport verifica que imports não encontrados retornam erro.
func TestUnresolvedImport(t *testing.T) {
	dir := t.TempDir()
	entry := writeFile(t, dir, "main.sy", `import inexistente.{ foo }
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
	writeFile(t, stdlibDir, "mock.sy", `pub fn assert_eq(a: Int, b: Int, name: String) {}`)

	// Arquivo principal importa via @soyuz.mock
	entry := writeFile(t, dir, "main.sy", `import ( { assert_eq } from "@soyuz/mock" )
fn main() { assert_eq(1, 1, "ok") }`)

	resolver := module.NewResolverWithStdlib(entry, stdlibDir)
	files, err := module.Collect(entry, resolver)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	// Deve ter resolvido: mock.sy + main.sy
	if len(files) != 2 {
		t.Errorf("esperado 2 arquivos, obtido %d: %v", len(files), files)
	}
}

// TestStdlibImportSemStdlibDir verifica erro quando stdlib não está configurada.
func TestStdlibImportSemStdlibDir(t *testing.T) {
	dir := t.TempDir()
	entry := writeFile(t, dir, "main.sy", `import ( { assert_eq } from "@soyuz/mock" )`)

	resolver := module.NewResolver(entry) // sem StdlibDir
	_, err := module.Collect(entry, resolver)
	if err == nil {
		t.Fatal("esperado erro, mas resolveu sem stdlib configurada")
	}
}
