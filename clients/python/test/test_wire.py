from __future__ import annotations

import unittest
from datetime import timedelta

import httpx

from reg_client import (
    AsyncRestClient,
    RegistryError,
    RegistryProtocolError,
    duration_seconds,
    format_duration,
)


class DurationTest(unittest.TestCase):
    def test_formats_numeric_and_timedelta_durations(self) -> None:
        self.assertEqual(format_duration(5), "5s")
        self.assertEqual(format_duration(0.25), "0.25s")
        self.assertEqual(format_duration(timedelta(minutes=1, seconds=30)), "90s")

    def test_parses_complete_go_duration_strings(self) -> None:
        self.assertEqual(duration_seconds("1h30m500ms"), 5400.5)
        for value in ("", "0s", "junk5s", "5s-tail", "1sBAD2m"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    duration_seconds(value)


class AsyncRestClientTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.requests: list[httpx.Request] = []
        self.responses: list[httpx.Response] = []

        async def handler(request: httpx.Request) -> httpx.Response:
            self.requests.append(request)
            if not self.responses:
                raise AssertionError(
                    f"unexpected request: {request.method} {request.url}"
                )
            return self.responses.pop(0)

        self.http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        self.client = AsyncRestClient(
            "http://registry.example/", http_client=self.http_client
        )

    async def asyncTearDown(self) -> None:
        await self.client.aclose()
        self.assertFalse(
            self.http_client.is_closed,
            "an injected HTTP client must remain owned by its caller",
        )
        await self.http_client.aclose()

    async def test_get_registers_uses_repeated_names_and_wait(self) -> None:
        self.responses.append(
            httpx.Response(
                200,
                json={
                    "registers": {
                        "nullable": {"value": None},
                        "temp": {"value": 21.5, "metadata": {"unit": "C"}},
                    }
                },
            )
        )

        registers = await self.client.get_registers(
            ["nullable", "temp"], timedelta(seconds=5)
        )

        request = self.requests[0]
        self.assertEqual(request.url.params.get_list("name"), ["nullable", "temp"])
        self.assertEqual(request.url.params["wait"], "5s")
        self.assertIsNone(registers["nullable"].value)
        self.assertEqual(registers["nullable"].metadata, {})
        self.assertEqual(registers["temp"].metadata, {"unit": "C"})

    async def test_request_changes_sends_consumer_shape(self) -> None:
        self.responses.append(httpx.Response(202))

        await self.client.request_changes({"temp": 25, "mode": None})

        self.assertEqual(self.requests[0].method, "PUT")
        self.assertEqual(
            self.requests[0].read().decode(),
            '{"registers":{"temp":{"value":25},"mode":{"value":null}}}',
        )

    async def test_set_register_sends_metadata_and_ttl(self) -> None:
        self.responses.append(httpx.Response(204))

        await self.client.set_register(
            "temp", 21.5, {"unit": "C"}, timedelta(seconds=10)
        )

        self.assertEqual(self.requests[0].method, "PUT")
        self.assertEqual(
            self.requests[0].read().decode(),
            '{"registers":{"temp":{"value":21.5,"metadata":{"unit":"C"},"ttl":"10s"}}}',
        )

    async def test_get_change_requests_preserves_null(self) -> None:
        self.responses.append(
            httpx.Response(
                200,
                json={"registers": {"temp": {"value": None}, "mode": {"value": "eco"}}},
            )
        )

        requests = await self.client.get_change_requests(["temp", "mode"], "30s")

        self.assertEqual(requests, {"temp": None, "mode": "eco"})
        self.assertEqual(self.requests[0].url.params["wait"], "30s")

    async def test_raises_detailed_error_for_wrong_status(self) -> None:
        self.responses.append(httpx.Response(503, text="unavailable"))

        with self.assertRaisesRegex(RegistryError, r"HTTP 503 unavailable") as raised:
            await self.client.get_registers()

        self.assertEqual(raised.exception.status_code, 503)

    async def test_rejects_invalid_response_shape(self) -> None:
        for body in ({}, {"registers": []}):
            with self.subTest(body=body):
                self.responses.append(httpx.Response(200, json=body))
                with self.assertRaises(RegistryProtocolError):
                    await self.client.get_registers()

    async def test_rejects_missing_values_and_invalid_metadata(self) -> None:
        invalid_registers = [
            {"temp": {}},
            {"temp": {"value": 20, "metadata": None}},
            {"temp": {"value": 20, "metadata": []}},
        ]
        for registers in invalid_registers:
            with self.subTest(registers=registers):
                self.responses.append(
                    httpx.Response(200, json={"registers": registers})
                )
                with self.assertRaises(RegistryProtocolError):
                    await self.client.get_registers()

        self.responses.append(httpx.Response(200, json={"registers": {"temp": {}}}))
        with self.assertRaises(RegistryProtocolError):
            await self.client.get_change_requests()


if __name__ == "__main__":
    unittest.main()
