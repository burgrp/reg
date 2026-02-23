# @burgrp/reg – JavaScript Client

JavaScript client library for the [reg](../../README.md) IoT registry.
Implements the same Provider/Consumer model as the Go client, with a
two-layer architecture and full batching support.

Requires **Node.js 18+** (uses native `fetch` and `node:test`).

## Installation

```bash
# From the clients/js directory
npm install
```

## Architecture

```
src/
  wire/
    consumer.js   – Low-level HTTP client for /consumer endpoint
    provider.js   – Low-level HTTP client for /provider endpoint
    index.js      – Wire layer exports
  client.js       – High-level Client with batching and TTL refresh
  index.js        – Main exports
test/
  wire/
    consumer.test.js
    provider.test.js
  client.test.js
```

### Wire Layer (`src/wire/`)

Thin wrappers around the REST API with no reactive behaviour:

| Class            | Methods                                    |
|------------------|--------------------------------------------|
| `ConsumerClient` | `getRegisters(names?, wait?)`, `requestChanges(changes)` |
| `ProviderClient` | `setRegisters(registers)`, `getChangeRequests(names, wait?)` |

Both constructors accept an optional custom `fetch` function for testing.

### High-Level Client (`src/client.js`)

Event-driven, with batching optimisation:

* Multiple `consume()` subscriptions share a **single** long-poll request.
* Multiple `provide()` subscriptions share a **single** change-request poll.
* Providers automatically refresh their TTL at `ttl / 2` to prevent expiry.
* Change detection avoids redundant events when values are unchanged.

## Quick Start

```js
import { Client } from '@burgrp/reg'

const client = new Client('http://localhost:8080')
```

Or using the `REGISTRY` environment variable:

```js
import { createClientFromEnv } from '@burgrp/reg'

const client = createClientFromEnv()
```

## API

### `consume(name)` → `ConsumerSubscription`

Subscribe to a register. Immediately emits the current value, then emits
again on every change detected by long-polling.

```js
const sub = client.consume('temperature')

sub.on('value', ({ value, metadata }) => {
  console.log('temperature:', value, metadata)
})

// Request a value change from the provider
sub.request(22.0)

// Stop receiving updates
sub.stop()
```

**Events:**
* `'value'` – `{ value, metadata }` – emitted on initial fetch and each change

---

### `consumeAll()` → `ConsumeAllSubscription`

Subscribe to all registers. Emits one `'update'` event per register on
initial fetch, then emits for each changed register on every poll.

```js
const sub = client.consumeAll()

sub.on('update', ({ name, value, metadata }) => {
  console.log(name, '=', value)
})

sub.stop()
```

**Events:**
* `'update'` – `{ name, value, metadata }` – emitted for each changed register

---

### `async provide(name, value, metadata?, ttl?)` → `ProviderSubscription`

Publish a register and keep it alive. Sets the register immediately, then
continuously refreshes the TTL so it does not expire while the provider is
running.

```js
const pub = await client.provide(
  'temperature',
  21.5,
  { unit: 'celsius', location: 'living-room' },
  '5s'       // TTL – default "5s"
)

// React to consumer change requests
pub.on('change', async requestedValue => {
  // validate / accept / modify, then update
  await pub.update(requestedValue)
})

// Push a new value at any time
await pub.update(22.0)

// Stop providing (register will expire after TTL)
pub.stop()
```

**Events:**
* `'change'` – `value` – emitted when a consumer requests a value change

---

## Constructor Options

```js
const client = new Client('http://localhost:8080', {
  fetch: customFetchFn,          // default: globalThis.fetch
  consumerPollInterval: 5000,    // ms, default: 5000
  providerPollInterval: 30000,   // ms, default: 30000
})
```

## REST API Contract

| Method | Endpoint    | Description                                  |
|--------|-------------|----------------------------------------------|
| GET    | `/consumer` | Read registers; `?name=X&name=Y&wait=5s`     |
| PUT    | `/consumer` | Request value changes (202 Accepted)         |
| PUT    | `/provider` | Set registers with value, metadata, TTL      |
| GET    | `/provider` | Poll for consumer change requests; `?name=X&wait=30s` |

**Request / Response format (provider PUT):**
```json
{
  "registers": {
    "temperature": {
      "value": 21.5,
      "metadata": { "unit": "celsius" },
      "ttl": "5s"
    }
  }
}
```

## Running Tests

```bash
npm test
```

Tests use the Node.js built-in `node:test` runner – no extra dependencies
needed. The fetch function is injected via constructor, so no real server
is required.
