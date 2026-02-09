# Go Client Library

The Go client library provides a high-level, channel-based API for interacting with the Registry service.

## Installation

```bash
go get github.com/burgrp/reg/pkg/client/rest
```

## Quick Start

### Consumer Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

func main() {
    // Create client
    client := rest.NewClient("http://localhost:8080")

    // Create context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Consume register
    values, requests, err := client.Consume(ctx, "temperature")
    if err != nil {
        log.Fatal(err)
    }

    // Read values from channel
    for value := range values {
        fmt.Printf("Temperature: %v %v\n", value.Value, value.Metadata)
    }
}
```

### Provider Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

func main() {
    // Create client
    client := rest.NewClient("http://localhost:8080")

    // Create context
    ctx := context.Background()

    // Initial value and metadata
    initialValue := 22.5
    metadata := map[string]any{"unit": "celsius"}
    ttl := 10 * time.Second

    // Provide register
    updates, changeRequests, err := client.Provide(ctx, "temperature",
        initialValue, metadata, ttl)
    if err != nil {
        log.Fatal(err)
    }

    // Handle change requests
    for request := range changeRequests {
        fmt.Printf("Change requested: %v\n", request)
        // Accept the change
        updates <- request
    }
}
```

## Client Creation

### Basic Client

```go
import "github.com/burgrp/reg/pkg/client/rest"

client := rest.NewClient("http://localhost:8080")
```

### Client with Custom HTTP Client

```go
import (
    "net/http"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

httpClient := &http.Client{
    Timeout: 60 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 10,
    },
}

client := rest.NewClientWithHTTPClient("http://localhost:8080", httpClient)
```

### Client from Environment Variable

```go
import "github.com/burgrp/reg/pkg/client/factory"

// Reads REGISTRY environment variable
client, err := factory.NewClientFromEnv()
if err != nil {
    log.Fatal(err)
}
```

## Consumer API

### Consume Method

```go
Consume(ctx context.Context, name string) (
    <-chan ValueAndMetadata,  // Receive values
    chan<- any,               // Send change requests
    error,
)
```

**Parameters:**
- `ctx` - Context for lifecycle management
- `name` - Register name to consume

**Returns:**
- `values` - Receive-only channel for register values
- `requests` - Send-only channel for change requests
- `error` - Error if operation fails

### ValueAndMetadata Type

```go
type ValueAndMetadata struct {
    Value    any            // Register value
    Metadata map[string]any // Register metadata
}
```

### Consumer Behavior

1. **Immediate initial value** - First value sent immediately (no-wait GET)
2. **Continuous updates** - Long-polls every 5 seconds for changes
3. **Automatic cleanup** - When context cancelled, channels closed automatically

### Complete Consumer Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

func main() {
    client := rest.NewClient("http://localhost:8080")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Consume register
    values, requests, err := client.Consume(ctx, "thermostat")
    if err != nil {
        log.Fatal(err)
    }

    // Monitor values in goroutine
    go func() {
        for value := range values {
            fmt.Printf("Current state: %+v\n", value.Value)
            fmt.Printf("Metadata: %+v\n", value.Metadata)
        }
    }()

    // Send change request after 5 seconds
    time.Sleep(5 * time.Second)
    requests <- map[string]any{
        "mode":   "heating",
        "target": 22,
    }

    // Keep running
    time.Sleep(30 * time.Second)
}
```

### Multiple Subscriptions

```go
// Subscribe to multiple registers
values1, requests1, _ := client.Consume(ctx, "temperature")
values2, requests2, _ := client.Consume(ctx, "humidity")
values3, _, _ := client.Consume(ctx, "pressure")

// All subscriptions share a single batch poller
// Only one HTTP request is made for all three registers
```

## Provider API

### Provide Method

```go
Provide(ctx context.Context, name string, value any,
        metadata map[string]any, ttl time.Duration) (
    chan<- any,   // Send updates
    <-chan any,   // Receive change requests
    error,
)
```

**Parameters:**
- `ctx` - Context for lifecycle management
- `name` - Register name
- `value` - Initial value (any JSON-serializable type)
- `metadata` - Optional metadata map
- `ttl` - Time-to-live duration

**Returns:**
- `updates` - Send-only channel to update register value
- `changeRequests` - Receive-only channel for consumer change requests
- `error` - Error if operation fails

### Provider Behavior

1. **Immediate initial set** - Sets register with initial value
2. **Automatic TTL refresh** - Refreshes at 50% of TTL interval
3. **Continuous polling** - Long-polls every 30 seconds for change requests
4. **Automatic cleanup** - When context cancelled, stops refreshing

### Complete Provider Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

func main() {
    client := rest.NewClient("http://localhost:8080")
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Initial state
    currentTemp := 22.5
    metadata := map[string]any{
        "unit":     "celsius",
        "location": "room1",
    }
    ttl := 10 * time.Second

    // Provide register
    updates, changeRequests, err := client.Provide(ctx,
        "temperature", currentTemp, metadata, ttl)
    if err != nil {
        log.Fatal(err)
    }

    // Handle change requests
    go func() {
        for request := range changeRequests {
            fmt.Printf("Change requested: %v\n", request)

            // Validate request
            if temp, ok := request.(float64); ok {
                if temp >= 15 && temp <= 30 {
                    // Accept change
                    currentTemp = temp
                    updates <- currentTemp
                    fmt.Printf("Change accepted: %v\n", currentTemp)
                } else {
                    fmt.Printf("Change rejected: out of range\n")
                }
            }
        }
    }()

    // Simulate temperature sensor updates
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        // Simulate temperature reading
        currentTemp += 0.1
        updates <- currentTemp
        fmt.Printf("Temperature updated: %v\n", currentTemp)
    }
}
```

### Multiple Providers

```go
// Provide multiple registers
updates1, reqs1, _ := client.Provide(ctx, "temp", 22.5, nil, 10*time.Second)
updates2, reqs2, _ := client.Provide(ctx, "humidity", 60.0, nil, 10*time.Second)
updates3, reqs3, _ := client.Provide(ctx, "pressure", 1013.0, nil, 10*time.Second)

// All providers share a single batch poller
// Only one HTTP request is made for all three registers
```

## Batch Optimization

The client library automatically batches multiple subscriptions:

### Consumer Batching

```go
// Create multiple subscriptions
for i := 0; i < 10; i++ {
    name := fmt.Sprintf("sensor%d", i)
    values, _, _ := client.Consume(ctx, name)

    go func(ch <-chan ValueAndMetadata) {
        for v := range ch {
            // Process value
        }
    }(values)
}

// All 10 subscriptions share one batch poller
// Single HTTP request: GET /consumer?name=sensor0&name=sensor1&...&name=sensor9
```

### Provider Batching

```go
// Create multiple providers
for i := 0; i < 10; i++ {
    name := fmt.Sprintf("actuator%d", i)
    _, changeReqs, _ := client.Provide(ctx, name, 0, nil, 10*time.Second)

    go func(ch <-chan any) {
        for req := range ch {
            // Handle change request
        }
    }(changeReqs)
}

// All 10 providers share one batch poller
// Single HTTP request: GET /provider?name=actuator0&name=actuator1&...&name=actuator9
```

## Configurable Polling Intervals

For testing or specific network conditions, polling intervals can be adjusted:

```go
client := rest.NewClient("http://localhost:8080")

// Faster polling for low-latency scenarios
client.ConsumerPollInterval = 1 * time.Second  // Default: 5s
client.ProviderPollInterval = 10 * time.Second // Default: 30s

// Use client normally
values, _, _ := client.Consume(ctx, "temperature")
```

Note: Faster polling increases server load and bandwidth usage.

## Context Management

### Cancellation

```go
ctx, cancel := context.WithCancel(context.Background())

values, _, _ := client.Consume(ctx, "temperature")

// Cancel subscription
cancel()

// Channels will be closed, goroutines cleaned up
```

### Timeout

```go
// Subscription expires after 30 seconds
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

values, _, _ := client.Consume(ctx, "temperature")

for value := range values {
    // Process values
    // Channel closes after 30 seconds
}
```

### Deadline

```go
deadline := time.Now().Add(1 * time.Hour)
ctx, cancel := context.WithDeadline(context.Background(), deadline)
defer cancel()

values, _, _ := client.Consume(ctx, "temperature")
```

## Error Handling

### Connection Errors

```go
client := rest.NewClient("http://localhost:8080")

ctx := context.Background()
values, _, err := client.Consume(ctx, "temperature")
if err != nil {
    // Error creating subscription (client-side validation)
    log.Printf("Failed to create subscription: %v", err)
    return
}

// Connection errors during operation are handled internally
// Client will retry with backoff
for value := range values {
    // Process values
}
```

### HTTP Client Configuration

```go
httpClient := &http.Client{
    Timeout: 60 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxIdleConnsPerHost:   10,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
    },
}

client := rest.NewClientWithHTTPClient("http://localhost:8080", httpClient)
```

## Advanced Patterns

### Selective Change Acceptance

```go
updates, changeRequests, _ := client.Provide(ctx, "thermostat",
    initialState, metadata, 10*time.Second)

for request := range changeRequests {
    state, ok := request.(map[string]any)
    if !ok {
        log.Println("Invalid request format")
        continue
    }

    // Validate request
    if mode, ok := state["mode"].(string); ok {
        if mode == "heating" || mode == "cooling" || mode == "off" {
            // Accept valid change
            updates <- request
            log.Printf("Accepted: %v", request)
        } else {
            log.Printf("Rejected: invalid mode %s", mode)
        }
    }
}
```

### Bidirectional Communication

```go
// Consumer side
values, requests, _ := client.Consume(ctx, "device")

// Monitor values
go func() {
    for value := range values {
        fmt.Printf("Device state: %v\n", value.Value)
    }
}()

// Send commands
requests <- map[string]any{"action": "start"}
time.Sleep(5 * time.Second)
requests <- map[string]any{"action": "stop"}

// Provider side
updates, changeRequests, _ := client.Provide(ctx, "device",
    "idle", nil, 10*time.Second)

// Handle commands
for cmd := range changeRequests {
    cmdMap := cmd.(map[string]any)
    switch cmdMap["action"] {
    case "start":
        updates <- "running"
    case "stop":
        updates <- "stopped"
    }
}
```

### Fan-out Pattern

```go
// One provider, multiple consumers
// Provider
updates, reqs, _ := client.Provide(ctx, "broadcast", "message", nil, 10*time.Second)

// Consumer 1
values1, _, _ := client.Consume(ctx, "broadcast")
go func() {
    for v := range values1 {
        fmt.Printf("Consumer 1 received: %v\n", v.Value)
    }
}()

// Consumer 2
values2, _, _ := client.Consume(ctx, "broadcast")
go func() {
    for v := range values2 {
        fmt.Printf("Consumer 2 received: %v\n", v.Value)
    }
}()

// All consumers receive updates
updates <- "Hello, everyone!"
```

## Thread Safety

The client is thread-safe and can be used from multiple goroutines:

```go
client := rest.NewClient("http://localhost:8080")

// Create subscriptions from multiple goroutines
for i := 0; i < 10; i++ {
    go func(id int) {
        name := fmt.Sprintf("sensor%d", id)
        values, _, _ := client.Consume(ctx, name)
        for v := range values {
            fmt.Printf("Sensor %d: %v\n", id, v.Value)
        }
    }(i)
}
```

## Performance Considerations

### Memory Usage

Each subscription uses:
- Two channels (1 buffered slot each)
- One goroutine for cleanup
- Shared batch poller (one per client)

For 1000 subscriptions:
- Memory: ~1-2 MB
- Goroutines: ~1002 (1000 cleanup + 1 batch consumer + 1 batch provider)

### Network Usage

With batching:
- 100 consumer subscriptions = 1 HTTP request per poll cycle
- 100 provider subscriptions = 1 HTTP request per poll cycle

Without batching (hypothetical):
- 100 subscriptions = 100 HTTP requests per poll cycle

Batch optimization reduces network usage by 100x in this scenario.

### CPU Usage

Minimal CPU usage:
- Polling loops sleep between requests
- JSON parsing only when data changes
- No busy-waiting or polling

## Testing

### Using Test Client

```go
import "github.com/burgrp/reg/pkg/client/rest"

func TestConsumer(t *testing.T) {
    client := rest.NewClient("http://localhost:8080")

    // Use fast polling for tests
    client.ConsumerPollInterval = 100 * time.Millisecond

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    values, _, err := client.Consume(ctx, "test-register")
    if err != nil {
        t.Fatal(err)
    }

    // Wait for value
    select {
    case v := <-values:
        if v.Value != expectedValue {
            t.Errorf("got %v, want %v", v.Value, expectedValue)
        }
    case <-time.After(500 * time.Millisecond):
        t.Fatal("timeout waiting for value")
    }
}
```

### Mock Server

Use httptest for unit testing:

```go
import (
    "net/http/httptest"
    "testing"
)

func TestClientWithMockServer(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Mock server responses
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"registers": {"temp": {"value": 22.5}}}`))
    }))
    defer server.Close()

    client := rest.NewClient(server.URL)
    // Test client behavior
}
```
