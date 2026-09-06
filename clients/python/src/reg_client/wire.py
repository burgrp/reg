from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Any

import httpx

from .duration import Duration, format_duration
from .models import Register


class RegistryError(RuntimeError):
    def __init__(self, method: str, path: str, status_code: int, body: str) -> None:
        detail = f" {body.strip()}" if body.strip() else ""
        super().__init__(f"{method} {path} failed: HTTP {status_code}{detail}")
        self.method = method
        self.path = path
        self.status_code = status_code
        self.body = body


class RegistryProtocolError(RuntimeError):
    pass


class AsyncRestClient:
    def __init__(
        self,
        base_url: str,
        *,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        base_url = base_url.rstrip("/")
        if not base_url:
            raise ValueError("base_url must not be empty")

        self._base_url = base_url
        self._owns_http_client = http_client is None
        self._http_client = http_client or httpx.AsyncClient(
            timeout=httpx.Timeout(10.0, read=None)
        )
        self._closed = False

    async def get_registers(
        self,
        names: Iterable[str] = (),
        wait: Duration | None = None,
    ) -> dict[str, Register]:
        params: list[tuple[str, str | int | float | bool | None]] = [
            ("name", name) for name in names
        ]
        if wait is not None:
            params.append(("wait", format_duration(wait)))

        response = await self._http_client.get(
            f"{self._base_url}/consumer", params=params
        )
        self._raise_for_status(response, 200, "GET", "/consumer")
        registers = self._get_register_map(response)

        result: dict[str, Register] = {}
        for name, raw_register in registers.items():
            register = self._as_mapping(raw_register, f"register {name!r}")
            value = self._required_value(register, name)
            metadata = register.get("metadata", {})
            if not isinstance(metadata, dict):
                raise RegistryProtocolError(
                    f"register {name!r} metadata must be an object"
                )
            result[name] = Register(value=value, metadata=dict(metadata))
        return result

    async def request_change(self, name: str, value: Any) -> None:
        await self.request_changes({name: value})

    async def request_changes(self, changes: Mapping[str, Any]) -> None:
        payload = {
            "registers": {name: {"value": value} for name, value in changes.items()}
        }
        response = await self._http_client.put(
            f"{self._base_url}/consumer", json=payload
        )
        self._raise_for_status(response, 202, "PUT", "/consumer")

    async def set_register(
        self,
        name: str,
        value: Any,
        metadata: Mapping[str, Any] | None = None,
        ttl: Duration | None = None,
    ) -> None:
        register: dict[str, Any] = {"value": value}
        if metadata is not None:
            register["metadata"] = dict(metadata)
        if ttl is not None:
            register["ttl"] = format_duration(ttl)
        await self.set_registers({name: register})

    async def set_registers(self, registers: Mapping[str, Mapping[str, Any]]) -> None:
        payload = {
            "registers": {name: dict(value) for name, value in registers.items()}
        }
        response = await self._http_client.put(
            f"{self._base_url}/provider", json=payload
        )
        self._raise_for_status(response, 204, "PUT", "/provider")

    async def get_change_requests(
        self,
        names: Iterable[str] = (),
        wait: Duration | None = None,
    ) -> dict[str, Any]:
        params: list[tuple[str, str | int | float | bool | None]] = [
            ("name", name) for name in names
        ]
        if wait is not None:
            params.append(("wait", format_duration(wait)))

        response = await self._http_client.get(
            f"{self._base_url}/provider", params=params
        )
        self._raise_for_status(response, 200, "GET", "/provider")
        registers = self._get_register_map(response)
        return {
            name: self._required_value(
                self._as_mapping(register, f"register {name!r}"), name
            )
            for name, register in registers.items()
        }

    async def aclose(self) -> None:
        if self._closed:
            return
        self._closed = True
        if self._owns_http_client:
            await self._http_client.aclose()

    async def __aenter__(self) -> AsyncRestClient:
        return self

    async def __aexit__(self, *_: object) -> None:
        await self.aclose()

    @staticmethod
    def _raise_for_status(
        response: httpx.Response,
        expected_status: int,
        method: str,
        path: str,
    ) -> None:
        if response.status_code != expected_status:
            raise RegistryError(method, path, response.status_code, response.text)

    @staticmethod
    def _get_register_map(response: httpx.Response) -> Mapping[str, Any]:
        try:
            body = response.json()
        except ValueError as error:
            raise RegistryProtocolError(
                "registry response is not valid JSON"
            ) from error
        if not isinstance(body, dict):
            raise RegistryProtocolError("registry response must be an object")
        if "registers" not in body:
            raise RegistryProtocolError("registry response is missing registers")
        registers = body["registers"]
        if not isinstance(registers, dict):
            raise RegistryProtocolError("registry response registers must be an object")
        return registers

    @staticmethod
    def _as_mapping(value: Any, label: str) -> Mapping[str, Any]:
        if not isinstance(value, dict):
            raise RegistryProtocolError(f"{label} must be an object")
        return value

    @staticmethod
    def _required_value(register: Mapping[str, Any], name: str) -> Any:
        if "value" not in register:
            raise RegistryProtocolError(f"register {name!r} is missing value")
        return register["value"]
