
# PAULOS — GO PROJECT
# Add this to the root of a Go repository.

set shell := ["bash", "-uc"]
set positional-arguments

# Display project commands
default:
    @just --list

# ─── DEVELOPMENT ─────────────────────────────

# Build all Go packages
build:
    go build ./...

# Run all tests
test:
    go test ./...

# Run tests with the race detector
test-race:
    go test -race ./...

# Format all Go packages
fmt:
    go fmt ./...

# Check formatting without modifying files
fmt-check:
    @files="$(gofmt -l .)"; \
    if [ -n "$files" ]; then \
        printf '%s\n' "$files"; \
        exit 1; \
    fi

# Run static analysis
vet:
    go vet ./...

# Run Staticcheck
lint:
    staticcheck ./...

# Download module dependencies
deps:
    go mod download

# Tidy module dependencies
tidy:
    go mod tidy

# Run formatting, analysis and tests
check: fmt vet test

# ─── UTILITIES ───────────────────────────────

# View Git repository status
status:
    git status -sb

# Launch Lazygit
git:
    lazygit

# Display the repository's Go version
version:
    @grep '^go ' go.mod
