"""Tests for the async Codex Monitor API client."""

import pytest

from custom_components.codex_monitor.api import (
    CodexMonitorApi,
    CodexMonitorAuthenticationError,
    CodexMonitorInvalidResponse,
)


class FakeContent:
    def __init__(self, lines):
        self._lines = lines

    def __aiter__(self):
        self._iterator = iter(self._lines)
        return self

    async def __anext__(self):
        try:
            return next(self._iterator)
        except StopIteration as err:
            raise StopAsyncIteration from err


class FakeResponse:
    def __init__(self, payload, *, status=200, lines=None):
        self._payload = payload
        self.status = status
        self.content = FakeContent(lines or [])

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, traceback):
        return False

    def raise_for_status(self):
        return None

    async def json(self, content_type=None):
        return self._payload


class FakeSession:
    def __init__(self, responses):
        self.responses = responses
        self.posts = []

    def get(self, url, *, headers, timeout):
        assert headers["Authorization"] == "Bearer secret"
        for suffix, response in self.responses.items():
            if url.endswith(suffix):
                return response if isinstance(response, FakeResponse) else FakeResponse(response)
        raise AssertionError(f"unexpected request: {url}")

    def post(self, url, *, headers, timeout, json):
        assert headers == {"Authorization": "Bearer secret"}
        self.posts.append((url, json))
        return FakeResponse({"ok": True, "action": "approve"})


@pytest.mark.asyncio
async def test_probe_checks_installation_identity():
    session = FakeSession(
        {
            "/api/v1/version": {"installation_id": "same"},
            "/api/v1/status": {"installation_id": "same"},
        }
    )
    api = CodexMonitorApi(session, "http://agent:8765/", "secret")

    version, status = await api.async_probe()

    assert api.base_url == "http://agent:8765"
    assert version["installation_id"] == status["installation_id"]


@pytest.mark.asyncio
async def test_probe_rejects_changed_installation_identity():
    session = FakeSession(
        {
            "/api/v1/version": {"installation_id": "one"},
            "/api/v1/status": {"installation_id": "two"},
        }
    )
    api = CodexMonitorApi(session, "http://agent:8765", "secret")

    with pytest.raises(CodexMonitorInvalidResponse):
        await api.async_probe()


@pytest.mark.asyncio
async def test_authentication_error_is_distinct_from_connectivity():
    session = FakeSession({"/api/v1/status": FakeResponse({}, status=401)})
    api = CodexMonitorApi(session, "http://agent:8765", "secret")

    with pytest.raises(CodexMonitorAuthenticationError):
        await api.async_get_status()


@pytest.mark.asyncio
async def test_control_action_posts_exact_identifiers():
    session = FakeSession({})
    api = CodexMonitorApi(session, "http://agent:8765", "secret")

    result = await api.async_approve_request("request", "thread", "turn", for_session=True)

    assert result["ok"] is True
    assert session.posts == [
        (
            "http://agent:8765/api/v1/actions/approve",
            {
                "request_id": "request",
                "thread_id": "thread",
                "turn_id": "turn",
                "for_session": True,
            },
        )
    ]


@pytest.mark.asyncio
async def test_sse_parser_preserves_event_id_and_payload():
    session = FakeSession(
        {
            "/api/v1/events": FakeResponse(
                {},
                lines=[
                    b"id: 7\n",
                    b"event: task_activity\n",
                    b'data: {"type":"approval_required","thread_id":"thread"}\n',
                    b"\n",
                ],
            )
        }
    )
    api = CodexMonitorApi(session, "http://agent:8765", "secret")

    events = [event async for event in api.async_listen_events("6")]

    assert len(events) == 1
    assert events[0].event == "task_activity"
    assert events[0].event_id == "7"
    assert events[0].data["thread_id"] == "thread"
