package stdlib

import (
	"embed"
	"io/fs"
	"strings"
)

// FS embeds everything under std/lib/ recursively.
// Qualquer arquivo .sy adicionado em std/lib/ (inclusive subpastas) é automaticamente incluído.
//
//go:embed lib
var FS embed.FS

// Files maps relative module path → file contents.
// Keys use the path relative to lib/, e.g. "mock.sy", "collections/list.sy".
var Files = map[string][]byte{}

func init() {
	fs.WalkDir(FS, "lib", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".sy") {
			return nil
		}
		// path = "lib/mock.sy" → key = "mock.sy"
		// path = "lib/collections/list.sy" → key = "collections/list.sy"
		relPath := strings.TrimPrefix(path, "lib/")
		data, _ := FS.ReadFile(path)
		Files[relPath] = data
		return nil
	})
}
