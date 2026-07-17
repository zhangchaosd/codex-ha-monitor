"""Tests for snapshot validation and entity value extraction."""

from copy import deepcopy

import pytest

from custom_components.codex_monitor.models import InvalidSnapshotError, MonitorSnapshot


def _status():
    return {
        "installation_id": "test-installation",
        "generated_at": "2026-07-17T00:00:00Z",
        "agent": {
            "version": "0.2.0",
            "go_version": "go1.26.5",
            "uptime_seconds": 10,
        },
        "codex_cli": {
            "version": "0.144.5",
            "raw": "codex-cli 0.144.5",
            "binary": "/Applications/ChatGPT.app/codex",
        },
        "codex": {
            "connection_state": "connected",
            "visibility": "agent_owned_with_filesystem_fallback",
            "last_success_at": "2026-07-17T00:00:00Z",
        },
        "summary": {
            "workload_state": "WAITING_APPROVAL",
            "state_source": "hook",
            "state_confidence": "observed",
            "known_threads": 12,
            "active_threads": 2,
            "states": {"running": 1, "waiting_approval": 1},
        },
        "hooks": {
            "received_events": 7,
            "active_sessions": 2,
            "last_event_at": "2026-07-17T00:00:00Z",
        },
        "usage": {
            "availability": "available",
            "summary": {"lifetimeTokens": 123456, "currentStreakDays": 8},
        },
        "rate_limits": {
            "availability": "available",
            "rateLimits": {
                "primary": {
                    "usedPercent": 9,
                    "resetsAt": 1784786315,
                    "windowDurationMins": 10080,
                },
                "secondary": None,
            },
        },
        "stale": False,
    }


def _threads():
    return {
        "threads": [
            {
                "id": "running-thread",
                "name": "Implement integration",
                "state": "RUNNING",
                "updated_at": "2026-07-17T00:00:03Z",
            },
            {
                "id": "approval-thread",
                "name": "Approve command",
                "state": "WAITING_APPROVAL",
                "updated_at": "2026-07-17T00:00:01Z",
            },
        ]
    }


def test_snapshot_extracts_entity_values():
    snapshot = MonitorSnapshot.from_payloads(_status(), _threads())

    assert snapshot.installation_id == "test-installation"
    assert snapshot.workload_state == "waiting_approval"
    assert snapshot.connection_state == "connected"
    assert snapshot.active_threads == 2
    assert snapshot.known_threads == 12
    assert snapshot.current_thread_name == "Approve command"
    assert snapshot.agent_version == "0.2.0"
    assert snapshot.codex_version == "0.144.5"
    assert snapshot.lifetime_tokens == 123456
    assert snapshot.current_streak_days == 8
    assert snapshot.received_hook_events == 7
    assert snapshot.rate_limit_window("primary")["usedPercent"] == 9
    assert snapshot.is_stale is False


def test_fingerprint_ignores_poll_only_timestamps():
    before_status = _status()
    before_threads = _threads()
    before = MonitorSnapshot.from_payloads(before_status, before_threads)

    after_status = deepcopy(before_status)
    after_status["generated_at"] = "2026-07-17T00:10:00Z"
    after_status["agent"]["uptime_seconds"] = 610
    after_status["codex"]["last_success_at"] = "2026-07-17T00:10:00Z"
    after_threads = deepcopy(before_threads)
    after_threads["threads"][0]["updated_at"] = "2026-07-17T00:10:00Z"
    after = MonitorSnapshot.from_payloads(after_status, after_threads)

    assert before == after


def test_fingerprint_changes_when_entity_state_changes():
    before = MonitorSnapshot.from_payloads(_status(), _threads())
    changed_status = deepcopy(_status())
    changed_status["summary"]["workload_state"] = "IDLE"
    changed = MonitorSnapshot.from_payloads(changed_status, _threads())

    assert before != changed


@pytest.mark.parametrize(
    ("status", "threads"),
    [
        ({}, {"threads": []}),
        ({"installation_id": "id"}, {}),
        ({"installation_id": "id"}, {"threads": "not-a-list"}),
    ],
)
def test_invalid_payload_is_rejected(status, threads):
    with pytest.raises(InvalidSnapshotError):
        MonitorSnapshot.from_payloads(status, threads)
