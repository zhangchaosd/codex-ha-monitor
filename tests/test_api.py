"""Tests for the async Codex Monitor API client."""

import pytest

from custom_components.codex_monitor.api import (
    CodexMonitorApi,
    CodexMonitorInvalidResponse,
)


class FakeResponse:
    def __init__(self, payload):
        self._payload = payload

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

    def get(self, url, timeout):
        for suffix, response in self.responses.items():
            if url.endswith(suffix):
                return FakeResponse(response)
        raise AssertionError(f"unexpected request: {url}")


@pytest.mark.asyncio
async def test_probe_checks_installation_identity():
    session = FakeSession(
        {
            "/api/v1/version": {"installation_id": "same"},
            "/api/v1/status": {"installation_id": "same"},
        }
    )
    api = CodexMonitorApi(session, "http://agent:8765/")

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
    api = CodexMonitorApi(session, "http://agent:8765")

    with pytest.raises(CodexMonitorInvalidResponse):
        await api.async_probe()
