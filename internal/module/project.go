package module

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectConfig holds project-root discovery and package alias mappings.
type ProjectConfig struct {
	Root     string            // absolute path to project root (.soyuz-root or soyuz.toml dir)
	Packages map[string]string // alias name → relative path from root (e.g. "lexer" → "lib/lexer")
}

// FindProjectRoot walks up from startDir looking for .soyuz-root or soyuz.toml.
// Returns the directory containing the marker, or startDir if none found.
func FindProjectRoot(startDir string) (string, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, ".soyuz-root")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "soyuz.toml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs, nil
		}
		dir = parent
	}
}

// LoadProjectConfig discovers the project root from entryFile's directory and loads soyuz.toml packages.
func LoadProjectConfig(entryFile string) (*ProjectConfig, error) {
	entryDir := filepath.Dir(entryFile)
	root, err := FindProjectRoot(entryDir)
	if err != nil {
		return nil, err
	}
	cfg := &ProjectConfig{
		Root:     root,
		Packages: make(map[string]string),
	}
	tomlPath := filepath.Join(root, "soyuz.toml")
	if err := loadPackagesFromTOML(tomlPath, cfg.Packages); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadPackagesFromTOML parses only [packages] name = "path" entries from soyuz.toml.
func loadPackagesFromTOML(path string, out map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	inPackages := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[packages]" {
			inPackages = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inPackages = false
			continue
		}
		if !inPackages {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`)
		val = strings.Trim(val, "'")
		if key != "" && val != "" {
			out[key] = val
		}
	}
	return scanner.Err()
}

// ResolveAliasPath maps @alias/rest to filesystem segments relative to project root.
func (c *ProjectConfig) ResolveAliasPath(alias string, rest []string) ([]string, error) {
	base, ok := c.Packages[alias]
	if !ok {
		return nil, fmt.Errorf("pacote '%s' não definido em soyuz.toml [packages]", alias)
	}
	segs := strings.Split(base, "/")
	return append(segs, rest...), nil
}
