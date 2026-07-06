# Nitpick build tooling.
#
# Override GO or INSTALL_DIR from the command line if needed, e.g.:
#   make install INSTALL_DIR=/usr/local/bin

GO          ?= go
BINARY      := nitpick
CMD         := ./cmd/nitpick
GOEXE       := $(shell $(GO) env GOEXE)
BIN         := $(BINARY)$(GOEXE)
INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: all build test test-race vet fmt fmt-check tidy run install uninstall docker-windows clean help

## all: format, vet, test, then build
all: fmt vet test build

## build: compile the nitpick binary into the working directory
build:
	$(GO) build -o $(BIN) $(CMD)

## test: run the test suite
test:
	$(GO) test ./...

## test-race: run tests with the race detector (needs a C toolchain / CGO)
test-race:
	$(GO) test -race ./...

## vet: run go vet
vet:
	$(GO) vet ./...

## fmt: format all sources in place
fmt:
	gofmt -w .

## fmt-check: fail if any file is not gofmt-clean
fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## tidy: sync go.mod/go.sum
tidy:
	$(GO) mod tidy

## run: build and run the binary (pass args with ARGS="...")
run: build
	./$(BIN) $(ARGS)

## install: build and copy the binary to INSTALL_DIR (default ~/.local/bin)
install: build
	@mkdir -p "$(INSTALL_DIR)"
	cp -f "$(BIN)" "$(INSTALL_DIR)/$(BIN)"
	@chmod +x "$(INSTALL_DIR)/$(BIN)" 2>/dev/null || true
	@echo "Installed $(BIN) to $(INSTALL_DIR)"
	@echo "Ensure $(INSTALL_DIR) is on your PATH."

## uninstall: remove the installed binary from INSTALL_DIR
uninstall:
	rm -f "$(INSTALL_DIR)/$(BIN)"
	@echo "Removed $(INSTALL_DIR)/$(BIN)"

## docker-windows: cross-build the Windows binary into .nitpick/bin via Docker
docker-windows:
	DOCKER_BUILDKIT=1 docker build --output .nitpick/bin .
	@echo "Built .nitpick/bin/nitpick.exe"

## clean: remove the locally built binary
clean:
	rm -f $(BIN)

## help: list available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
