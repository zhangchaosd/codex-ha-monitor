"""Home Assistant runtime setup tests."""

from unittest.mock import patch

import pytest
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.codex_monitor.api import CodexMonitorStreamEvent
from custom_components.codex_monitor.const import (
    CONF_TOKEN,
    CONF_URL,
    DOMAIN,
    SERVICE_APPROVE_REQUEST,
    SERVICE_INTERRUPT_TURN,
    SERVICE_REJECT_REQUEST,
    SERVICE_SUBMIT_INPUT,
)


def _status():
    return {
        "schema_version": "1.1",
        "installation_id": "test-installation",
        "host": {"name": "Test Mac", "os": "darwin", "arch": "arm64"},
        "agent": {"version": "0.4.0", "go_version": "go1.26", "uptime_seconds": 1},
        "codex_cli": {"version": "0.145.0"},
        "codex": {
            "connection_state": "connected",
            "visibility": "agent_owned_with_filesystem_fallback",
        },
        "summary": {
            "workload_state": "RUNNING",
            "state_source": "app_server_event",
            "state_confidence": "exact",
            "known_threads": 1,
            "active_threads": 1,
            "active_workflows": 1,
            "active_workers": 1,
            "states": {
                "running": 1,
                "waiting_approval": 0,
                "waiting_input": 0,
                "idle": 0,
                "error": 0,
                "unknown": 0,
            },
        },
        "hooks": {"received_events": 0, "active_sessions": 0},
        "usage": {"availability": "unavailable"},
        "rate_limits": {"availability": "unavailable"},
        "stale": False,
    }


@pytest.mark.asyncio
async def test_setup_creates_one_device_entities_event_and_actions(hass, aioclient_mock):
    """The integration should load as one agent-backed push device."""
    aioclient_mock.get("http://agent:8765/api/v1/status", json=_status())
    aioclient_mock.get(
        "http://agent:8765/api/v1/threads?limit=50",
        json={
            "threads": [
                {
                    "id": "thread",
                    "root_thread_id": "thread",
                    "thread_role": "root",
                    "name": "Test task",
                    "state": "RUNNING",
                    "state_source": "app_server_event",
                    "state_confidence": "exact",
                    "loaded": True,
                    "controllable": True,
                    "updated_at": "2026-07-18T00:00:00Z",
                }
            ]
        },
    )
    entry = MockConfigEntry(
        domain=DOMAIN,
        title="Test Mac",
        unique_id="test-installation",
        data={CONF_URL: "http://agent:8765", CONF_TOKEN: "secret"},
    )
    entry.add_to_hass(hass)

    with patch(
        "custom_components.codex_monitor.coordinator.CodexMonitorCoordinator.async_start_stream"
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()

    devices = dr.async_entries_for_config_entry(dr.async_get(hass), entry.entry_id)
    assert len(devices) == 1
    entities = er.async_entries_for_config_entry(er.async_get(hass), entry.entry_id)
    assert any(item.domain == "sensor" for item in entities)
    assert any(item.domain == "binary_sensor" for item in entities)
    assert any(item.domain == "event" for item in entities)
    for service in (
        SERVICE_APPROVE_REQUEST,
        SERVICE_REJECT_REQUEST,
        SERVICE_SUBMIT_INPUT,
        SERVICE_INTERRUPT_TURN,
    ):
        assert hass.services.has_service(DOMAIN, service)

    coordinator = entry.runtime_data.coordinator
    event = CodexMonitorStreamEvent(
        "task_activity",
        {
            "type": "approval_required",
            "thread_id": "thread",
            "turn_id": "turn",
            "request_id": "request",
            "controllable": True,
        },
        "7",
    )
    for listener in tuple(coordinator._event_listeners):
        listener(event)
    await hass.async_block_till_done()
    event_state = next(
        state
        for state in hass.states.async_all("event")
        if state.entity_id.endswith("task_activity")
    )
    assert event_state.attributes["event_type"] == "approval_required"
    assert event_state.attributes["request_id"] == "request"

    aioclient_mock.post(
        "http://agent:8765/api/v1/actions/approve",
        json={
            "ok": True,
            "action": "approve",
            "request_id": "request",
            "thread_id": "thread",
            "turn_id": "turn",
        },
    )
    response = await hass.services.async_call(
        DOMAIN,
        SERVICE_APPROVE_REQUEST,
        {
            "device_id": devices[0].id,
            "request_id": "request",
            "thread_id": "thread",
            "turn_id": "turn",
        },
        blocking=True,
        return_response=True,
    )
    assert response["ok"] is True

    assert await hass.config_entries.async_unload(entry.entry_id)
