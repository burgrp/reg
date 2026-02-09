# Register Lifecycle Management

Understanding how registers are created, maintained, and expired.

## Lifecycle States

A register progresses through these states:

```
[Created] --> [Active] --> [Expired] --> [Removed]
     ^           |
     |___________|
      (Refreshed)
```

## Creation

### When Registers are Created

A register is created when a provider sets it for the first time:

```bash
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 22.5,
        "metadata": {"unit": "celsius"},
        "ttl": "10s"
      }
    }
  }'
```

At creation:
- Register name is established (cannot be changed)
- Initial value is set
- Metadata is stored
- TTL timer starts

### Initial State

```
Register: temperature
  Value: 22.5
  Metadata: {"unit": "celsius"}
  Created: 2024-01-15 10:00:00
  Expires: 2024-01-15 10:00:10 (TTL: 10s)
  Status: Active
```

## Active State

### While Active

A register is active from creation until TTL expiration:

- Visible to consumers via GET /consumer
- Can be read by any number of consumers
- Can receive change requests from consumers
- Can be updated by provider

### Updating Values

Provider can update value without changing TTL timer:

```bash
# Update only the value
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 23.0,
        "metadata": {"unit": "celsius"},
        "ttl": "10s"
      }
    }
  }'
```

Note: Each PUT resets the TTL timer.

### Reading Values

Consumers can read at any time:

```bash
curl "http://localhost:8080/consumer?name=temperature"
```

Returns:
```json
{
  "registers": {
    "temperature": {
      "value": 23.0,
      "metadata": {"unit": "celsius"}
    }
  }
}
```

## TTL Management

### What is TTL?

Time-To-Live (TTL) is the duration a register remains valid without refresh:

- Specified in Go duration format: "10s", "5m", "1h"
- Default TTL: 10 seconds (if omitted)
- Timer starts at creation/update
- Stored as absolute expiration time internally

### TTL Timer

```
Timeline:
  0s              5s              10s
  |---------------|---------------|
  ^                               ^
  Created                    Expires
  TTL: 10s
```

### Refreshing (Extending TTL)

Provider refreshes by setting the register again:

```bash
# Refresh by re-setting with same value
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 23.0,
        "metadata": {"unit": "celsius"},
        "ttl": "10s"
      }
    }
  }'
```

Effect: TTL timer resets to 10 seconds from now.

### Automatic Refresh (Client Library)

The Go client library automatically refreshes at 50% of TTL:

```go
// Provider with 10s TTL
updates, reqs, _ := client.Provide(ctx, "temp", 22.5, nil, 10*time.Second)

// Client automatically refreshes every 5 seconds
// Provider doesn't need to manually refresh
```

Timeline:
```
  0s      5s      10s     15s     20s
  |-------|-------|-------|-------|
  ^       ^       ^       ^       ^
  Set   Refresh Refresh Refresh Refresh
        (auto)  (auto)  (auto)  (auto)
```

### Choosing TTL Duration

Guidelines for TTL values:

**Short TTL (5-10 seconds):**
- Fast failure detection
- Higher refresh traffic
- Use for critical devices that need quick failover

**Medium TTL (30-60 seconds):**
- Balanced approach
- Reasonable failure detection
- Lower refresh traffic
- Good for most IoT devices

**Long TTL (5-15 minutes):**
- Slow failure detection
- Minimal refresh traffic
- Use for stable devices with rare updates

Example by device type:

```
Device Type              Recommended TTL
----------------         ---------------
Critical sensor          10s
Smart home device        30s
Configuration store      5m
Status dashboard         1m
Heartbeat monitor        5s
```

## Expiration

### When Registers Expire

A register expires when:

1. TTL duration elapses without refresh
2. No new PUT request received in time
3. Background cleanup goroutine detects expiration

### Cleanup Process

Background goroutine runs every 1 second:

```go
// Simplified cleanup logic
for {
    time.Sleep(1 * time.Second)

    now := time.Now()
    for name, register := range registers {
        if register.TTL.Before(now) {
            // Register expired
            delete(registers, name)
            notifyExpiration(name)
        }
    }
}
```

### Expiration Effects

When a register expires:

1. Removed from registry storage
2. No longer visible to consumers
3. Pending change requests are lost
4. Long-polling consumers receive empty response
5. Warning logged: "Register expired" (data loss)

### Handling Expiration (Consumer)

Consumer sees empty result after expiration:

```bash
# Before expiration
curl "http://localhost:8080/consumer?name=temperature"
# Returns: {"registers": {"temperature": {"value": 23.0}}}

# After expiration (TTL elapsed, no refresh)
curl "http://localhost:8080/consumer?name=temperature"
# Returns: {"registers": {}}
```

Client library behavior:

```go
values, _, _ := client.Consume(ctx, "temperature")

for value := range values {
    fmt.Printf("Temperature: %v\n", value.Value)
    // If register expires, no more values sent
    // Channel remains open waiting for re-creation
}
```

## Re-creation

### Starting Over

An expired register can be re-created by provider:

```bash
# Register expired, no longer exists

# Provider creates it again
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 22.5,
        "metadata": {"unit": "celsius"},
        "ttl": "10s"
      }
    }
  }'

# Register is now active again
```

From consumer perspective:

```go
// Consumer was waiting on long-poll
values, _, _ := client.Consume(ctx, "temperature")

// Register expired - no values for a while

// Provider re-creates register
// Consumer immediately receives new value
```

## Best Practices

### For Providers

**1. Choose appropriate TTL:**
```go
// Good: Reasonable TTL for device type
client.Provide(ctx, "sensor", value, nil, 30*time.Second)

// Bad: Too short (excessive refresh traffic)
client.Provide(ctx, "sensor", value, nil, 1*time.Second)

// Bad: Too long (delayed failure detection)
client.Provide(ctx, "sensor", value, nil, 1*time.Hour)
```

**2. Use automatic refresh (Go client):**
```go
// Client library handles refresh automatically
updates, reqs, _ := client.Provide(ctx, "temp", 22.5, nil, 10*time.Second)

// Just send updates when value changes
updates <- 23.0
```

**3. Handle network interruptions:**
```go
// Long-running provider
for {
    updates, reqs, err := client.Provide(ctx, "temp", currentValue, nil, ttl)
    if err != nil {
        log.Printf("Connection error: %v", err)
        time.Sleep(5 * time.Second)
        continue // Retry
    }

    // Normal operation
    for req := range reqs {
        // Handle requests
    }

    // If loop exits, reconnect
    log.Println("Reconnecting...")
}
```

**4. Graceful shutdown:**
```go
ctx, cancel := context.WithCancel(context.Background())

// On shutdown signal
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
go func() {
    <-sigChan
    cancel() // Stops providing, register will expire
}()

updates, reqs, _ := client.Provide(ctx, "temp", 22.5, nil, 10*time.Second)
```

### For Consumers

**1. Handle empty results:**
```go
values, _, _ := client.Consume(ctx, "temperature")

for value := range values {
    if value.Value == nil {
        log.Println("Register not available")
        continue
    }

    // Process value
    fmt.Printf("Temp: %v\n", value.Value)
}
```

**2. Detect absence:**
```go
values, _, _ := client.Consume(ctx, "device")

lastSeen := time.Now()

for value := range values {
    lastSeen = time.Now()
    // Process value
}

// In separate goroutine
ticker := time.NewTicker(5 * time.Second)
for range ticker.C {
    if time.Since(lastSeen) > 30*time.Second {
        log.Println("Device appears offline")
    }
}
```

**3. Retry on disconnection:**
```go
for {
    values, reqs, err := client.Consume(ctx, "sensor")
    if err != nil {
        log.Printf("Connection error: %v", err)
        time.Sleep(5 * time.Second)
        continue
    }

    for value := range values {
        // Process values
    }

    // Channel closed, reconnect
    log.Println("Reconnecting...")
}
```

## Failure Scenarios

### Provider Crash

```
Timeline:
  0s      10s     20s     30s
  |-------|-------|-------|
  ^       ^               ^
  Set     Crash       Expired

  TTL: 20s
  Crash at: 10s
  Expires at: 20s
```

Result:
- Register expires after TTL (20s from creation)
- Consumers see empty result after expiration
- No way to distinguish crash from intentional stop

### Network Partition

```
Provider     Registry     Consumer
    |            |            |
    | -- Set --> |            |
    |            | <- Read -- |
    |            |            |
    X [network] X            |
    |  partition |            |
    |            |            |
    |            | (expires)  |
    |            | <- Read -- |
    |            | -- {} ---> |
```

Result:
- Provider cannot refresh due to network partition
- Register expires after TTL
- Consumer sees empty result
- Provider must re-create after network recovers

### Split Brain Prevention

Registry prevents split brain by design:

- No distributed consensus needed
- Single registry instance is source of truth
- TTL expiration handles stale data automatically
- Last write wins for concurrent updates

## Monitoring

### Server-Side Logging

Registry logs important lifecycle events:

```
INFO  Register set: name=temperature value=22.5 ttl=10s
DEBUG Register read: name=temperature
DEBUG Change requested: name=temperature value=25.0
WARN  Register expired: name=temperature (data loss)
INFO  Graceful shutdown initiated
```

### Metrics to Track

Monitor these metrics for operational insight:

1. **Register count** - Number of active registers
2. **Creation rate** - Registers created per second
3. **Expiration rate** - Registers expired per second
4. **Update rate** - Provider updates per second
5. **Read rate** - Consumer reads per second
6. **Average TTL** - Typical TTL values in use
7. **Expiration ratio** - Expired / Created

High expiration ratio indicates:
- TTL too short for device capabilities
- Network reliability issues
- Provider crashes or failures

## Summary

Key lifecycle principles:

1. **TTL drives lifecycle** - Registers expire without refresh
2. **Provider responsible** - Must refresh before TTL expiration
3. **Automatic cleanup** - Registry removes expired registers
4. **Consumer resilient** - Should handle empty results gracefully
5. **Re-creation allowed** - Expired registers can be re-created
6. **Choose TTL wisely** - Balance failure detection vs. refresh traffic
