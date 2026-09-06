# reg-client

Async Python 3.10+ client for the [reg](../../README.md) IoT registry. It has a
typed low-level REST API and a reactive high-level API with shared long-polling,
automatic TTL refresh, and deterministic async cleanup.

## Installation

From the repository root:

```bash
python -m pip install ./clients/python
```

For development tools:

```bash
python -m pip install -e './clients/python[dev]'
```

## Consume One Register

`consume()` returns an async iterator and async context manager. The first item
is the current register value when it exists; later items are changed values.

```python
import asyncio

from reg_client import Client


async def main() -> None:
    async with Client("http://localhost:8080") as client:
        async with client.consume("temperature") as subscription:
            async for register in subscription:
                print(register.value, register.metadata)

                if register.value < 20:
                    await subscription.request(22)


asyncio.run(main())
```

Multiple `consume()` subscriptions share one batched consumer long-poll.
Closing the last subscription cancels that request immediately.

## Provide a Register

`provide()` publishes the initial value before returning. Its subscription is
an async iterator of consumer change requests. The current value and TTL are
refreshed every half TTL until the subscription closes.

```python
import asyncio
from datetime import timedelta

from reg_client import Client


async def main() -> None:
    async with Client("http://localhost:8080") as client:
        provider = await client.provide(
            "temperature",
            21.5,
            {"unit": "celsius"},
            ttl=timedelta(seconds=10),
        )

        async with provider:
            async for requested_value in provider:
                if 10 <= requested_value <= 35:
                    await provider.update(requested_value)


asyncio.run(main())
```

Multiple providers share one batched provider long-poll. A client instance
allows only one active provider for each register name.

## Consume All Registers

`consume_all()` yields `RegisterUpdate` objects. Expired registers are reported
with `removed=True` and may later be emitted again if recreated.

```python
async with client.consume_all() as subscription:
    async for update in subscription:
        if update.removed:
            print(update.name, "expired")
        else:
            print(update.name, update.value, update.metadata)

        # A change request can target any register.
        # await subscription.request(update.name, new_value)
```

## Configuration

The high-level constructor accepts seconds, `datetime.timedelta`, or a positive
duration string using `ms`, `s`, `m`, and `h` components.

```python
client = Client(
    "http://localhost:8080",
    consumer_poll_interval=5,
    provider_poll_interval="30s",
    retry_delay=1,
)
```

`Client.from_env()` reads the base URL from `REGISTRY`:

```python
async with Client.from_env() as client:
    ...
```

Polling retries transient HTTP errors after `retry_delay`. Explicit operations
such as `request()`, `update()`, and the initial `provide()` propagate errors to
the caller.

## Low-Level REST API

`AsyncRestClient` exposes the wire operations directly:

```python
from reg_client import AsyncRestClient

async with AsyncRestClient("http://localhost:8080") as wire:
    registers = await wire.get_registers(["temperature", "humidity"])
    await wire.request_change("temperature", 22)
    await wire.set_register("temperature", 21.5, {"unit": "celsius"}, ttl="10s")
    requests = await wire.get_change_requests(["temperature"], wait="30s")
```

Non-matching HTTP statuses raise `RegistryError`. Invalid JSON response shapes
raise `RegistryProtocolError`. An injected `httpx.AsyncClient` remains owned by
the caller and is not closed with `AsyncRestClient`.

## Tests

```bash
python -m unittest discover -s clients/python/test -v
ruff check clients/python
ruff format --check clients/python
mypy clients/python/src
```
