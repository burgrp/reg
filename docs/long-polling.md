# Long Polling

Long polling enables efficient real-time updates without the complexity of WebSockets.

## What is Long Polling?

Long polling is a technique where the client makes a request, and the server holds the connection open until:
1. New data is available (returns immediately)
2. Timeout expires (returns with current state)
3. Server is shutting down (returns immediately)

This provides near-real-time updates with standard HTTP, avoiding the need for WebSocket infrastructure.

## How Registry Uses Long Polling

Registry uses long polling for two scenarios:

1. **Consumers waiting for value changes** - GET /consumer with wait parameter
2. **Providers waiting for change requests** - GET /provider with wait parameter

## Consumer Long Polling

### Without Long Polling (Inefficient)

```bash
# Client polls every second
while true; do
  curl "http://localhost:8080/consumer?name=temperature"
  sleep 1
done
```

Problems:
- Wastes bandwidth (most requests return unchanged data)
- Adds server load (constant request processing)
- Higher latency (up to 1 second delay before seeing changes)

### With Long Polling (Efficient)

```bash
# Client uses long polling
while true; do
  curl "http://localhost:8080/consumer?name=temperature&wait=5s"
done
```

Benefits:
- Minimal bandwidth (only returns when data changes or timeout)
- Lower server load (one request per 5 seconds max)
- Lower latency (immediate notification on changes)

## Implementation Details

### Server Behavior

When a long-poll request arrives:

1. **Check current state**
   - If requested registers have pending changes, return immediately
   - If no changes, proceed to step 2

2. **Register listener**
   - Add callback to notification system
   - Callback will be invoked when any requested register changes

3. **Wait for notification or timeout**
   - Block until notification arrives OR wait duration expires
   - Use select with timeout channel

4. **Return response**
   - Gather current state of requested registers
   - Remove listener
   - Send HTTP response

### Notification System

The registry core uses a generic listener pattern:

```go
type Listeners[T any] struct {
    mu        sync.RWMutex
    listeners map[int]func(T)
    nextID    int
}
```

When a register value changes:
1. Core calls `valueChangeListeners.Notify(names)`
2. All registered listeners receive notification
3. Listeners wake up their waiting HTTP handlers
4. Handlers return responses to clients

### Thread Safety

Long polling is thread-safe through:
- Read-write locks for register access
- Separate mutex for listener management
- Channel-based notification to avoid deadlocks

## Consumer Long Polling Details

### Basic Usage

```bash
curl "http://localhost:8080/consumer?name=temp&wait=5s"
```

### Multiple Registers

Wait for any of multiple registers to change:

```bash
curl "http://localhost:8080/consumer?name=temp&name=humidity&wait=5s"
```

Returns when ANY of the requested registers changes.

### Default Behavior (No Wait)

Without wait parameter, returns immediately:

```bash
curl "http://localhost:8080/consumer?name=temp"
```

### Response on Timeout

If no changes occur within wait duration:

```json
{
  "registers": {
    "temp": {
      "value": 22.5,
      "metadata": {"unit": "celsius"}
    }
  }
}
```

Returns current state (same as if change occurred).

### Response on Change

If register changes during wait:

```json
{
  "registers": {
    "temp": {
      "value": 23.0,
      "metadata": {"unit": "celsius"}
    }
  }
}
```

Returns immediately with new state.

## Provider Long Polling Details

### Basic Usage

```bash
curl "http://localhost:8080/provider?name=temp&wait=30s"
```

### Polling for Change Requests

Wait for consumers to request changes:

```bash
# Provider polls with long wait
curl "http://localhost:8080/provider?name=thermostat&wait=30s"
```

If consumer requests change during wait:

```json
{
  "registers": {
    "thermostat": {
      "value": {"mode": "heating", "target": 22}
    }
  }
}
```

### Consuming Requests

Important: Each request is consumed on read:

```bash
# First poll - returns request
curl "http://localhost:8080/provider?name=temp"
# Response: {"registers": {"temp": {"value": 25}}}

# Second poll - request already consumed
curl "http://localhost:8080/provider?name=temp"
# Response: {"registers": {}}
```

Only one provider receives each change request.

### Response on Timeout

If no change requests within wait duration:

```json
{
  "registers": {}
}
```

Returns empty map (no pending requests).

## Best Practices

### Choosing Wait Duration

**Consumers:**
- Typical: 5 seconds
- Reasons:
  - Short enough to retry quickly on network issues
  - Long enough to reduce HTTP overhead
  - Good balance between latency and resource usage

**Providers:**
- Typical: 30 seconds
- Reasons:
  - Change requests are less frequent
  - Longer waits reduce provider resource usage
  - Still responsive enough for interactive control

### Loop Pattern

Proper long-polling loop:

```bash
while true; do
  # Long poll with error handling
  if ! curl -f "http://localhost:8080/consumer?name=temp&wait=5s"; then
    # On error, back off before retry
    sleep 1
  fi
done
```

Key points:
- Immediately start next request after previous completes
- Add backoff on errors
- No additional sleep needed (wait parameter handles timing)

### Error Handling

Handle connection failures gracefully:

```bash
while true; do
  response=$(curl -s -w "\n%{http_code}" \
    "http://localhost:8080/consumer?name=temp&wait=5s")

  http_code=$(echo "$response" | tail -n1)
  body=$(echo "$response" | head -n-1)

  if [ "$http_code" = "200" ]; then
    # Process response
    echo "$body"
  else
    # Back off on error
    echo "Error: $http_code"
    sleep 5
  fi
done
```

### Timeout Considerations

**Client timeout must exceed server wait:**

```bash
# Server wait: 5s
# Client timeout: 10s (safe margin)
curl --max-time 10 "http://localhost:8080/consumer?wait=5s"
```

If client timeout is too short, connection closes prematurely.

### Handling Server Shutdown

During graceful shutdown, server returns immediately:

```json
{
  "registers": {
    "temp": {
      "value": 22.5,
      "metadata": {"unit": "celsius"}
    }
  }
}
```

Client should retry (server may be restarting).

## Performance Characteristics

### Resource Usage

**Without Long Polling (1s poll interval):**
- Requests per minute: 60
- Bandwidth: ~60 * response_size
- Server load: 60 request handlers

**With Long Polling (5s wait):**
- Requests per minute: 12 (worst case, if no changes)
- Bandwidth: ~12 * response_size (or less if changes occur)
- Server load: ~12 concurrent handlers

**Savings: 80% reduction in requests, bandwidth, and load**

### Latency

**Without Long Polling (1s interval):**
- Average latency: 500ms
- Max latency: 1000ms

**With Long Polling:**
- Average latency: <100ms
- Max latency: network round-trip time

**Result: ~5-10x improvement in latency**

### Scalability

Long polling scales well:
- Each waiting request holds one goroutine (lightweight)
- No broadcast infrastructure needed
- Simple HTTP load balancing works
- Tens of thousands of concurrent waiters per server

## Comparison with Alternatives

### Long Polling vs. Regular Polling

| Aspect | Regular Polling | Long Polling |
|--------|----------------|--------------|
| Latency | High (poll interval / 2) | Low (immediate) |
| Bandwidth | High (constant) | Low (on-change) |
| Server Load | High (constant) | Low (on-change) |
| Complexity | Simple | Simple |
| Infrastructure | HTTP only | HTTP only |

### Long Polling vs. WebSockets

| Aspect | Long Polling | WebSockets |
|--------|--------------|------------|
| Latency | Low | Very Low |
| Bandwidth | Low | Very Low |
| Server Load | Low | Low |
| Complexity | Simple | Moderate |
| Infrastructure | HTTP only | HTTP + WS support |
| Firewall friendly | Yes | Sometimes blocked |
| Load balancing | Easy | Requires sticky sessions |
| Reconnection | Automatic | Manual handling |

Long polling is often the pragmatic choice for IoT systems:
- Works everywhere HTTP works
- Simpler to deploy and debug
- Adequate latency for most IoT use cases
- No special load balancer configuration

## Example: Complete Long-Polling Client

### Consumer in Bash

```bash
#!/bin/bash

REGISTRY="http://localhost:8080"
REGISTER="temperature"

echo "Monitoring $REGISTER..."

while true; do
  response=$(curl -s -f "$REGISTRY/consumer?name=$REGISTER&wait=5s")

  if [ $? -eq 0 ]; then
    # Parse and display value
    value=$(echo "$response" | jq -r ".registers.$REGISTER.value")
    echo "$(date '+%H:%M:%S') - $REGISTER: $value"
  else
    echo "$(date '+%H:%M:%S') - Connection error, retrying..."
    sleep 2
  fi
done
```

### Provider in Bash

```bash
#!/bin/bash

REGISTRY="http://localhost:8080"
REGISTER="thermostat"

# Function to update register
update_register() {
  local value=$1
  curl -s -X PUT "$REGISTRY/provider" \
    -H "Content-Type: application/json" \
    -d "{
      \"registers\": {
        \"$REGISTER\": {
          \"value\": $value,
          \"metadata\": {\"location\": \"room1\"},
          \"ttl\": \"30s\"
        }
      }
    }" > /dev/null
}

# Set initial value
current_temp=20
update_register "{\"mode\": \"off\", \"target\": $current_temp}"

echo "Polling for change requests..."

while true; do
  response=$(curl -s -f "$REGISTRY/provider?name=$REGISTER&wait=30s")

  if [ $? -eq 0 ]; then
    # Check if change request received
    requested=$(echo "$response" | jq -r ".registers.$REGISTER.value // empty")

    if [ -n "$requested" ]; then
      echo "$(date '+%H:%M:%S') - Change requested: $requested"

      # Accept change and update
      update_register "$requested"
      echo "$(date '+%H:%M:%S') - Change applied"
    fi
  else
    echo "$(date '+%H:%M:%S') - Connection error, retrying..."
    sleep 2
  fi
done
```

These examples demonstrate proper long-polling loops with error handling and backoff.
