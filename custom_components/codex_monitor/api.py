"""Asynchronous client for the Codex Monitor Agent HTTP API."""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator, Mapping
from dataclasses import dataclass
from typing import Any

from aiohttp import ClientError, ClientResponseError, ClientSession, ClientTimeout

from .models import InvalidSnapshotError, MonitorSnapshot


class CodexMonitorApiError(Exception):
    """Base error for the Codex Monitor API."""


class CodexMonitorCannotConnect(CodexMonitorApiError):
    """Raised when the agent cannot be reached."""


class CodexMonitorInvalidResponse(CodexMonitorApiError):
    """Raised when the agent returns an unsupported response."""


class CodexMonitorAuthenticationError(CodexMonitorApiError):
    """Raised when the agent rejects the configured bearer token."""


@dataclass(frozen=True)
class CodexMonitorStreamEvent:
    """One parsed event from the agent SSE stream."""

    event: str
    data: Mapping[str, Any]
    event_id: str | None = None


class CodexMonitorApi:
    """Small aiohttp client using Home Assistant's shared session."""

    def __init__(self, session: ClientSession, base_url: str, token: str) -> None:
        self._session = session
        self._base_url = base_url.rstrip("/")
        self._headers = {"Authorization": f"Bearer {token}"}
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

    async def async_get_requests(self) -> Mapping[str, Any]:
        """Read pending approval and input requests."""
        return await self._async_get_json("/api/v1/requests")

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

    async def async_approve_request(
        self,
        request_id: str,
        thread_id: str,
        turn_id: str,
        *,
        for_session: bool = False,
    ) -> Mapping[str, Any]:
        """Approve one exact pending app-server request."""
        return await self._async_post_json(
            "/api/v1/actions/approve",
            {
                "request_id": request_id,
                "thread_id": thread_id,
                "turn_id": turn_id,
                "for_session": for_session,
            },
        )

    async def async_reject_request(
        self,
        request_id: str,
        thread_id: str,
        turn_id: str,
        *,
        cancel_turn: bool = False,
    ) -> Mapping[str, Any]:
        """Reject one exact pending app-server request."""
        return await self._async_post_json(
            "/api/v1/actions/reject",
            {
                "request_id": request_id,
                "thread_id": thread_id,
                "turn_id": turn_id,
                "cancel_turn": cancel_turn,
            },
        )

    async def async_submit_input(
        self,
        request_id: str,
        thread_id: str,
        turn_id: str,
        *,
        text: str | None = None,
        answers: Mapping[str, list[str]] | None = None,
    ) -> Mapping[str, Any]:
        """Submit text or structured answers to one pending input request."""
        payload: dict[str, Any] = {
            "request_id": request_id,
            "thread_id": thread_id,
            "turn_id": turn_id,
        }
        if text is not None:
            payload["text"] = text
        if answers is not None:
            payload["answers"] = dict(answers)
        return await self._async_post_json("/api/v1/actions/submit-input", payload)

    async def async_interrupt_turn(self, thread_id: str, turn_id: str) -> Mapping[str, Any]:
        """Interrupt one exact turn through the connected app-server."""
        return await self._async_post_json(
            "/api/v1/actions/interrupt",
            {"thread_id": thread_id, "turn_id": turn_id},
        )

    async def async_listen_events(
        self, last_event_id: str | None = None
    ) -> AsyncIterator[CodexMonitorStreamEvent]:
        """Yield authenticated, replayable SSE messages until disconnected."""
        headers = {**self._headers, "Accept": "text/event-stream"}
        if last_event_id:
            headers["Last-Event-ID"] = last_event_id
        timeout = ClientTimeout(total=None, sock_connect=10, sock_read=45)
        try:
            async with self._session.get(
                f"{self._base_url}/api/v1/events",
                headers=headers,
                timeout=timeout,
            ) as response:
                self._raise_for_status(response)
                event_name = "message"
                event_id: str | None = None
                data_lines: list[str] = []
                async for raw_line in response.content:
                    line = raw_line.decode("utf-8").rstrip("\r\n")
                    if not line:
                        if data_lines:
                            payload = json.loads("\n".join(data_lines))
                            if not isinstance(payload, Mapping):
                                raise CodexMonitorInvalidResponse(
                                    "SSE event data is not a JSON object"
                                )
                            yield CodexMonitorStreamEvent(event_name, payload, event_id)
                        event_name, event_id, data_lines = "message", None, []
                        continue
                    if line.startswith(":"):
                        continue
                    field, _, value = line.partition(":")
                    value = value.removeprefix(" ")
                    if field == "event":
                        event_name = value
                    elif field == "id":
                        event_id = value
                    elif field == "data":
                        data_lines.append(value)
        except CodexMonitorApiError:
            raise
        except (TimeoutError, ClientError, ClientResponseError) as err:
            raise CodexMonitorCannotConnect(str(err)) from err
        except (UnicodeDecodeError, ValueError, TypeError) as err:
            raise CodexMonitorInvalidResponse("agent SSE stream is invalid") from err

    async def _async_get_json(self, path: str) -> Mapping[str, Any]:
        return await self._async_request_json("GET", path)

    async def _async_post_json(self, path: str, payload: Mapping[str, Any]) -> Mapping[str, Any]:
        return await self._async_request_json("POST", path, payload)

    async def _async_request_json(
        self,
        method: str,
        path: str,
        payload: Mapping[str, Any] | None = None,
    ) -> Mapping[str, Any]:
        try:
            request = self._session.get if method == "GET" else self._session.post
            kwargs: dict[str, Any] = {"headers": self._headers, "timeout": self._timeout}
            if payload is not None:
                kwargs["json"] = payload
            async with request(f"{self._base_url}{path}", **kwargs) as response:
                self._raise_for_status(response)
                payload = await response.json(content_type=None)
        except CodexMonitorApiError:
            raise
        except (TimeoutError, ClientError, ClientResponseError) as err:
            raise CodexMonitorCannotConnect(str(err)) from err
        except (ValueError, TypeError) as err:
            raise CodexMonitorInvalidResponse("agent response is not valid JSON") from err

        if not isinstance(payload, Mapping):
            raise CodexMonitorInvalidResponse("agent response is not a JSON object")
        return payload

    @staticmethod
    def _raise_for_status(response: Any) -> None:
        if getattr(response, "status", 200) in (401, 403):
            raise CodexMonitorAuthenticationError("agent rejected the API token")
        response.raise_for_status()
