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

func TestLoadPackagesFromTOML(t *testing.T) {
	root := t.TempDir()
	toml := filepath.Join(root, "soyuz.toml")
	content := `[packages]
lexer = "lib/lexer"
app = "src"
`
	if err := os.WriteFile(toml, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs := make(map[string]string)
	if err := loadPackagesFromTOML(toml, pkgs); err != nil {
		t.Fatal(err)
	}
	if pkgs["lexer"] != "lib/lexer" {
		t.Fatalf("lexer: %q", pkgs["lexer"])
	}
	if pkgs["app"] != "src" {
		t.Fatalf("app: %q", pkgs["app"])
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
