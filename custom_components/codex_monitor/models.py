"""Data model and value extraction for Codex Monitor."""

from __future__ import annotations

import hashlib
import json
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from typing import Any

from .const import CONNECTION_STATES, WORKLOAD_STATES


class InvalidSnapshotError(ValueError):
    """Raised when an agent response does not contain a usable snapshot."""


def _mapping(value: Any) -> Mapping[str, Any]:
    return value if isinstance(value, Mapping) else {}


def _normalise_state(value: Any, allowed: Sequence[str]) -> str:
    if not isinstance(value, str):
        return "unknown"
    state = value.strip().lower()
    return state if state in allowed else "unknown"


def _number(value: Any) -> float | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return value
    return None


@dataclass(frozen=True)
class MonitorSnapshot:
    """One combined status and thread snapshot.

    Raw payloads are excluded from equality. The fingerprint contains only data
    exposed by entities, so changing timestamps such as ``generated_at`` and
    ``uptime_seconds`` do not cause unnecessary Home Assistant state writes.
    """

    status: Mapping[str, Any] = field(compare=False)
    threads: tuple[Mapping[str, Any], ...] = field(compare=False)
    fingerprint: str

    @classmethod
    def from_payloads(
        cls,
        status: Mapping[str, Any],
        threads_payload: Mapping[str, Any],
    ) -> MonitorSnapshot:
        """Validate and combine the two agent responses."""
        if not isinstance(status, Mapping):
            raise InvalidSnapshotError("status response is not an object")

        installation_id = status.get("installation_id")
        if not isinstance(installation_id, str) or not installation_id.strip():
            raise InvalidSnapshotError("status response has no installation_id")

        raw_threads = threads_payload.get("threads")
        if not isinstance(raw_threads, list):
            raise InvalidSnapshotError("threads response has no threads array")

        threads = tuple(item for item in raw_threads if isinstance(item, Mapping))
        provisional = cls(status=status, threads=threads, fingerprint="")
        fingerprint = provisional._build_fingerprint()
        return cls(status=status, threads=threads, fingerprint=fingerprint)

    @property
    def installation_id(self) -> str:
        """Return the stable agent installation identifier."""
        return str(self.status["installation_id"])

    @property
    def workload_state(self) -> str:
        """Return a Home Assistant-safe workload state."""
        return _normalise_state(
            _mapping(self.status.get("summary")).get("workload_state"),
            WORKLOAD_STATES,
        )

    @property
    def connection_state(self) -> str:
        """Return a Home Assistant-safe connection state."""
        return _normalise_state(
            _mapping(self.status.get("codex")).get("connection_state"),
            CONNECTION_STATES,
        )

    @property
    def active_threads(self) -> float | None:
        """Return the active thread count."""
        return _number(_mapping(self.status.get("summary")).get("active_threads"))

    @property
    def known_threads(self) -> float | None:
        """Return the known thread count."""
        return _number(_mapping(self.status.get("summary")).get("known_threads"))

    @property
    def current_thread(self) -> Mapping[str, Any] | None:
        """Return the most important active thread, if any."""
        priorities = {
            "waiting_approval": 4,
            "waiting_input": 3,
            "running": 2,
            "error": 1,
        }
        candidates = []
        for thread in self.threads:
            state = _normalise_state(thread.get("state"), WORKLOAD_STATES)
            if state in priorities:
                candidates.append((priorities[state], str(thread.get("updated_at", "")), thread))

        if not candidates:
            return None
        candidates.sort(key=lambda item: (item[0], item[1]), reverse=True)
        return candidates[0][2]

    @property
    def current_thread_name(self) -> str | None:
        """Return a compact display name for the current thread."""
        thread = self.current_thread
        if thread is None:
            return None
        for key in ("name", "preview", "id"):
            value = thread.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()[:255]
        return None

    @property
    def agent_version(self) -> str | None:
        """Return the monitor agent version."""
        value = _mapping(self.status.get("agent")).get("version")
        return value if isinstance(value, str) and value else None

    @property
    def codex_version(self) -> str | None:
        """Return the Codex CLI version."""
        value = _mapping(self.status.get("codex_cli")).get("version")
        return value if isinstance(value, str) and value else None

    @property
    def lifetime_tokens(self) -> float | None:
        """Return lifetime token usage when app-server exposes it."""
        usage = _mapping(self.status.get("usage"))
        summary = _mapping(usage.get("summary"))
        return _number(summary.get("lifetimeTokens"))

    @property
    def current_streak_days(self) -> float | None:
        """Return the current Codex usage streak."""
        usage = _mapping(self.status.get("usage"))
        summary = _mapping(usage.get("summary"))
        return _number(summary.get("currentStreakDays"))

    @property
    def received_hook_events(self) -> float | None:
        """Return the number of hook events received by the agent."""
        return _number(_mapping(self.status.get("hooks")).get("received_events"))

    @property
    def is_stale(self) -> bool:
        """Return whether the agent marks its data as stale."""
        return self.status.get("stale") is True

    def rate_limit_window(self, name: str) -> Mapping[str, Any]:
        """Return a primary or secondary rate limit window."""
        rate_limits = _mapping(self.status.get("rate_limits"))
        common = _mapping(rate_limits.get("rateLimits"))
        window = common.get(name)
        return _mapping(window)

    def _build_fingerprint(self) -> str:
        summary = _mapping(self.status.get("summary"))
        codex = _mapping(self.status.get("codex"))
        agent = _mapping(self.status.get("agent"))
        cli = _mapping(self.status.get("codex_cli"))
        hooks = _mapping(self.status.get("hooks"))
        current = self.current_thread

        entity_data = {
            "installation_id": self.installation_id,
            "workload_state": self.workload_state,
            "connection_state": self.connection_state,
            "state_source": summary.get("state_source"),
            "state_confidence": summary.get("state_confidence"),
            "known_threads": self.known_threads,
            "active_threads": self.active_threads,
            "states": summary.get("states"),
            "visibility": codex.get("visibility"),
            "last_error": codex.get("last_error"),
            "stale": self.is_stale,
            "agent_version": self.agent_version,
            "go_version": agent.get("go_version"),
            "codex_version": self.codex_version,
            "codex_raw": cli.get("raw"),
            "codex_binary": cli.get("binary"),
            "lifetime_tokens": self.lifetime_tokens,
            "current_streak_days": self.current_streak_days,
            "received_hook_events": self.received_hook_events,
            "active_sessions": hooks.get("active_sessions"),
            "last_event_at": hooks.get("last_event_at"),
            "primary_rate_limit": self.rate_limit_window("primary"),
            "secondary_rate_limit": self.rate_limit_window("secondary"),
            "current_thread": None
            if current is None
            else {
                "id": current.get("id"),
                "turn_id": current.get("turn_id"),
                "name": current.get("name"),
                "preview": current.get("preview"),
                "cwd": current.get("cwd"),
                "source": current.get("source"),
                "state": current.get("state"),
                "state_source": current.get("state_source"),
                "state_confidence": current.get("state_confidence"),
                "loaded": current.get("loaded"),
                "last_hook_event": current.get("last_hook_event"),
            },
        }
        serialised = json.dumps(entity_data, sort_keys=True, separators=(",", ":"), default=str)
        return hashlib.sha256(serialised.encode("utf-8")).hexdigest()
