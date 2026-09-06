from __future__ import annotations

import asyncio
import math
import os
from collections.abc import AsyncIterator, Mapping
from dataclasses import dataclass, field
from typing import Any, Protocol, TypeVar, cast

from .duration import Duration, normalize_duration
from .models import Register, RegisterUpdate
from .wire import AsyncRestClient

_STOP = object()
_T = TypeVar("_T")


class _WireClient(Protocol):
    async def get_registers(
        self, names: tuple[str, ...], wait: Duration | None = None
    ) -> dict[str, Register]: ...

    async def request_change(self, name: str, value: Any) -> None: ...

    async def set_register(
        self,
        name: str,
        value: Any,
        metadata: Mapping[str, Any] | None = None,
        ttl: Duration | None = None,
    ) -> None: ...

    async def get_change_requests(
        self, names: tuple[str, ...], wait: Duration | None = None
    ) -> dict[str, Any]: ...


class _Subscription(AsyncIterator[_T]):
    def __init__(self) -> None:
        self._queue: asyncio.Queue[_T | object] = asyncio.Queue()
        self._closed = False

    @property
    def closed(self) -> bool:
        return self._closed

    def __aiter__(self) -> _Subscription[_T]:
        return self

    async def __anext__(self) -> _T:
        if self._closed and self._queue.empty():
            raise StopAsyncIteration
        item = await self._queue.get()
        if item is _STOP:
            raise StopAsyncIteration
        return cast(_T, item)

    def _push(self, item: _T) -> None:
        if not self._closed:
            self._queue.put_nowait(item)

    def _finish(self) -> None:
        if self._closed:
            return
        self._closed = True
        while not self._queue.empty():
            self._queue.get_nowait()
        self._queue.put_nowait(_STOP)

    async def aclose(self) -> None:
        raise NotImplementedError


class ConsumerSubscription(_Subscription[Register]):
    def __init__(self, client: AsyncClient, name: str) -> None:
        super().__init__()
        self._client = client
        self.name = name
        self._has_value = False

    def _push_register(self, register: Register) -> None:
        self._has_value = True
        self._push(register)

    async def request(self, value: Any) -> None:
        if self.closed:
            raise RuntimeError("consumer subscription is closed")
        await self._client._request_change(self.name, value)

    async def aclose(self) -> None:
        if self.closed:
            return
        tasks = self._client._remove_consumer(self)
        self._finish()
        await _cancel_and_wait(*tasks)

    async def __aenter__(self) -> ConsumerSubscription:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()


class ConsumeAllSubscription(_Subscription[RegisterUpdate]):
    def __init__(self, client: AsyncClient) -> None:
        super().__init__()
        self._client = client
        self._last_values: dict[str, Register] = {}

    def _has_register(self, name: str, register: Register) -> bool:
        return self._last_values.get(name) == register

    def _has_register_name(self, name: str) -> bool:
        return name in self._last_values

    def _push_update(
        self, update: RegisterUpdate, register: Register | None = None
    ) -> None:
        if update.removed:
            self._last_values.pop(update.name, None)
        elif register is not None:
            self._last_values[update.name] = register
        self._push(update)

    async def request(self, name: str, value: Any) -> None:
        if self.closed:
            raise RuntimeError("consume-all subscription is closed")
        await self._client._request_change(name, value)

    async def aclose(self) -> None:
        if self.closed:
            return
        tasks = self._client._remove_consume_all(self)
        self._finish()
        await _cancel_and_wait(*tasks)

    async def __aenter__(self) -> ConsumeAllSubscription:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()


class ProviderSubscription(_Subscription[Any]):
    def __init__(self, client: AsyncClient, name: str) -> None:
        super().__init__()
        self._client = client
        self.name = name

    async def update(self, value: Any) -> None:
        if self.closed:
            raise RuntimeError("provider subscription is closed")
        await self._client._update_provider(self, value)

    async def aclose(self) -> None:
        if self.closed:
            return
        tasks = self._client._remove_provider(self)
        self._finish()
        try:
            await _cancel_and_wait(*tasks)
        finally:
            self._client._finish_provider_close(self.name)

    async def __aenter__(self) -> ProviderSubscription:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()


@dataclass(slots=True)
class _ProviderState:
    subscription: ProviderSubscription
    value: Any
    metadata: dict[str, Any]
    ttl: str
    ttl_seconds: float
    refresh_task: asyncio.Task[None] | None = None
    write_lock: asyncio.Lock = field(default_factory=asyncio.Lock)
    write_tasks: set[asyncio.Task[None]] = field(default_factory=set)


class AsyncClient:
    def __init__(
        self,
        base_url: str,
        *,
        wire_client: _WireClient | None = None,
        consumer_poll_interval: Duration = 5,
        provider_poll_interval: Duration = 30,
        retry_delay: float = 1.0,
    ) -> None:
        self._wire: _WireClient = wire_client or AsyncRestClient(base_url)
        self._owns_wire = wire_client is None
        self._consumer_poll_interval = normalize_duration(consumer_poll_interval)[0]
        self._provider_poll_interval = normalize_duration(provider_poll_interval)[0]
        if (
            isinstance(retry_delay, bool)
            or not isinstance(retry_delay, (int, float))
            or not math.isfinite(retry_delay)
            or retry_delay < 0
        ):
            raise ValueError("retry_delay must be a finite non-negative number")
        self._retry_delay = float(retry_delay)
        self._closed = False

        self._consumer_subscriptions: dict[str, set[ConsumerSubscription]] = {}
        self._consumer_last: dict[str, Register] = {}
        self._consumer_revisions: dict[str, int] = {}
        self._consumer_initial_tasks: dict[str, asyncio.Task[None]] = {}
        self._consumer_task: asyncio.Task[None] | None = None

        self._consume_all_subscriptions: set[ConsumeAllSubscription] = set()
        self._consume_all_last: dict[str, Register] = {}
        self._consume_all_revision = 0
        self._consume_all_initialized = False
        self._consume_all_initial_task: asyncio.Task[None] | None = None
        self._consume_all_task: asyncio.Task[None] | None = None

        self._providers: dict[str, _ProviderState] = {}
        self._provider_closing: set[str] = set()
        self._provider_task: asyncio.Task[None] | None = None

    @classmethod
    def from_env(cls, **kwargs: Any) -> AsyncClient:
        base_url = os.environ.get("REGISTRY", "").strip()
        if not base_url:
            raise RuntimeError("REGISTRY environment variable is not set")
        return cls(base_url, **kwargs)

    def consume(self, name: str) -> ConsumerSubscription:
        self._ensure_open()
        name = _validate_name(name)
        subscription = ConsumerSubscription(self, name)
        self._consumer_subscriptions.setdefault(name, set()).add(subscription)

        if name in self._consumer_last:
            subscription._push_register(self._consumer_last[name])
        elif name not in self._consumer_initial_tasks:
            revision = self._consumer_revisions.get(name, 0)
            task = asyncio.create_task(self._fetch_initial(name, revision))
            self._consumer_initial_tasks[name] = task
        self._ensure_consumer_poller()
        return subscription

    def consume_all(self) -> ConsumeAllSubscription:
        self._ensure_open()
        subscription = ConsumeAllSubscription(self)
        self._consume_all_subscriptions.add(subscription)
        if self._consume_all_initialized:
            for name, register in self._consume_all_last.items():
                subscription._push_update(
                    RegisterUpdate(name, register.value, dict(register.metadata)),
                    register,
                )
        elif self._consume_all_initial_task is None:
            self._consume_all_initial_task = asyncio.create_task(
                self._fetch_all_initial(self._consume_all_revision)
            )
        self._ensure_consume_all_poller()
        return subscription

    async def provide(
        self,
        name: str,
        value: Any,
        metadata: Mapping[str, Any] | None = None,
        ttl: Duration = 5,
    ) -> ProviderSubscription:
        self._ensure_open()
        name = _validate_name(name)
        ttl_text, ttl_seconds = normalize_duration(ttl)
        if name in self._providers or name in self._provider_closing:
            raise RuntimeError(f"register {name!r} already has an active provider")

        subscription = ProviderSubscription(self, name)
        state = _ProviderState(
            subscription=subscription,
            value=value,
            metadata=dict(metadata or {}),
            ttl=ttl_text,
            ttl_seconds=ttl_seconds,
        )
        self._providers[name] = state
        try:
            await self._queue_provider_write(state)
        except BaseException:
            if self._providers.get(name) is state:
                del self._providers[name]
            subscription._finish()
            await _cancel_and_wait(*state.write_tasks)
            raise

        if self._closed or self._providers.get(name) is not state:
            subscription._finish()
            raise RuntimeError("client was closed while starting provider")

        state.refresh_task = asyncio.create_task(self._refresh_provider(state))
        self._ensure_provider_poller()
        return subscription

    async def aclose(self) -> None:
        if self._closed:
            return
        self._closed = True

        subscriptions: list[_Subscription[Any]] = []
        for group in self._consumer_subscriptions.values():
            subscriptions.extend(group)
        subscriptions.extend(self._consume_all_subscriptions)
        subscriptions.extend(state.subscription for state in self._providers.values())
        await asyncio.gather(
            *(subscription.aclose() for subscription in subscriptions),
            return_exceptions=True,
        )

        await _cancel_and_wait(
            self._take_task("_consumer_task"),
            self._take_task("_consume_all_task"),
            self._take_task("_provider_task"),
        )
        if self._owns_wire:
            await cast(AsyncRestClient, self._wire).aclose()

    async def __aenter__(self) -> AsyncClient:
        self._ensure_open()
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()

    async def _fetch_initial(self, name: str, revision: int) -> None:
        task = asyncio.current_task()
        try:
            registers = await self._wire.get_registers((name,))
            if self._consumer_revisions.get(name, 0) != revision:
                return
            self._consumer_revisions[name] = revision + 1
            register = registers.get(name)
            if register is None:
                self._consumer_last.pop(name, None)
                return

            changed = self._consumer_last.get(name) != register
            self._consumer_last[name] = register
            for subscription in tuple(self._consumer_subscriptions.get(name, ())):
                if changed or not subscription._has_value:
                    subscription._push_register(register)
        except asyncio.CancelledError:
            raise
        except Exception:
            pass
        finally:
            if self._consumer_initial_tasks.get(name) is task:
                del self._consumer_initial_tasks[name]

    async def _fetch_all_initial(self, revision: int) -> None:
        task = asyncio.current_task()
        try:
            registers = await self._wire.get_registers(())
            if self._consume_all_revision != revision:
                return
            self._consume_all_revision += 1
            self._apply_consume_all_snapshot(registers)
        except asyncio.CancelledError:
            raise
        except Exception:
            pass
        finally:
            if self._consume_all_initial_task is task:
                self._consume_all_initial_task = None

    def _ensure_consumer_poller(self) -> None:
        if self._closed or not self._consumer_subscriptions:
            return
        if self._consumer_task is None:
            self._consumer_task = asyncio.create_task(self._consumer_loop())

    async def _consumer_loop(self) -> None:
        task = asyncio.current_task()
        try:
            while self._consumer_subscriptions:
                names = tuple(self._consumer_subscriptions)
                try:
                    registers = await self._wire.get_registers(
                        names, self._consumer_poll_interval
                    )
                except asyncio.CancelledError:
                    raise
                except Exception:
                    await asyncio.sleep(self._retry_delay)
                    continue

                for name in names:
                    self._consumer_revisions[name] = (
                        self._consumer_revisions.get(name, 0) + 1
                    )
                    if name not in registers:
                        self._consumer_last.pop(name, None)

                for name, register in registers.items():
                    subscriptions = self._consumer_subscriptions.get(name)
                    if not subscriptions or self._consumer_last.get(name) == register:
                        continue
                    changed = self._consumer_last.get(name) != register
                    self._consumer_last[name] = register
                    for subscription in tuple(subscriptions):
                        if changed or not subscription._has_value:
                            subscription._push_register(register)
                await asyncio.sleep(0)
        finally:
            if self._consumer_task is task:
                self._consumer_task = None
                self._ensure_consumer_poller()

    def _ensure_consume_all_poller(self) -> None:
        if self._closed or not self._consume_all_subscriptions:
            return
        if self._consume_all_task is None:
            self._consume_all_task = asyncio.create_task(self._consume_all_loop())

    async def _consume_all_loop(self) -> None:
        task = asyncio.current_task()
        try:
            while self._consume_all_subscriptions:
                try:
                    registers = await self._wire.get_registers(
                        (), self._consumer_poll_interval
                    )
                except asyncio.CancelledError:
                    raise
                except Exception:
                    await asyncio.sleep(self._retry_delay)
                    continue

                self._consume_all_revision += 1
                self._apply_consume_all_snapshot(registers)
                await asyncio.sleep(0)
        finally:
            if self._consume_all_task is task:
                self._consume_all_task = None
                self._ensure_consume_all_poller()

    def _apply_consume_all_snapshot(self, registers: dict[str, Register]) -> None:
        self._consume_all_initialized = True
        for name, register in registers.items():
            changed = self._consume_all_last.get(name) != register
            self._consume_all_last[name] = register
            update = RegisterUpdate(name, register.value, dict(register.metadata))
            for subscription in tuple(self._consume_all_subscriptions):
                if changed or not subscription._has_register(name, register):
                    subscription._push_update(update, register)

        for name in tuple(self._consume_all_last):
            if name in registers:
                continue
            del self._consume_all_last[name]
            update = RegisterUpdate(name, removed=True)
            for subscription in tuple(self._consume_all_subscriptions):
                if subscription._has_register_name(name):
                    subscription._push_update(update)

    def _ensure_provider_poller(self) -> None:
        if self._closed or not self._providers:
            return
        if self._provider_task is None:
            self._provider_task = asyncio.create_task(self._provider_loop())

    async def _provider_loop(self) -> None:
        task = asyncio.current_task()
        try:
            while self._providers:
                names = tuple(self._providers)
                try:
                    requests = await self._wire.get_change_requests(
                        names, self._provider_poll_interval
                    )
                except asyncio.CancelledError:
                    raise
                except Exception:
                    await asyncio.sleep(self._retry_delay)
                    continue

                for name, value in requests.items():
                    state = self._providers.get(name)
                    if state is not None:
                        state.subscription._push(value)
                await asyncio.sleep(0)
        finally:
            if self._provider_task is task:
                self._provider_task = None
                self._ensure_provider_poller()

    async def _refresh_provider(self, state: _ProviderState) -> None:
        try:
            while self._providers.get(state.subscription.name) is state:
                await asyncio.sleep(state.ttl_seconds / 2)
                if self._providers.get(state.subscription.name) is not state:
                    return
                try:
                    await self._queue_provider_write(state)
                except asyncio.CancelledError:
                    raise
                except Exception:
                    pass
        except asyncio.CancelledError:
            raise

    async def _request_change(self, name: str, value: Any) -> None:
        await self._wire.request_change(name, value)

    async def _update_provider(
        self, subscription: ProviderSubscription, value: Any
    ) -> None:
        state = self._providers.get(subscription.name)
        if state is None or state.subscription is not subscription:
            raise RuntimeError(f"no active provider for register {subscription.name!r}")
        state.value = value
        await self._queue_provider_write(state)

    def _queue_provider_write(self, state: _ProviderState) -> asyncio.Task[None]:
        task = asyncio.create_task(self._perform_provider_write(state))
        state.write_tasks.add(task)
        task.add_done_callback(state.write_tasks.discard)
        return task

    async def _perform_provider_write(self, state: _ProviderState) -> None:
        async with state.write_lock:
            if self._providers.get(state.subscription.name) is not state:
                return
            await self._wire.set_register(
                state.subscription.name,
                state.value,
                state.metadata,
                state.ttl,
            )

    def _remove_consumer(
        self, subscription: ConsumerSubscription
    ) -> list[asyncio.Task[None]]:
        subscriptions = self._consumer_subscriptions.get(subscription.name)
        if subscriptions is None:
            return []
        subscriptions.discard(subscription)
        if not subscriptions:
            del self._consumer_subscriptions[subscription.name]
            self._consumer_last.pop(subscription.name, None)
            self._consumer_revisions[subscription.name] = (
                self._consumer_revisions.get(subscription.name, 0) + 1
            )
            initial_task = self._consumer_initial_tasks.pop(subscription.name, None)
        else:
            initial_task = None
        if self._consumer_subscriptions:
            return [task for task in (initial_task,) if task is not None]
        return [
            task
            for task in (initial_task, self._take_task("_consumer_task"))
            if task is not None
        ]

    def _remove_consume_all(
        self, subscription: ConsumeAllSubscription
    ) -> list[asyncio.Task[None]]:
        self._consume_all_subscriptions.discard(subscription)
        if self._consume_all_subscriptions:
            return []
        self._consume_all_last.clear()
        self._consume_all_initialized = False
        self._consume_all_revision += 1
        initial_task = self._consume_all_initial_task
        self._consume_all_initial_task = None
        return [
            task
            for task in (initial_task, self._take_task("_consume_all_task"))
            if task is not None
        ]

    def _remove_provider(
        self, subscription: ProviderSubscription
    ) -> list[asyncio.Task[None]]:
        state = self._providers.get(subscription.name)
        if state is None or state.subscription is not subscription:
            return []
        del self._providers[subscription.name]
        self._provider_closing.add(subscription.name)
        poll_task = None
        if not self._providers:
            poll_task = self._take_task("_provider_task")
        return [
            task
            for task in (state.refresh_task, poll_task, *state.write_tasks)
            if task is not None
        ]

    def _finish_provider_close(self, name: str) -> None:
        self._provider_closing.discard(name)

    def _take_task(self, name: str) -> asyncio.Task[None] | None:
        task = cast(asyncio.Task[None] | None, getattr(self, name))
        setattr(self, name, None)
        return task

    def _ensure_open(self) -> None:
        if self._closed:
            raise RuntimeError("client is closed")
        asyncio.get_running_loop()


Client = AsyncClient


def _validate_name(name: str) -> str:
    if not isinstance(name, str) or not name.strip():
        raise ValueError("register name must not be empty")
    return name.strip()


async def _cancel_and_wait(*tasks: asyncio.Task[Any] | None) -> None:
    pending = [task for task in tasks if task is not None]
    for task in pending:
        task.cancel()
    if pending:
        await asyncio.gather(*pending, return_exceptions=True)
