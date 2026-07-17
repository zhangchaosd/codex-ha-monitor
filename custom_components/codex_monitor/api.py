"""Asynchronous client for the Codex Monitor Agent HTTP API."""

from __future__ import annotations

import asyncio
from collections.abc import Mapping
from typing import Any

from aiohttp import ClientError, ClientResponseError, ClientSession, ClientTimeout

from .models import InvalidSnapshotError, MonitorSnapshot


class CodexMonitorApiError(Exception):
    """Base error for the Codex Monitor API."""


class CodexMonitorCannotConnect(CodexMonitorApiError):
    """Raised when the agent cannot be reached."""


class CodexMonitorInvalidResponse(CodexMonitorApiError):
    """Raised when the agent returns an unsupported response."""


class CodexMonitorApi:
    """Small aiohttp client using Home Assistant's shared session."""

    def __init__(self, session: ClientSession, base_url: str) -> None:
        self._session = session
        self._base_url = base_url.rstrip("/")
        self._timeout = ClientTimeout(total=10)

    @property
    def base_url(self) -> str:
        """Return the configured agent base URL."""
        return self._base_url

    async def async_get_version(self) -> Mapping[str, Any]:
        """Read agent and Codex version information."""
        payload = await self._async_get_json("/api/v1/version")
        installation_id = payload.get("installation_id")
        if not isinstance(installation_id, str) or not installation_id:
            raise CodexMonitorInvalidResponse("version response has no installation_id")
        return payload

    async def async_get_status(self) -> Mapping[str, Any]:
        """Read the current agent status."""
        return await self._async_get_json("/api/v1/status")

    async def async_get_threads(self, limit: int) -> Mapping[str, Any]:
        """Read the most recently updated Codex threads."""
        return await self._async_get_json(f"/api/v1/threads?limit={limit}")

    async def async_probe(self) -> tuple[Mapping[str, Any], Mapping[str, Any]]:
        """Validate connectivity and return version plus status data."""
        version, status = await asyncio.gather(
            self.async_get_version(),
            self.async_get_status(),
        )
        if version.get("installation_id") != status.get("installation_id"):
            raise CodexMonitorInvalidResponse("agent installation_id changed during probe")
        return version, status

    async def async_get_snapshot(self, thread_limit: int) -> MonitorSnapshot:
        """Read and combine status and thread responses."""
        status, threads = await asyncio.gather(
            self.async_get_status(),
            self.async_get_threads(thread_limit),
        )
        try:
            return MonitorSnapshot.from_payloads(status, threads)
        except InvalidSnapshotError as err:
            raise CodexMonitorInvalidResponse(str(err)) from err

    async def _async_get_json(self, path: str) -> Mapping[str, Any]:
        try:
            async with self._session.get(
                f"{self._base_url}{path}",
                timeout=self._timeout,
            ) as response:
                response.raise_for_status()
                payload = await response.json(content_type=None)
        except (TimeoutError, ClientError, ClientResponseError) as err:
            raise CodexMonitorCannotConnect(str(err)) from err
        except (ValueError, TypeError) as err:
            raise CodexMonitorInvalidResponse("agent response is not valid JSON") from err

        if not isinstance(payload, Mapping):
            raise CodexMonitorInvalidResponse("agent response is not a JSON object")
        return payload
