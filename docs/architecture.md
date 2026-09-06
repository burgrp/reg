# Architecture Overview

## System Design

Registry uses a clean layered architecture with protocol-agnostic business logic and pluggable transport layers.

## Core Components

### 1. Protocol-Agnostic Registry Core

Location: `internal/registry/core.go`

The registry core manages all business logic without any protocol dependencies:

- **In-memory storage** - Fast access to register data
- **Thread-safe operations** - Concurrent access via RWMutex
- **TTL management** - Background goroutine checks for expired registers
- **Change request queue** - Per-register queue for consumer requests
- **Notification system** - Generic listener pattern for real-time updates

#### Data Structures

```go
type Register struct {
    Value    any                // Current value (JSON-compatible)
    Metadata map[string]any     // User-defined metadata
    TTL      time.Time          // Absolute expiration time
}
```

#### Core Operations

- `SetRegister(name, value, metadata, ttl)` - Create or update a register
- `WaitForChange(names, duration)` - Read registers with optional long polling
- `RequestChange(name, value)` - Queue a change request from consumer
- `WaitForChangeRequests(names, duration)` - Provider polls for change requests

### 2. Protocol Layers

Protocol layers translate between wire formats and core operations.

#### REST Protocol Layer

Location: `internal/rest/`

Implements HTTP/JSON API with separate modules for providers and consumers:

**Provider Endpoints** (`provider.go`):
- `PUT /provider` - Set/update registers
- `GET /provider?name=X&wait=30s` - Poll for consumer change requests

**Consumer Endpoints** (`consumer.go`):
- `GET /consumer?name=X&wait=5s` - Read registers with long polling
- `PUT /consumer` - Request value changes

**Server** (`server.go`):
- HTTP server setup
- Graceful shutdown coordination
- Shared request/response helpers

### 3. Client Libraries

The client library provides three layers of abstraction:

#### Wire Protocol Layer

Location: `pkg/wire/rest/`

Low-level HTTP clients that handle raw REST API calls:
- `ConsumerClient` - GetRegisters(), RequestChange()
- `ProviderClient` - SetRegisters(), GetChangeRequests()

#### High-Level Client Interface

Location: `pkg/client/client.go`

Protocol-agnostic interface with reactive channels:

```go
type Client interface {
    Consume(ctx, name) (<-chan ValueAndMetadata, chan<- any, error)
    Provide(ctx, name, value, metadata, ttl) (chan<- any, <-chan any, error)
}
```

#### REST Client Implementation

Location: `pkg/client/rest/`

Implements the Client interface with optimizations:
- **Batch polling** - Multiple subscriptions share single HTTP request
- **Automatic TTL refresh** - Providers auto-refresh at 50% TTL interval
- **Context-based lifecycle** - Clean cancellation and resource cleanup

#### Other Client Implementations

- `clients/js/` - Event-driven JavaScript client with batched long-polling
- `clients/node-red/` - Node-RED nodes and embedded JavaScript runtime
- `clients/python/` - Typed async Python client with async iterators and context managers

## Data Flow

### Provider Publishing Flow

```
Provider Application
       |
       | (1) Call Provide(ctx, name, value, metadata, ttl)
       v
REST Client (pkg/client/rest)
       |
       | (2) PUT /provider
       v
REST Server (internal/rest)
       |
       | (3) SetRegister()
       v
Registry Core (internal/registry)
       |
       | (4) Store register with TTL
       v
In-Memory Storage
```

### Consumer Reading Flow

```
Consumer Application
       |
       | (1) Call Consume(ctx, name)
       v
REST Client (pkg/client/rest)
       |
       | (2) GET /consumer?name=X&wait=5s
       v
REST Server (internal/rest)
       |
       | (3) WaitForChange()
       v
Registry Core (internal/registry)
       |
       | (4) Return current value or wait for change
       v
REST Client
       |
       | (5) Send value on channel
       v
Consumer Application receives on channel
```

### Change Request Flow

```
Consumer                    Registry                    Provider
    |                          |                            |
    | (1) PUT /consumer        |                            |
    |------------------------->|                            |
    |                          |                            |
    |      (2) RequestChange() |                            |
    |                          | (store in queue)           |
    |                          |                            |
    |                          |<---------------------------|
    |                          | (3) GET /provider?wait=30s |
    |                          |                            |
    |                          |--------------------------->|
    |                          | (4) Return queued request  |
    |                          |                            |
    |                          |     (5) Decide accept/deny |
    |                          |                            |
    |                          |<---------------------------|
    |                          | (6) PUT /provider (update) |
    |                          |                            |
    |<-------------------------|                            |
    | (7) GET /consumer        |                            |
    |------------------------->|                            |
    | (8) Receive new value    |                            |
```

## Concurrency Model

### Thread Safety

- **Registry Core** - Uses `sync.RWMutex` for register map and request queues
- **Listeners** - Separate mutex for callback management
- **Client Batching** - Per-client mutex protects subscription maps

### Goroutine Lifecycle

Each component spawns goroutines with proper cleanup:

**Server:**
- Main HTTP server goroutine
- TTL cleanup goroutine (checks every 1 second)
- Per-request handler goroutines

**Client:**
- One batch poller goroutine per client (consumer and provider)
- Per-subscription goroutines for initial fetch and request handling
- Cleanup goroutines that wait for context cancellation

All goroutines respect context cancellation and coordinate via:
- Context.Done() for cancellation signaling
- sync.WaitGroup for ensuring sends complete before channel closure
- Subscription contexts to prevent send-on-closed-channel

## Graceful Shutdown

Server shutdown sequence:

1. SIGINT/SIGTERM received
2. Root context cancelled
3. Signal sent to TTL cleanup goroutine via stopChan
4. HTTP server shutdown with 10-second timeout for in-flight requests
5. All goroutines exit cleanly

Coordinated via `errgroup.Group` for multiple concurrent tasks.

## Design Principles

### 1. Separation of Concerns

- Core logic has zero protocol dependencies
- Protocols only handle serialization and transport
- Easy to add WebSocket, MQTT, or other protocols

### 2. Simplicity

- No explicit connection tracking
- No ownership model (registers are independent)
- TTL handles lifecycle automatically

### 3. IoT-Focused

- Designed for devices with intermittent connectivity
- TTL-based expiration handles device failures
- Change requests enable command-and-control patterns

### 4. Performance

- In-memory storage for fast access
- Batch polling reduces HTTP overhead
- Long polling provides real-time updates without WebSocket complexity

### 5. Observability

- Structured logging with slog
- Clear lifecycle events (creation, expiration, changes)
- Debug-level logging for development

## Extension Points

### Adding New Protocol

To add WebSocket, MQTT, or other protocols:

1. Create `internal/{protocol}/` directory
2. Implement handlers that call registry core methods
3. Handle protocol-specific serialization
4. Integrate with graceful shutdown via context

Example structure:
```
internal/mqtt/
  - client.go      (MQTT connection handling)
  - consumer.go    (Subscribe to topics, call WaitForChange)
  - provider.go    (Publish messages, call SetRegister)
```

### Customizing Client Behavior

The REST client exposes configurable fields:

```go
client := rest.NewClient(url)
client.ConsumerPollInterval = 1 * time.Second  // Faster polling
client.ProviderPollInterval = 60 * time.Second // Slower polling
```

This allows tuning for different network conditions or testing.
