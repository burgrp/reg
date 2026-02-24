# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Registry (`reg`) is an IoT-focused service for managing "registers" - named entities with dynamic values and metadata. It implements a simplified Provider/Consumer model with automatic lifecycle management based on TTL (time-to-live).

## Build and Run

```bash
# Build the binary
go build -o reg

# Run the server
./reg serve
./reg serve --rest :8080

# Graceful shutdown
kill -TERM <pid>  # or Ctrl+C
```

## Core Architecture

### Protocol-Agnostic Registry Core

**Location:** `internal/registry/core.go`

The registry core is completely protocol-agnostic, using only native Go types with no serialization dependencies. It manages registers in-memory with thread-safe operations.

**Key data structures:**
- `registers` - Map of register name to Register (value, metadata, TTL)
- `pendingRequests` - Queue per register for consumer change requests (latest only)
- `valueChangeListeners` - Notifies when register values change
- `changeRequestListeners` - Notifies when consumers request changes

**Core operations:**
- `SetRegister(name, value, metadata, ttl)` - Provider creates/updates register
- `WaitForChange(names, duration)` - Consumer reads/polls for register changes (long polling)
- `RequestChange(name, value)` - Consumer requests value change
- `WaitForChangeRequests(names, duration)` - Provider polls for change requests (long polling)

**Lifecycle management:**
- Background goroutine checks TTL expiry every 1 second
- Expired registers are deleted and listeners notified
- Default TTL is 10 seconds if not specified
- Cleanup goroutine respects shutdown signal via `stopChan`

### REST Protocol Layer

**Location:** `internal/rest/`

The REST layer is split by role:
- `consumer.go` - Consumer endpoints (read, request changes)
- `provider.go` - Provider endpoints (set registers, poll for requests)
- `server.go` - HTTP server setup, graceful shutdown, shared helpers

**Consumer endpoints:**
- `GET /consumer?name=X&wait=5s` - Read registers with optional long polling
- `PUT /consumer` - Request value changes (returns 202 Accepted)

**Provider endpoints:**
- `PUT /provider` - Set/update registers with value, metadata, TTL
- `GET /provider?name=X&wait=30s` - Poll for consumer change requests

**Request/Response format:**
```json
{
  "registers": {
    "temp": {
      "value": 25.5,
      "metadata": {"unit": "celsius"},
      "ttl": "5s"
    }
  }
}
```

TTL uses Go duration format: "5s", "10m", "1h30m". This is handled by the custom `Duration` type in `duration.go` that unmarshals from JSON strings.

### Long Polling Pattern

Both consumer reads and provider request polling use the same wait/notify pattern:

1. If `wait` parameter specified, register listener for changes
2. Block until notification arrives OR timeout
3. Return current state (consuming pending requests for providers)

The `waitForNotification()` helper in core.go implements this pattern for both value changes and change requests.

### Change Request Flow

1. Consumer calls `PUT /consumer` with desired value
2. Request stored in `pendingRequests` map (latest only per register)
3. `changeRequestListeners` notified
4. Provider polling on `GET /provider` receives the request
5. Request consumed (removed from queue)
6. Provider decides to accept/reject/modify
7. Provider updates via `PUT /provider` if accepted
8. Consumers see new value via `GET /consumer` or long polling

There is no synchronous reply to consumers - they observe the actual value through polling or subscriptions.

## Key Design Patterns

### No Static Variables or init()

Commands use factory functions (`newServeCmd()`, `newRootCmd()`) instead of package-level variables and `init()`. This enables clean dependency injection and testability.

### Simplified Model (No Explicit Provider/Consumer Interfaces)

Unlike the original design documented in CLAUDE.md, the current implementation does NOT use explicit Provider/Consumer interfaces. Instead:
- Protocols call registry methods directly
- No connection tracking or ownership
- Registers exist independently until TTL expires
- Simpler implementation, sufficient for IoT use cases

### Graceful Shutdown

Shutdown sequence on SIGINT/SIGTERM:
1. Context cancelled
2. Signal sent to cleanup goroutine via `stopChan`
3. HTTP server shutdown with 10-second timeout for in-flight requests
4. All goroutines exit cleanly

The `errgroup.Group` coordinates multiple concurrent shutdown tasks.

### Listener Pattern for Notifications

The generic `Listeners[T]` type in `listener.go` manages callback registration/removal with thread-safe operations. Used for both value changes and change requests.

## Client Library Architecture

The client library is organized in three layers, from low-level to high-level:

### 1. Wire Protocol Layer (`pkg/wire/rest/`)

Low-level HTTP clients that handle raw REST API calls:
- `consumer.go` - `GetRegisters()`, `RequestChange()`
- `provider.go` - `SetRegisters()`, `GetChangeRequests()`

These use standard HTTP methods and handle JSON marshaling, but don't provide reactive patterns or batching.

### 2. High-Level Client Interface (`pkg/client/`)

**`client.go`** - Protocol-agnostic `Client` interface:
```go
type Client interface {
    Consume(ctx context.Context, name string) (<-chan ValueAndMetadata, chan<- any, error)
    Provide(ctx context.Context, name string, value any, metadata map[string]any, ttl time.Duration) (chan<- any, <-chan any, error)
}
```

**Channel-based reactive API:**
- `Consume()` - Returns receive-only channel for values, send-only channel for change requests
  - Immediately sends initial value (no-wait GET)
  - Continuous long-polling for updates
- `Provide()` - Returns send-only channel for updates, receive-only channel for change requests
  - Automatic TTL refresh at 50% interval
  - Long-polling for change requests

Both methods spawn goroutines that clean up automatically when context is cancelled.

### 3. REST Client Implementation (`pkg/client/rest/`)

**`client.go`** - Client struct and constructors:
```go
func NewClient(baseURL string) *Client
func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client
```

**`consumer.go`** - Consumer implementation with batching:
- Multiple `Consume()` calls share a single batch poller goroutine
- Consolidates subscriptions into single HTTP GET with multiple `?name=` parameters
- Long-polls every 5 seconds
- Distributes updates to individual subscription channels

**`provider.go`** - Provider implementation with batching:
- Multiple `Provide()` calls share a single batch poller goroutine
- Consolidates change request polling into single HTTP GET
- Long-polls every 30 seconds
- Auto-refreshes TTL at 50% interval for each register
- Distributes change requests to individual provider channels

**Batching optimization:**
When multiple consumers/providers exist for different registers, the client makes a single HTTP request for all of them instead of one request per register. This dramatically reduces network overhead in IoT scenarios with many simultaneous subscriptions.

### 4. Client Factory (`pkg/client/factory/`)

Separate package to avoid import cycles (factory imports both `pkg/client` and `pkg/client/rest`).

```go
func NewClient(registryURL string) (client.Client, error)
func NewClientFromEnv() (client.Client, error)
```

`NewClient()` creates a client based on URL scheme (currently only http/https supported). `NewClientFromEnv()` reads the `REGISTRY` environment variable.

## CLI Commands

### `serve` Command

Starts the registry server:
```bash
./reg serve              # Default :8080
./reg serve --rest :9000 # Custom address
```

See "Graceful Shutdown" section for shutdown behavior.

### `provide` Command

Provides a register and stays running for interactive updates:

```bash
# Provide register with default null value
./reg provide temp

# With initial value (JSON)
./reg provide temp 25.5

# With value and metadata (JSON)
./reg provide temp 25.5 '{"unit":"celsius"}'

# Custom TTL (default 5s)
./reg provide temp 25.5 '{"unit":"celsius"}' --ttl 10s

# Interactive usage
echo '30.0' | ./reg provide temp 25.5 '{"unit":"celsius"}'
```

**Behavior:**
- Requires `REGISTRY` environment variable (e.g., `http://localhost:8080`)
- Creates register with initial value and metadata (both must be valid JSON)
- Stays running, continuously:
  - Reading new values from stdin (one JSON value per line)
  - Writing consumer change requests to stdout (as JSON)
  - Writing status messages to stderr

**Example session:**
```bash
export REGISTRY=http://localhost:8080
./reg provide temp 25.5 '{"unit":"celsius"}' &
# stderr: Providing register 'temp' with value 25.5, metadata map[unit:celsius], TTL 5s
# stderr: Reading new values from stdin (one JSON value per line)...
# stderr: Writing change requests to stdout...

echo '30.0'
# stderr: Updated register 'temp' to: 30
# stdout: (any consumer change requests as JSON)
```

Press Ctrl+C to stop providing (register expires after TTL).

### `mcp` Command

Starts an MCP (Model Context Protocol) stdio server for AI assistant integration:

```bash
export REGISTRY=http://localhost:8080
./reg mcp
```

**Behavior:**
- Requires `REGISTRY` environment variable (e.g., `http://localhost:8080`)
- Communicates over stdin/stdout using JSON-RPC 2.0
- Designed for use with MCP clients like Claude Desktop, Cline, etc.
- Blocks indefinitely, processing JSON-RPC requests

**MCP Tools Exposed:**

1. **`get_register`** - Get a register's current value and metadata
   - Input: `name` (string)
   - Returns: Register value and metadata as text
   - Uses `Consume()` to get current value

2. **`set_register`** - Set a register's value (provider operation)
   - Input: `name` (string), `value` (any JSON type), `metadata` (object, optional), `ttl` (string, default "5s")
   - Returns: Confirmation message
   - Uses `Provide()` to create/update register with TTL

3. **`list_registers`** - List all registers with their values
   - Input: None
   - Returns: Formatted list of all registers with values and metadata
   - Uses `ConsumeAll()` to get all current registers

4. **`request_change`** - Request a value change (consumer operation)
   - Input: `name` (string), `value` (any JSON type)
   - Returns: Confirmation that request was sent
   - Uses `Consume()` to send change request to provider

**Implementation:**
- Location: `cmd/mcp.go`
- Uses official `github.com/modelcontextprotocol/go-sdk/mcp` library
- Tool handlers use typed input structs with jsonschema tags for automatic schema generation
- All tools return `*mcp.CallToolResult` with text content

**Error Handling:**
- Connection errors returned with `IsError: true` flag
- Timeouts (100ms for get/list operations) treated as "not found"
- Graceful handling of context cancellation

Press Ctrl+C to stop the MCP server.

## Code Organization

```
cmd/
  serve.go           - Serve command, signal handling, shutdown coordination
  provide.go         - Provide command, stdin/stdout interaction
  mcp.go             - MCP stdio server command, AI assistant integration
  root.go            - Root command, subcommand registration
pkg/
  client/
    client.go        - Protocol-agnostic Client interface
    factory/
      factory.go     - URL-based client factory
    rest/
      client.go      - REST client struct and constructors
      consumer.go    - Consume implementation with batching
      provider.go    - Provide implementation with batching and TTL refresh
  wire/
    rest/
      consumer.go    - Low-level consumer REST client
      provider.go    - Low-level provider REST client
internal/
  registry/
    core.go          - Protocol-agnostic registry implementation
    listener.go      - Generic notification listener pattern
  rest/
    server.go        - HTTP server, graceful shutdown, shared helpers
    consumer.go      - Consumer REST endpoints
    provider.go      - Provider REST endpoints
    duration.go      - Custom Duration type for JSON unmarshaling
main.go              - Entry point (minimal)
```

## Development Notes

### Adding New Protocol Implementations

To add WebSocket, MQTT, or other protocols:

1. Create `internal/{protocol}/` directory
2. Implement handlers that call registry methods:
   - `SetRegister()` for provider updates
   - `WaitForChange()` for consumer reads
   - `RequestChange()` for consumer requests
   - `WaitForChangeRequests()` for provider polling
3. Handle protocol-specific serialization (core uses `any` and `map[string]any`)
4. Integrate with graceful shutdown via context cancellation

The core registry handles all business logic - protocols only deal with transport and serialization.

### TTL Management

- TTL is specified as Go duration string ("5s", "10m") in REST
- Stored internally as absolute time (`time.Time`)
- Periodic cleanup checks every 1 second
- Missing/zero TTL defaults to 10 seconds
- Expired registers are removed, not marked as stale

### Thread Safety

- Registry uses `sync.RWMutex` for registers and pending requests separately
- Listeners have their own mutex
- Long-polling wait blocks outside locks to avoid deadlocks
- Cleanup goroutine holds write lock during expiry check

### Logging

Uses `slog` with `tint` handler for colored, structured logs:
- Info: Lifecycle events (startup, shutdown, register expiry)
- Debug: Register changes, change requests
- Warn: Register expiry (data loss)
- Error: HTTP encoding failures, shutdown errors
