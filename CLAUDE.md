# Registry (reg) - Architecture Documentation

## Overview

Registry is an IoT-focused service for managing "registers" - named entities with dynamic values and metadata. It implements a Provider/Consumer model with automatic lifecycle management based on TTL (time-to-live).

## Core Concepts

### Register

A register represents an IoT entity with three components:
- **Name**: Unique identifier (string)
- **Value**: Dynamic value of any JSON-compatible type (`any`)
- **Metadata**: Static configuration data (`map[string]any`)

Metadata is set by the provider and should not change during the register's lifetime. Only the value is dynamic.

### Provider/Consumer Model

**Provider:**
- Creates and owns registers
- Updates register values
- Must send updates within TTL window or register is removed
- Receives update requests from consumers (async)
- Provider connection determines register lifetime

**Consumer:**
- Reads register values
- Requests updates (no synchronous reply)
- Subscribes to value changes
- Receives `nil` value when register becomes unavailable

### TTL-Based Lifecycle

Registers have a time-to-live (TTL) specified in milliseconds:
1. Provider sends update with value, metadata, and TTL
2. Registry starts a timer for the specified TTL
3. Provider must send next update before TTL expires
4. If TTL expires:
   - Register is removed from registry
   - Subscribed consumers receive `nil` value notification
   - Provider must recreate register if needed

The `nil` value is the standard indicator for missing/unavailable registers.

## Architecture

### Protocol-Agnostic Core

**Location:** `pkg/registry/core.go`

The core registry is completely protocol-agnostic:
- Uses native Go types (`any`, `map[string]any`)
- No JSON or serialization dependencies
- Thread-safe with `sync.RWMutex`
- Implements Provider/Consumer interfaces

**Key Operations:**
- `RegisterProvider(Provider)` / `UnregisterProvider(string)` - Manage provider connections
- `RegisterConsumer(Consumer)` / `UnregisterConsumer(string)` - Manage consumer connections
- `ProviderUpdateRegister(providerID, name, value, metadata, ttlMs)` - Create/update register
- `ConsumerRequestUpdate(consumerID, name, value)` - Request update (forwarded to provider)
- `Get(name)` / `List()` - Read operations
- `Subscribe(consumerID, registerName)` - Subscribe to updates

**Important:** The unified `ProviderUpdateRegister` method handles both creation and updates. No separate create operation exists.

### Protocol Implementations

Each protocol (REST, WebSocket, MQTT, etc.) implements serialization and transport:
- Converts between protocol format and native Go types
- Manages protocol-specific connection lifecycle
- Implements Provider/Consumer interfaces for bidirectional communication

**REST Implementation:** `pkg/rest/server.go`

Consumer endpoints:
- `GET /registers` - List all registers
- `GET /registers/{name}` - Read register
- `PUT /registers/{name}` - Request update (returns 202 Accepted)

Provider endpoints:
- `PUT /provider/registers/{name}` - Create or update register

Request format:
```json
{
  "value": <any>,
  "metadata": {"key": "value"},
  "ttl": 5000
}
```

## Design Decisions

### Why Unified Create/Update Endpoint?

Providers continuously update registers. Having separate create/update operations adds complexity:
- Provider doesn't need to track whether register exists
- Simpler state management
- Natural fit for IoT sensors that periodically send data

### Why TTL-Based Lifecycle?

- Automatic cleanup when providers go offline
- No explicit disconnect/cleanup protocol needed
- Consumers immediately know when data is stale
- Provider controls update frequency based on data characteristics

### Why nil for Missing Values?

- Simple, clear semantics
- JSON-compatible (`null`)
- Distinguishes "register doesn't exist" from "value is 0/false/empty"
- Consistent across all protocol implementations

### Why Async Consumer Updates?

Consumer update requests are forwarded to providers asynchronously:
- No synchronous reply to consumer
- Provider decides whether to accept, reject, or modify the requested value
- Provider updates registry if it accepts the change
- Consumers see the actual value through subscription or polling

This matches real IoT scenarios where:
- Devices may accept values within constraints (e.g., thermostat limiting range)
- Devices may be temporarily offline
- Update success depends on physical device capabilities

## Command Structure

The CLI uses Cobra without static variables or `init()` functions:
- Factory functions create commands (`newServeCmd()`, `newRootCmd()`)
- Clean dependency injection
- Commands are constructed at runtime

**Serve Command:**
```bash
reg serve --addr :8080
```

Starts the registry server with:
- Tint-colored structured logging (slog)
- REST API on specified address
- Core registry with provider/consumer management

## Future Protocol Implementations

To add a new protocol:

1. Create `pkg/{protocol}/server.go`
2. Implement serialization for your protocol
3. Implement `Provider` and `Consumer` interfaces
4. Register providers/consumers with core registry
5. Handle subscription notifications (SSE, WebSocket, MQTT, etc.)

The core registry handles all business logic - protocols only deal with transport and serialization.
