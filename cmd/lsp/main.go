package main

import (
	"soyuz/internal/lsp"

	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	glspserver "github.com/tliron/glsp/server"
)

func main() {
	// Nível -1 = silencia todo output para stderr.
	// Neovim captura stderr de processos LSP e loga como ERROR — não queremos ruído.
	commonlog.Configure(-1, nil)

	handler := lsp.NewHandler()
	server := glspserver.NewServer(handler, "soyuz-lsp", false)
	server.RunStdio()
}
