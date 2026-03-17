# Precon — Pre-Conscious Context Management

default:
    @just --list

# Build the binary
build:
    go build -o precon ./cmd/precon
    codesign -f -s - precon

# Run tests
test:
    go test ./...

# Build with race detector and run
dev:
    go build -race -o precon ./cmd/precon && ./precon

# Run the binary
run: build
    ./precon

# Format Go code
fmt:
    go fmt ./...

# Vet code
vet:
    go vet ./...

# Install to /usr/local/bin
install: build
    cp precon /usr/local/bin/precon
    codesign -f -s - /usr/local/bin/precon

# Check everything
check: fmt vet test
