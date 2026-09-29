# Makefile for PyPlayground
# ========================
# Quick commands for building and running the project.
# Usage: Open a terminal in the project root and type `make <command>`

# Default target — runs when you just type `make`
.DEFAULT_GOAL := run

# Keep in sync with the version pinned in .github/workflows/ci.yml
GOLANGCI_LINT_VERSION := v2.13.2

# Windows binaries need the .exe extension
EXE := $(if $(filter Windows_NT,$(OS)),.exe,)

# Run the server in development mode
run:
	go run ./cmd/server/main.go

# Build a production binary
build:
	go build -o bin/playground$(EXE) ./cmd/server/main.go

# Run the compiled binary
start: build
	./bin/playground$(EXE)

# Run Go tests with the race detector (Docker sandbox tests skip if Docker isn't running)
test:
	go test -race ./... -v

# Format all Go code
fmt:
	go fmt ./...

# Run Go vet (static analysis)
vet:
	go vet ./...

# Run golangci-lint (same version as CI)
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# Run everything CI runs
ci: vet lint
	REQUIRE_DOCKER=1 go test -race -count=1 ./...

# Clean build artifacts
clean:
	rm -rf bin/

.PHONY: run build start test fmt vet lint ci clean
