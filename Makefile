BINARY := ght
GO ?= go
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: build install uninstall clean fmt vet test

build:
	$(GO) build -o $(BINARY) main.go

install: build
	install -m 0755 $(BINARY) $(BINDIR)/$(BINARY)

uninstall:
	rm -f $(BINDIR)/$(BINARY)

clean:
	rm -f $(BINARY)

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...
