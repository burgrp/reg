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
./reg serve --addr :8080

# Graceful shutdown
kill -TERM <pid>  # or Ctrl+C
```

## Core Architecture

### Protocol-Agnostic Registry Core

**Location:** `pkg/registry/core.go`

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

**Location:** `pkg/rest/`

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

## Code Organization

```
cmd/
  serve.go           - Serve command, signal handling, shutdown coordination
pkg/
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

1. Create `pkg/{protocol}/` directory
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
