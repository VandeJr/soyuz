PREFIX ?= $(HOME)/.local/bin

.PHONY: all build install uninstall clean

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
