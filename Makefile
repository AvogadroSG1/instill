.PHONY: build test unit-test bats-test lint vet install clean

BINARY ?= instill
PREFIX ?= $(HOME)/.local
GOFLAGS ?=
GOLANGCI_LINT_VERSION ?= v2.6.2
BATS_VERSION ?= 1.12.0

build:
	go build $(GOFLAGS) -o $(BINARY) .

unit-test:
	go test ./...

bats-test:
	@if command -v bats >/dev/null 2>&1; then \
		bats test; \
	elif command -v npx >/dev/null 2>&1; then \
		npx --yes bats@$(BATS_VERSION) test; \
	else \
		echo "bats is required; install bats or npm/npx"; \
		exit 127; \
	fi

test: unit-test bats-test

vet:
	go vet ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

install:
	mkdir -p $(PREFIX)/bin
	go build $(GOFLAGS) -o $(PREFIX)/bin/$(BINARY) .

clean:
	rm -rf dist instill
