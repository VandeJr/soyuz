PREFIX      ?= $(HOME)/.local/bin
NVIM_PARSER  = $(HOME)/.local/share/nvim/site/parser
NVIM_QUERIES = $(HOME)/.config/nvim/queries/soyuz
TS_DIR       = tree-sitter-soyuz

.PHONY: all build install uninstall clean ts-build ts-install

all: build

build:
	go build -o soyuz     ./cmd/
	go build -o soyuz-lsp ./cmd/lsp/

install: build
	install -Dm755 soyuz     $(PREFIX)/soyuz
	install -Dm755 soyuz-lsp $(PREFIX)/soyuz-lsp

uninstall:
	rm -f $(PREFIX)/soyuz $(PREFIX)/soyuz-lsp

clean:
	rm -f soyuz soyuz-lsp

ts-build:
	cd $(TS_DIR) && tree-sitter generate && tree-sitter build -o soyuz.so

ts-install: ts-build
	install -Dm755 $(TS_DIR)/soyuz.so $(NVIM_PARSER)/soyuz.so
	install -Dm644 $(TS_DIR)/queries/highlights.scm $(NVIM_QUERIES)/highlights.scm
	install -Dm644 $(TS_DIR)/queries/indents.scm    $(NVIM_QUERIES)/indents.scm
	install -Dm644 $(TS_DIR)/queries/folds.scm      $(NVIM_QUERIES)/folds.scm
	install -Dm644 $(TS_DIR)/queries/locals.scm     $(NVIM_QUERIES)/locals.scm
