# Development Guide

Guide for building, testing, and contributing to Registry.

## Prerequisites

- Go 1.21 or later
- Git
- Make (optional, for convenience)

## Getting Started

### Clone Repository

```bash
git clone https://github.com/burgrp/reg.git
cd reg
```

### Build

```bash
# Build binary
go build -o reg

# Build with version information
VERSION=1.0.0
go build -ldflags="-X 'github.com/burgrp/reg/cmd.Version=$VERSION'" -o reg

# Verify build
./reg version
```

### Run Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests without cache
go test -count=1 ./...

# Run specific test
go test -v ./pkg/client/rest -run TestClient_Consume

# Run with race detector
go test -race ./...

# Run with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Run Server Locally

```bash
# Start on default port (8080)
./reg serve

# Start on custom port
./reg serve --rest :9000

# With custom logging
LOG_LEVEL=debug ./reg serve
```

## Project Structure

```
reg/
├── cmd/                    # CLI commands
│   ├── root.go            # Root command
│   ├── serve.go           # Server command
│   ├── provide.go         # Provider CLI command
│   ├── get.go             # Consumer read command
│   ├── list.go            # List registers command
│   ├── browse.go          # Interactive TUI
│   └── version.go         # Version command
├── internal/              # Private application code
│   ├── registry/          # Core business logic
│   │   ├── core.go        # Registry implementation
│   │   └── listener.go    # Notification system
│   └── rest/              # REST protocol layer
│       ├── server.go      # HTTP server
│       ├── consumer.go    # Consumer endpoints
│       ├── provider.go    # Provider endpoints
│       └── duration.go    # Duration JSON handling
├── pkg/                   # Public library code
│   ├── client/            # Client interface
│   │   ├── client.go      # Protocol-agnostic interface
│   │   ├── factory/       # Client factory
│   │   └── rest/          # REST client implementation
│   │       ├── client.go  # Client struct
│   │       ├── consumer.go # Consumer implementation
│   │       └── provider.go # Provider implementation
│   ├── wire/              # Wire protocol clients
│   │   └── rest/          # Low-level REST clients
│   └── browser/           # TUI browser
├── docs/                  # Documentation
├── main.go                # Entry point
├── go.mod                 # Go modules
└── build.sh               # Release build script
```

## Development Workflow

### Making Changes

1. Create a feature branch:
```bash
git checkout -b feature/my-feature
```

2. Make your changes

3. Run tests:
```bash
go test ./...
```

4. Format code:
```bash
go fmt ./...
```

5. Run linters:
```bash
go vet ./...
golangci-lint run  # If installed
```

6. Commit changes:
```bash
git add .
git commit -m "Add feature: description"
```

### Testing Changes

#### Manual Testing

Terminal 1 (Server):
```bash
./reg serve
```

Terminal 2 (Provider):
```bash
export REGISTRY=http://localhost:8080
./reg provide temperature 22.5 '{"unit":"celsius"}' --ttl 10s
```

Terminal 3 (Consumer):
```bash
export REGISTRY=http://localhost:8080
./reg get temperature
```

#### Integration Testing

```bash
# Start server in background
./reg serve &
SERVER_PID=$!

# Run tests against server
export REGISTRY=http://localhost:8080

# Test provider
./reg provide test 123 &
PROVIDER_PID=$!

sleep 1

# Test consumer
RESULT=$(./reg get test)
echo "$RESULT"

# Cleanup
kill $PROVIDER_PID $SERVER_PID
```

## Writing Tests

### Unit Tests

```go
package rest

import (
    "testing"
)

func TestMyFunction(t *testing.T) {
    // Arrange
    input := "test"

    // Act
    result := myFunction(input)

    // Assert
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

### Integration Tests

```go
func TestServerIntegration(t *testing.T) {
    // Create test server
    server := httptest.NewServer(handler)
    defer server.Close()

    // Test client
    client := rest.NewClient(server.URL)

    // Run test
    ctx := context.Background()
    values, _, err := client.Consume(ctx, "test")
    if err != nil {
        t.Fatal(err)
    }

    // Verify
    select {
    case v := <-values:
        if v.Value != expected {
            t.Errorf("got %v, want %v", v.Value, expected)
        }
    case <-time.After(1 * time.Second):
        t.Fatal("timeout")
    }
}
```

### Table-Driven Tests

```go
func TestDuration_UnmarshalJSON(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    time.Duration
        wantErr bool
    }{
        {"seconds", `"5s"`, 5 * time.Second, false},
        {"minutes", `"10m"`, 10 * time.Minute, false},
        {"invalid", `"invalid"`, 0, true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var d Duration
            err := d.UnmarshalJSON([]byte(tt.input))

            if (err != nil) != tt.wantErr {
                t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
                return
            }

            if time.Duration(d) != tt.want {
                t.Errorf("UnmarshalJSON() = %v, want %v", d, tt.want)
            }
        })
    }
}
```

## Debugging

### Enable Debug Logging

```go
import "log/slog"

// In main.go or test
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
slog.SetDefault(logger)
```

### Debugging with Delve

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug test
dlv test ./pkg/client/rest -- -test.run TestClient_Consume

# Debug application
dlv debug . -- serve --rest :8080

# Common commands
(dlv) break main.main
(dlv) continue
(dlv) print variable
(dlv) next
(dlv) step
(dlv) quit
```

### Tracing HTTP Requests

```go
client := rest.NewClientWithHTTPClient("http://localhost:8080", &http.Client{
    Transport: &http.Transport{
        // Enable tracing
        DisableKeepAlives: true,
    },
})
```

Or use environment variable:
```bash
export GODEBUG=http2debug=1
./reg serve
```

## Performance Testing

### Benchmarking

```go
func BenchmarkRegistry_SetRegister(b *testing.B) {
    r := registry.New()
    ctx := context.Background()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        r.SetRegister(ctx, "test", i, nil, 10*time.Second)
    }
}
```

Run benchmarks:
```bash
# Run all benchmarks
go test -bench=. ./...

# Run specific benchmark
go test -bench=BenchmarkRegistry_SetRegister ./internal/registry

# With memory profiling
go test -bench=. -benchmem ./...

# Generate CPU profile
go test -bench=. -cpuprofile=cpu.prof ./...
go tool pprof cpu.prof
```

### Load Testing

```bash
# Install hey
go install github.com/rakyll/hey@latest

# Load test provider endpoint
hey -n 10000 -c 100 -m PUT \
  -H "Content-Type: application/json" \
  -d '{"registers":{"temp":{"value":22.5,"ttl":"10s"}}}' \
  http://localhost:8080/provider

# Load test consumer endpoint
hey -n 10000 -c 100 \
  http://localhost:8080/consumer?name=temp
```

## Continuous Integration

### GitHub Actions

Tests run automatically on push via `.github/workflows/test.yml`:

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - run: go test ./...
```

### Pre-commit Hooks

Create `.git/hooks/pre-commit`:

```bash
#!/bin/bash

echo "Running tests..."
go test ./...
if [ $? -ne 0 ]; then
    echo "Tests failed"
    exit 1
fi

echo "Running go fmt..."
go fmt ./...

echo "Running go vet..."
go vet ./...
if [ $? -ne 0 ]; then
    echo "go vet failed"
    exit 1
fi

exit 0
```

Make it executable:
```bash
chmod +x .git/hooks/pre-commit
```

## Release Process

### Version Tagging

```bash
# Tag release
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### Building Release Binaries

Build script creates binaries for multiple platforms:

```bash
# Build for all platforms
GITHUB_REF=refs/tags/v1.0.0 ./build.sh

# Binaries created in bin/
ls bin/
# reg-linux-amd64
# reg-linux-arm64
# reg-darwin-amd64
# reg-darwin-arm64
# reg-windows-amd64.exe
```

### Manual Release Build

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build \
  -ldflags="-X 'github.com/burgrp/reg/cmd.Version=1.0.0'" \
  -o reg-linux-amd64

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build \
  -ldflags="-X 'github.com/burgrp/reg/cmd.Version=1.0.0'" \
  -o reg-darwin-arm64

# Windows AMD64
GOOS=windows GOARCH=amd64 go build \
  -ldflags="-X 'github.com/burgrp/reg/cmd.Version=1.0.0'" \
  -o reg-windows-amd64.exe
```

## Common Tasks

### Add New CLI Command

1. Create command file in `cmd/`:
```go
// cmd/mycommand.go
package cmd

import "github.com/spf13/cobra"

func newMyCommandCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "mycommand",
        Short: "Description",
        RunE: func(cmd *cobra.Command, args []string) error {
            // Implementation
            return nil
        },
    }
}
```

2. Register in root command:
```go
// cmd/root.go
func newRootCmd() *cobra.Command {
    root := &cobra.Command{...}
    root.AddCommand(
        newServeCmd(),
        newMyCommandCmd(), // Add here
    )
    return root
}
```

### Add New REST Endpoint

1. Add handler to `internal/rest/`:
```go
// internal/rest/myendpoint.go
func handleMyEndpoint(reg *registry.Registry) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Implementation
    }
}
```

2. Register in server:
```go
// internal/rest/server.go
func RunServer(ctx context.Context, addr string, reg *registry.Registry) error {
    mux := http.NewServeMux()
    mux.HandleFunc("/myendpoint", handleMyEndpoint(reg))
    // ...
}
```

### Add Client Method

1. Add to interface:
```go
// pkg/client/client.go
type Client interface {
    Consume(...) (...)
    Provide(...) (...)
    MyNewMethod(...) (...) // Add here
}
```

2. Implement in REST client:
```go
// pkg/client/rest/myclient.go
func (c *Client) MyNewMethod(...) (...) {
    // Implementation
}
```

## Troubleshooting

### Tests Failing

```bash
# Clear test cache
go clean -testcache

# Run with verbose output
go test -v ./...

# Run specific failing test
go test -v ./pkg -run TestName
```

### Import Cycle

Reorganize code:
- Move shared types to separate package
- Use interfaces to break dependencies
- Ensure `internal/` doesn't import `pkg/`

### Race Condition

```bash
# Run with race detector
go test -race ./...

# Fix by:
# - Adding mutexes
# - Using channels
# - Avoiding shared state
```

## Code Style

### Formatting

```bash
# Format all code
go fmt ./...

# Check formatting
gofmt -l .
```

### Naming Conventions

- Packages: lowercase, single word
- Exported: PascalCase
- Unexported: camelCase
- Interfaces: noun or -er suffix
- Constants: PascalCase or SCREAMING_SNAKE_CASE

### Comments

```go
// Package rest implements the REST protocol layer.
package rest

// Client implements the client.Client interface using REST.
type Client struct {
    // consumerClient handles consumer operations.
    consumerClient *wire.ConsumerClient
}

// NewClient creates a new REST client with the given base URL.
func NewClient(baseURL string) *Client {
    // Implementation
}
```

## Resources

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)
