# A convenience for working on exposer itself. Using exposer needs none of it:
# `exposer build <library>` is the whole pipeline (B-8), and the binary carries
# its theme, its schema and the Hugo version it renders with.

# The library to build. testdata/library is the one that ships with the tool.
LIBRARY ?= testdata/library
TARGET  ?= target
GO      ?= go
# The configuration lives in the library, at _data/exposer.yaml (R-18). Set
# CONFIG only to build the same library with a different one.
CONFIG  ?=
PORT    ?= 8888

CONFIG_FLAG := $(if $(CONFIG),--config $(CONFIG),)
BIN := $(TARGET)/bin/exposer

.PHONY: build serve check test lint clean

build: $(BIN)
	./$(BIN) build $(LIBRARY) --target $(TARGET) $(CONFIG_FLAG)

$(BIN): $(wildcard cmd/exposer/*.go) embed.go go.mod $(shell find site-gen schema -type f)
	@mkdir -p $(TARGET)/bin
	$(GO) build -o $(BIN) ./cmd/exposer

# B-7: preview the artifact locally. It builds first -- a preview of a stale
# artifact is the thing a preview exists to prevent -- and a warm build costs
# under a second.
serve: build
	./$(BIN) serve --target $(TARGET) --addr 127.0.0.1:$(PORT)

check: build
	./$(BIN) validate --document $(TARGET)/index.json

# The unit tests need nothing installed; the rest drive exiftool, ImageMagick
# and the pinned Hugo against testdata/, and skip -- naming what is missing --
# where a tool is absent.
test:
	$(GO) test ./...

# What CI checks before it runs anything: formatting and go vet.
lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }
	$(GO) vet ./...

clean:
	rm -rf $(TARGET)
