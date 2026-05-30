package module

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectMeta holds project-level metadata from the [project] section of soyuz.toml.
type ProjectMeta struct {
	Name    string // default: basename of project root directory
	Version string // default: "0.1.0"
	Type    string // "binary" | "library"; default "binary"
	Entry   string // entry file relative to root; default "main.sy"
}

// ProjectConfig holds project-root discovery, metadata and package alias mappings.
type ProjectConfig struct {
	Root     string            // absolute path to project root (.soyuz-root or soyuz.toml dir)
	Meta     ProjectMeta       // values from [project] section
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

// LoadProjectConfig discovers the project root from entryFile's directory and loads soyuz.toml.
func LoadProjectConfig(entryFile string) (*ProjectConfig, error) {
	entryDir := filepath.Dir(entryFile)
	root, err := FindProjectRoot(entryDir)
	if err != nil {
		return nil, err
	}
	cfg := &ProjectConfig{
		Root:     root,
		Packages: make(map[string]string),
		Meta: ProjectMeta{
			Name:    filepath.Base(root),
			Version: "0.1.0",
			Type:    "binary",
			Entry:   "main.sy",
		},
	}
	tomlPath := filepath.Join(root, "soyuz.toml")
	if err := loadFromTOML(tomlPath, cfg); err != nil {
		return nil, err
	}
	// Adjust default entry for library projects when not explicitly set.
	if cfg.Meta.Type == "library" && cfg.Meta.Entry == "main.sy" {
		cfg.Meta.Entry = "lib.sy"
	}
	return cfg, nil
}

// loadFromTOML parses [project] and [packages] sections from soyuz.toml into cfg.
func loadFromTOML(path string, cfg *ProjectConfig) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			section = strings.TrimSpace(section)
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
		if key == "" || val == "" {
			continue
		}
		switch section {
		case "project":
			switch key {
			case "name":
				cfg.Meta.Name = val
			case "version":
				cfg.Meta.Version = val
			case "type":
				cfg.Meta.Type = val
			case "entry":
				cfg.Meta.Entry = val
			}
		case "packages":
			cfg.Packages[key] = val
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
