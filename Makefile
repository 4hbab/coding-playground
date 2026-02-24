# Makefile for PyPlayground
# ========================
# Quick commands for building and running the project.
# Usage: Open a terminal in the project root and type `make <command>`

# Default target — runs when you just type `make`
.DEFAULT_GOAL := run

# Run the server in development mode
run:
	go run ./cmd/server/main.go

# Build a production binary
build:
	go build -o bin/playground.exe ./cmd/server/main.go

# Run the compiled binary
start: build
	./bin/playground.exe

# Run Go tests
test:
	go test ./... -v

# Format all Go code
fmt:
	go fmt ./...

# Run Go vet (static analysis)
vet:
	go vet ./...

# Clean build artifacts
clean:
	rm -rf bin/

.PHONY: run build start test fmt vet clean
