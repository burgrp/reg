# REST API Reference

Complete HTTP API reference for the Registry service.

## Base URL

The default server runs on `http://localhost:8080`. All endpoints are relative to this base URL.

## Common Concepts

### Register Structure

A register consists of:
- **name** (string) - Unique identifier for the register
- **value** (any JSON type) - Current value (number, string, object, array, etc.)
- **metadata** (object) - Optional key-value pairs with additional information
- **ttl** (duration string) - Time-to-live in Go duration format

### Duration Format

TTL values use Go duration strings:
- `5s` - 5 seconds
- `10m` - 10 minutes
- `1h30m` - 1 hour and 30 minutes
- `500ms` - 500 milliseconds

### Response Codes

- `200 OK` - Successful GET request
- `202 Accepted` - Successful PUT request (consumer change request)
- `204 No Content` - Successful PUT request (provider update)
- `400 Bad Request` - Invalid JSON or parameters
- `405 Method Not Allowed` - Incorrect HTTP method
- `500 Internal Server Error` - Server error

## Provider Endpoints

Providers publish register values and poll for consumer change requests.

### Set Register Values

Update or create registers with values, metadata, and TTL.

```
PUT /provider
Content-Type: application/json
```

**Request Body:**

```json
{
  "registers": {
    "temperature": {
      "value": 22.5,
      "metadata": {
        "unit": "celsius",
        "location": "room1"
      },
      "ttl": "10s"
    },
    "humidity": {
      "value": 60.0,
      "metadata": {
        "unit": "percent"
      },
      "ttl": "10s"
    }
  }
}
```

**Fields:**
- `registers` (required) - Map of register names to register data
  - `value` (required) - Any JSON-serializable value
  - `metadata` (optional) - Object with string keys
  - `ttl` (optional) - Duration string, defaults to 10 seconds if omitted

**Response:**
- Status: `204 No Content`

**Example:**

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

### Poll for Change Requests

Long-poll for consumer change requests.

```
GET /provider?name=<name>&wait=<duration>
```

**Query Parameters:**
- `name` (optional, repeatable) - Register names to poll for. If omitted, polls all registers.
- `wait` (optional) - Long-poll duration (e.g., "30s"). If omitted, returns immediately.

**Response Body:**

```json
{
  "registers": {
    "temperature": {
      "value": 25.0
    }
  }
}
```

**Response:**
- Status: `200 OK`
- Body: Map of register names to requested values
- Empty map if no change requests pending

**Behavior:**
- Returns immediately if change requests are pending
- If `wait` specified and no requests pending, blocks until:
  - A change request arrives
  - Wait duration expires
  - Server shutdown begins
- Consuming the response removes requests from the queue (one consumer per request)

**Example:**

```bash
# Poll for any change requests, wait up to 30 seconds
curl "http://localhost:8080/provider?wait=30s"

# Poll for specific registers
curl "http://localhost:8080/provider?name=temperature&name=humidity&wait=30s"
```

## Consumer Endpoints

Consumers read register values and request changes.

### Read Register Values

Read current register values with optional long polling.

```
GET /consumer?name=<name>&wait=<duration>
```

**Query Parameters:**
- `name` (optional, repeatable) - Register names to read. If omitted, returns all registers.
- `wait` (optional) - Long-poll duration (e.g., "5s"). If omitted, returns immediately.

**Response Body:**

```json
{
  "registers": {
    "temperature": {
      "value": 22.5,
      "metadata": {
        "unit": "celsius",
        "location": "room1"
      }
    },
    "humidity": {
      "value": 60.0,
      "metadata": {
        "unit": "percent"
      }
    }
  }
}
```

**Response:**
- Status: `200 OK`
- Body: Map of register names to values and metadata
- Empty map if no matching registers exist

**Behavior:**
- Returns immediately with current values if no `wait` parameter
- If `wait` specified, blocks until:
  - Any requested register value changes
  - Wait duration expires
  - Server shutdown begins

**Example:**

```bash
# Read current values immediately
curl "http://localhost:8080/consumer?name=temperature"

# Read multiple registers
curl "http://localhost:8080/consumer?name=temperature&name=humidity"

# Long-poll for changes (wait up to 5 seconds)
curl "http://localhost:8080/consumer?name=temperature&wait=5s"

# Read all registers
curl "http://localhost:8080/consumer"
```

### Request Value Changes

Request that a provider change a register value.

```
PUT /consumer
Content-Type: application/json
```

**Request Body:**

```json
{
  "registers": {
    "temperature": {
      "value": 25.0
    },
    "mode": {
      "value": "cooling"
    }
  }
}
```

**Fields:**
- `registers` (required) - Map of register names to change requests
  - `value` (required) - Desired value (any JSON type)

**Response:**
- Status: `202 Accepted`

**Behavior:**
- Queues the change request for the provider to poll
- Only the most recent request per register is kept (newer requests overwrite older ones)
- No guarantee the provider will accept the change
- Providers must poll `/provider` to receive requests
- Asynchronous operation (no immediate response about acceptance)

**Example:**

```bash
curl -X PUT http://localhost:8080/consumer \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 25.0
      }
    }
  }'
```

## Query Parameter Formats

Both `name` and `names` parameters are supported:

```bash
# Multiple name parameters
curl "http://localhost:8080/consumer?name=temp&name=humidity"

# Comma-separated names parameter
curl "http://localhost:8080/consumer?names=temp,humidity"

# Mixed (both are merged)
curl "http://localhost:8080/consumer?name=temp&names=humidity,pressure"
```

## Error Responses

### 400 Bad Request

Invalid JSON or malformed parameters:

```json
{
  "error": "invalid character 'x' looking for beginning of value"
}
```

### 405 Method Not Allowed

Wrong HTTP method for endpoint:

```json
{
  "error": "Method not allowed"
}
```

### 500 Internal Server Error

Server-side error (check logs):

```json
{
  "error": "internal error description"
}
```

## Long Polling Details

See [Long Polling](long-polling.md) for detailed behavior and best practices.

## Examples

### Complete Provider/Consumer Flow

**1. Provider sets initial value:**

```bash
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "thermostat": {
        "value": {"mode": "off", "target": 20},
        "metadata": {"location": "living-room"},
        "ttl": "30s"
      }
    }
  }'
```

**2. Consumer reads value:**

```bash
curl "http://localhost:8080/consumer?name=thermostat"
```

Response:
```json
{
  "registers": {
    "thermostat": {
      "value": {"mode": "off", "target": 20},
      "metadata": {"location": "living-room"}
    }
  }
}
```

**3. Consumer requests change:**

```bash
curl -X PUT http://localhost:8080/consumer \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "thermostat": {
        "value": {"mode": "heating", "target": 22}
      }
    }
  }'
```

**4. Provider polls for requests:**

```bash
curl "http://localhost:8080/provider?name=thermostat&wait=30s"
```

Response:
```json
{
  "registers": {
    "thermostat": {
      "value": {"mode": "heating", "target": 22}
    }
  }
}
```

**5. Provider accepts and updates:**

```bash
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "thermostat": {
        "value": {"mode": "heating", "target": 22},
        "metadata": {"location": "living-room"},
        "ttl": "30s"
      }
    }
  }'
```

**6. Consumer sees updated value:**

```bash
curl "http://localhost:8080/consumer?name=thermostat&wait=5s"
```

Response:
```json
{
  "registers": {
    "thermostat": {
      "value": {"mode": "heating", "target": 22},
      "metadata": {"location": "living-room"}
    }
  }
}
```

## Rate Limits and Best Practices

### Provider Best Practices

- Use reasonable TTL values (10-60 seconds for typical IoT devices)
- Refresh registers before TTL expires to maintain presence
- Poll for change requests with 30-second wait times
- Validate change requests before accepting
- Update register after accepting change request

### Consumer Best Practices

- Use long polling (5-second wait) instead of rapid polling
- Request reasonable changes that the provider can fulfill
- Monitor actual register values to see if changes were accepted
- Handle cases where providers reject or ignore change requests

### Avoiding Common Pitfalls

- Don't set TTL too short (causes excessive refresh traffic)
- Don't set TTL too long (delays cleanup of failed devices)
- Don't poll without wait parameter in a tight loop (wasteful)
- Don't assume change requests will be accepted (always verify)
