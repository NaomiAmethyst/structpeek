# SPDX-License-Identifier: GPL-3.0-or-later
# Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

PREFIX  ?= /usr/local
BINDIR  ?= $(PREFIX)/bin
DESTDIR ?=

GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

SOURCES := $(shell find cmd internal -name '*.go') go.mod go.sum

.PHONY: all test check fuzz install uninstall clean

all: structpeek

structpeek: $(SOURCES)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ ./cmd/structpeek

test:
	$(GO) test ./...

check: test
	$(GO) vet ./...
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo 'run gofmt -w .'; exit 1; }

FUZZTIME ?= 60s
fuzz:
	$(GO) test -run '^$$' -fuzz FuzzDecode -fuzztime $(FUZZTIME) ./internal/decode
	$(GO) test -run '^$$' -fuzz FuzzInfer -fuzztime $(FUZZTIME) ./internal/infer

install: structpeek
	install -Dm755 structpeek $(DESTDIR)$(BINDIR)/structpeek

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/structpeek

clean:
	rm -rf structpeek dist
