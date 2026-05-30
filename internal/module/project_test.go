package module

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, ".soyuz-root")
	if err := os.WriteFile(marker, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	if found != root {
		t.Fatalf("esperado root %q, obteve %q", root, found)
	}
}

func TestLoadFromTOML(t *testing.T) {
	root := t.TempDir()
	toml := filepath.Join(root, "soyuz.toml")
	content := `[project]
name = "meu-app"
version = "1.2.3"
type = "binary"
entry = "src/main.sy"

[packages]
lexer = "lib/lexer"
app = "src"
`
	if err := os.WriteFile(toml, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &ProjectConfig{
		Packages: make(map[string]string),
		Meta:     ProjectMeta{Name: "default", Version: "0.1.0", Type: "binary", Entry: "main.sy"},
	}
	if err := loadFromTOML(toml, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Meta.Name != "meu-app" {
		t.Fatalf("name: %q", cfg.Meta.Name)
	}
	if cfg.Meta.Version != "1.2.3" {
		t.Fatalf("version: %q", cfg.Meta.Version)
	}
	if cfg.Meta.Entry != "src/main.sy" {
		t.Fatalf("entry: %q", cfg.Meta.Entry)
	}
	if cfg.Packages["lexer"] != "lib/lexer" {
		t.Fatalf("lexer: %q", cfg.Packages["lexer"])
	}
	if cfg.Packages["app"] != "src" {
		t.Fatalf("app: %q", cfg.Packages["app"])
	}
}

func TestResolveAliasPath(t *testing.T) {
	cfg := &ProjectConfig{
		Packages: map[string]string{"lexer": "lib/lexer"},
	}
	segs, err := cfg.ResolveAliasPath("lexer", []string{"tokens"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lib", "lexer", "tokens"}
	if len(segs) != len(want) {
		t.Fatalf("segs %v", segs)
	}
	for i := range want {
		if segs[i] != want[i] {
			t.Fatalf("segs[%d] = %q, want %q", i, segs[i], want[i])
		}
	}
}
