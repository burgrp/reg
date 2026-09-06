from __future__ import annotations

import asyncio
import os
import unittest
from collections.abc import Mapping
from typing import Any
from unittest.mock import patch

from reg_client import AsyncClient, Register, RegisterUpdate


class FakeWireClient:
    def __init__(self) -> None:
        self.initial_registers: dict[str, Register] = {}
        self.consumer_polls: asyncio.Queue[dict[str, Register]] = asyncio.Queue()
        self.all_polls: asyncio.Queue[dict[str, Register]] = asyncio.Queue()
        self.provider_polls: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self.consumer_poll_names: list[tuple[str, ...]] = []
        self.provider_poll_names: list[tuple[str, ...]] = []
        self.set_calls: list[tuple[str, Any, dict[str, Any], Any]] = []
        self.request_calls: list[tuple[str, Any]] = []
        self.active_consumer_polls = 0
        self.active_all_polls = 0
        self.active_provider_polls = 0

    async def get_registers(
        self, names: tuple[str, ...], wait: Any = None
    ) -> dict[str, Register]:
        if wait is None:
            if names:
                return {
                    name: self.initial_registers[name]
                    for name in names
                    if name in self.initial_registers
                }
            return dict(self.initial_registers)

        if names:
            self.consumer_poll_names.append(names)
            self.active_consumer_polls += 1
            try:
                return await self.consumer_polls.get()
            finally:
                self.active_consumer_polls -= 1

        self.active_all_polls += 1
        try:
            return await self.all_polls.get()
        finally:
            self.active_all_polls -= 1

    async def request_change(self, name: str, value: Any) -> None:
        self.request_calls.append((name, value))

    async def set_register(
        self,
        name: str,
        value: Any,
        metadata: Mapping[str, Any] | None = None,
        ttl: Any = None,
    ) -> None:
        self.set_calls.append((name, value, dict(metadata or {}), ttl))

    async def get_change_requests(
        self, names: tuple[str, ...], wait: Any = None
    ) -> dict[str, Any]:
        self.provider_poll_names.append(names)
        self.active_provider_polls += 1
        try:
            return await self.provider_polls.get()
        finally:
            self.active_provider_polls -= 1


class DeferredInitialWire(FakeWireClient):
    def __init__(self) -> None:
        super().__init__()
        self.initial_requests: list[asyncio.Future[dict[str, Register]]] = []

    async def get_registers(
        self, names: tuple[str, ...], wait: Any = None
    ) -> dict[str, Register]:
        if wait is None and names:
            future = asyncio.get_running_loop().create_future()
            self.initial_requests.append(future)
            return await future
        return await super().get_registers(names, wait)


class DeferredAllInitialWire(FakeWireClient):
    def __init__(self) -> None:
        super().__init__()
        self.initial_request: asyncio.Future[dict[str, Register]] | None = None

    async def get_registers(
        self, names: tuple[str, ...], wait: Any = None
    ) -> dict[str, Register]:
        if wait is None and not names:
            self.initial_request = asyncio.get_running_loop().create_future()
            return await self.initial_request
        return await super().get_registers(names, wait)


class BlockingWriteWire(FakeWireClient):
    def __init__(self) -> None:
        super().__init__()
        self.write_calls = 0
        self.active_writes = 0
        self.max_active_writes = 0
        self.release_initial = asyncio.Event()
        self.block_follow_up = asyncio.Event()

    async def set_register(
        self,
        name: str,
        value: Any,
        metadata: Mapping[str, Any] | None = None,
        ttl: Any = None,
    ) -> None:
        self.write_calls += 1
        self.active_writes += 1
        self.max_active_writes = max(self.max_active_writes, self.active_writes)
        try:
            if self.write_calls == 1:
                await self.release_initial.wait()
            else:
                await self.block_follow_up.wait()
        finally:
            self.active_writes -= 1


async def wait_until(predicate: Any, timeout: float = 0.5) -> None:
    async def wait() -> None:
        while not predicate():
            await asyncio.sleep(0)

    await asyncio.wait_for(wait(), timeout)


class AsyncClientTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.wire = FakeWireClient()
        self.client = AsyncClient(
            "http://registry.example",
            wire_client=self.wire,
            retry_delay=0,
        )

    async def asyncTearDown(self) -> None:
        await self.client.aclose()

    async def test_consume_reads_updates_and_requests_changes(self) -> None:
        self.wire.initial_registers["temp"] = Register(20, {"unit": "C"})
        subscription = self.client.consume("temp")

        self.assertEqual(
            await asyncio.wait_for(anext(subscription), 0.5),
            Register(20, {"unit": "C"}),
        )
        await wait_until(lambda: self.wire.active_consumer_polls == 1)
        self.wire.consumer_polls.put_nowait({"temp": Register(21, {"unit": "C"})})
        self.assertEqual(
            await asyncio.wait_for(anext(subscription), 0.5),
            Register(21, {"unit": "C"}),
        )

        await subscription.request(22)
        self.assertEqual(self.wire.request_calls, [("temp", 22)])
        await subscription.aclose()

    async def test_consume_reemits_same_value_after_register_is_missing(self) -> None:
        self.wire.initial_registers["temp"] = Register(20)
        subscription = self.client.consume("temp")

        self.assertEqual(await asyncio.wait_for(anext(subscription), 0.5), Register(20))
        await wait_until(lambda: self.wire.active_consumer_polls == 1)
        self.wire.consumer_polls.put_nowait({})
        await wait_until(lambda: self.wire.active_consumer_polls == 1)
        self.wire.consumer_polls.put_nowait({"temp": Register(20)})

        self.assertEqual(await asyncio.wait_for(anext(subscription), 0.5), Register(20))
        await subscription.aclose()

    async def test_consumer_poller_batches_names_and_restarts(self) -> None:
        first = self.client.consume("temp")
        second = self.client.consume("humidity")
        await wait_until(lambda: self.wire.active_consumer_polls == 1)

        self.assertEqual(set(self.wire.consumer_poll_names[0]), {"temp", "humidity"})
        await first.aclose()
        await second.aclose()
        self.assertEqual(self.wire.active_consumer_polls, 0)

        replacement = self.client.consume("pressure")
        await wait_until(lambda: self.wire.active_consumer_polls == 1)
        self.assertEqual(self.wire.consumer_poll_names[-1], ("pressure",))
        await replacement.aclose()

    async def test_same_name_consumers_share_initial_and_ignore_stale_result(
        self,
    ) -> None:
        wire = DeferredInitialWire()
        client = AsyncClient("http://registry.example", wire_client=wire)
        first = client.consume("temp")
        second = client.consume("temp")

        await wait_until(lambda: len(wire.initial_requests) > 0)
        self.assertEqual(len(wire.initial_requests), 1)
        await wait_until(lambda: wire.active_consumer_polls == 1)
        wire.consumer_polls.put_nowait({"temp": Register(2)})

        self.assertEqual(await asyncio.wait_for(anext(first), 0.5), Register(2))
        self.assertEqual(await asyncio.wait_for(anext(second), 0.5), Register(2))
        wire.initial_requests[0].set_result({"temp": Register(1)})
        await asyncio.sleep(0)
        await asyncio.sleep(0)

        with self.assertRaises(asyncio.TimeoutError):
            await asyncio.wait_for(anext(first), 0.01)
        with self.assertRaises(asyncio.TimeoutError):
            await asyncio.wait_for(anext(second), 0.01)

        await first.aclose()
        await second.aclose()
        await client.aclose()

    async def test_consume_all_emits_removals_and_recreated_values(self) -> None:
        self.wire.initial_registers["temp"] = Register(20)
        subscription = self.client.consume_all()

        self.assertEqual(
            await asyncio.wait_for(anext(subscription), 0.5),
            RegisterUpdate("temp", 20),
        )
        await wait_until(lambda: self.wire.active_all_polls == 1)
        self.wire.all_polls.put_nowait({})
        self.assertEqual(
            await asyncio.wait_for(anext(subscription), 0.5),
            RegisterUpdate("temp", removed=True),
        )
        await wait_until(lambda: self.wire.active_all_polls == 1)
        self.wire.all_polls.put_nowait({"temp": Register(20)})
        self.assertEqual(
            await asyncio.wait_for(anext(subscription), 0.5),
            RegisterUpdate("temp", 20),
        )
        await subscription.aclose()

    async def test_consume_all_ignores_a_stale_initial_snapshot(self) -> None:
        wire = DeferredAllInitialWire()
        client = AsyncClient("http://registry.example", wire_client=wire)
        subscription = client.consume_all()

        await wait_until(lambda: wire.initial_request is not None)
        await wait_until(lambda: wire.active_all_polls == 1)
        wire.all_polls.put_nowait({"temp": Register(2)})
        self.assertEqual(
            await asyncio.wait_for(anext(subscription), 0.5),
            RegisterUpdate("temp", 2),
        )
        assert wire.initial_request is not None
        wire.initial_request.set_result({"temp": Register(1)})
        await asyncio.sleep(0)
        await asyncio.sleep(0)

        with self.assertRaises(asyncio.TimeoutError):
            await asyncio.wait_for(anext(subscription), 0.01)

        await subscription.aclose()
        await client.aclose()

    async def test_provide_sets_updates_refreshes_and_receives_requests(self) -> None:
        provider = await self.client.provide("temp", 20, {"unit": "C"}, ttl=0.04)
        self.assertEqual(self.wire.set_calls[0], ("temp", 20, {"unit": "C"}, "0.04s"))

        await wait_until(lambda: self.wire.active_provider_polls == 1)
        self.wire.provider_polls.put_nowait({"temp": None})
        self.assertIsNone(await asyncio.wait_for(anext(provider), 0.5))

        await provider.update(21)
        self.assertEqual(self.wire.set_calls[-1][1], 21)
        await wait_until(lambda: len(self.wire.set_calls) >= 3)
        self.assertEqual(self.wire.set_calls[-1][1], 21)
        await provider.aclose()
        self.assertEqual(self.wire.active_provider_polls, 0)

    async def test_rejects_duplicate_provider_and_allows_replacement_after_close(
        self,
    ) -> None:
        first = await self.client.provide("temp", 20, ttl="1h")
        with self.assertRaisesRegex(RuntimeError, "already has an active provider"):
            await self.client.provide("temp", 21, ttl="1h")

        await first.aclose()
        second = await self.client.provide("temp", 21, ttl="1h")
        await second.aclose()

    async def test_serializes_and_cancels_provider_writes(self) -> None:
        wire = BlockingWriteWire()
        client = AsyncClient("http://registry.example", wire_client=wire)
        provide_task = asyncio.create_task(client.provide("temp", 1, ttl=0.02))
        await wait_until(lambda: wire.active_writes == 1)
        wire.release_initial.set()
        provider = await provide_task

        update_task = asyncio.create_task(provider.update(2))
        await wait_until(lambda: wire.write_calls == 2)
        await asyncio.sleep(0.04)
        self.assertEqual(wire.active_writes, 1)
        self.assertEqual(wire.max_active_writes, 1)

        await provider.aclose()
        with self.assertRaises(asyncio.CancelledError):
            await update_task
        self.assertEqual(wire.active_writes, 0)

        replacement_task = asyncio.create_task(client.provide("temp", 3, ttl="1h"))
        await wait_until(lambda: wire.write_calls == 3)
        wire.block_follow_up.set()
        replacement = await replacement_task
        await replacement.aclose()
        await client.aclose()

    async def test_context_managers_close_subscriptions_and_client(self) -> None:
        async with self.client.consume("temp") as consumer:
            self.assertFalse(consumer.closed)
        self.assertTrue(consumer.closed)

        async with await self.client.provide("temp", 20, ttl="1h") as provider:
            self.assertFalse(provider.closed)
        self.assertTrue(provider.closed)

        await self.client.aclose()
        with self.assertRaisesRegex(RuntimeError, "client is closed"):
            self.client.consume("temp")

    async def test_from_env_requires_registry_url(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "REGISTRY"):
                AsyncClient.from_env()

    async def test_rejects_invalid_retry_delays(self) -> None:
        for value in (-1, float("nan"), float("inf"), True, "1"):
            with self.subTest(value=value):
                with self.assertRaisesRegex(ValueError, "retry_delay"):
                    AsyncClient(
                        "http://registry.example",
                        wire_client=self.wire,
                        retry_delay=value,  # type: ignore[arg-type]
                    )


if __name__ == "__main__":
    unittest.main()
