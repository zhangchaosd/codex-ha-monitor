"""Sensor entities for Codex Monitor."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from datetime import UTC, datetime
from typing import Any

from homeassistant.components.sensor import (
    SensorDeviceClass,
    SensorEntity,
    SensorEntityDescription,
    SensorStateClass,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import PERCENTAGE, EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .const import CONNECTION_STATES, WORKLOAD_STATES
from .entity import CodexMonitorEntity
from .models import MonitorSnapshot


def _rate_used(name: str) -> Callable[[MonitorSnapshot], Any]:
    return lambda data: data.rate_limit_window(name).get("usedPercent")


def _rate_reset(name: str) -> Callable[[MonitorSnapshot], datetime | None]:
    def value(data: MonitorSnapshot) -> datetime | None:
        timestamp = data.rate_limit_window(name).get("resetsAt")
        if not isinstance(timestamp, (int, float)) or isinstance(timestamp, bool):
            return None
        return datetime.fromtimestamp(timestamp, tz=UTC)

    return value


SENSOR_DESCRIPTIONS = (
    SensorEntityDescription(
        key="workload_state",
        translation_key="workload_state",
        device_class=SensorDeviceClass.ENUM,
        options=WORKLOAD_STATES,
        icon="mdi:robot-outline",
    ),
    SensorEntityDescription(
        key="current_task",
        translation_key="current_task",
        icon="mdi:clipboard-text-clock-outline",
    ),
    SensorEntityDescription(
        key="active_threads",
        translation_key="active_threads",
        icon="mdi:progress-wrench",
        state_class=SensorStateClass.MEASUREMENT,
    ),
    SensorEntityDescription(
        key="active_workers",
        translation_key="active_workers",
        icon="mdi:account-group-outline",
        state_class=SensorStateClass.MEASUREMENT,
        entity_category=EntityCategory.DIAGNOSTIC,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="running_tasks",
        translation_key="running_tasks",
        icon="mdi:progress-wrench",
        state_class=SensorStateClass.MEASUREMENT,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="pending_approvals",
        translation_key="pending_approvals",
        icon="mdi:account-check-outline",
        state_class=SensorStateClass.MEASUREMENT,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="pending_inputs",
        translation_key="pending_inputs",
        icon="mdi:form-textbox",
        state_class=SensorStateClass.MEASUREMENT,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="failed_tasks",
        translation_key="failed_tasks",
        icon="mdi:alert-circle-outline",
        state_class=SensorStateClass.MEASUREMENT,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="connection_state",
        translation_key="connection_state",
        device_class=SensorDeviceClass.ENUM,
        options=CONNECTION_STATES,
        entity_category=EntityCategory.DIAGNOSTIC,
        icon="mdi:lan-connect",
    ),
    SensorEntityDescription(
        key="known_threads",
        translation_key="known_threads",
        icon="mdi:forum-outline",
        state_class=SensorStateClass.MEASUREMENT,
        entity_category=EntityCategory.DIAGNOSTIC,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="codex_version",
        translation_key="codex_version",
        icon="mdi:console",
        entity_category=EntityCategory.DIAGNOSTIC,
    ),
    SensorEntityDescription(
        key="agent_version",
        translation_key="agent_version",
        icon="mdi:server-network",
        entity_category=EntityCategory.DIAGNOSTIC,
    ),
    SensorEntityDescription(
        key="lifetime_tokens",
        translation_key="lifetime_tokens",
        icon="mdi:counter",
        state_class=SensorStateClass.TOTAL_INCREASING,
        entity_category=EntityCategory.DIAGNOSTIC,
    ),
    SensorEntityDescription(
        key="current_streak_days",
        translation_key="current_streak_days",
        icon="mdi:calendar-check-outline",
        native_unit_of_measurement="d",
        state_class=SensorStateClass.MEASUREMENT,
        entity_category=EntityCategory.DIAGNOSTIC,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="primary_rate_limit_used",
        translation_key="primary_rate_limit_used",
        icon="mdi:speedometer",
        native_unit_of_measurement=PERCENTAGE,
        state_class=SensorStateClass.MEASUREMENT,
    ),
    SensorEntityDescription(
        key="primary_rate_limit_reset",
        translation_key="primary_rate_limit_reset",
        device_class=SensorDeviceClass.TIMESTAMP,
        icon="mdi:timer-refresh-outline",
    ),
    SensorEntityDescription(
        key="secondary_rate_limit_used",
        translation_key="secondary_rate_limit_used",
        icon="mdi:speedometer-slow",
        native_unit_of_measurement=PERCENTAGE,
        state_class=SensorStateClass.MEASUREMENT,
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="secondary_rate_limit_reset",
        translation_key="secondary_rate_limit_reset",
        device_class=SensorDeviceClass.TIMESTAMP,
        icon="mdi:timer-refresh-outline",
        entity_registry_enabled_default=False,
    ),
    SensorEntityDescription(
        key="received_hook_events",
        translation_key="received_hook_events",
        icon="mdi:webhook",
        state_class=SensorStateClass.TOTAL_INCREASING,
        entity_category=EntityCategory.DIAGNOSTIC,
        entity_registry_enabled_default=False,
    ),
)


VALUE_FUNCTIONS: dict[str, Callable[[MonitorSnapshot], Any]] = {
    "workload_state": lambda data: data.workload_state,
    "current_task": lambda data: data.current_thread_name,
    "active_threads": lambda data: data.active_threads,
    "active_workers": lambda data: data.active_workers,
    "running_tasks": lambda data: data.state_count("running"),
    "pending_approvals": lambda data: data.state_count("waiting_approval"),
    "pending_inputs": lambda data: data.state_count("waiting_input"),
    "failed_tasks": lambda data: data.state_count("error"),
    "connection_state": lambda data: data.connection_state,
    "known_threads": lambda data: data.known_threads,
    "codex_version": lambda data: data.codex_version,
    "agent_version": lambda data: data.agent_version,
    "lifetime_tokens": lambda data: data.lifetime_tokens,
    "current_streak_days": lambda data: data.current_streak_days,
    "primary_rate_limit_used": _rate_used("primary"),
    "primary_rate_limit_reset": _rate_reset("primary"),
    "secondary_rate_limit_used": _rate_used("secondary"),
    "secondary_rate_limit_reset": _rate_reset("secondary"),
    "received_hook_events": lambda data: data.received_hook_events,
}


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up Codex Monitor sensor entities."""
    coordinator = entry.runtime_data.coordinator
    async_add_entities(
        CodexMonitorSensor(coordinator, entry, description) for description in SENSOR_DESCRIPTIONS
    )


class CodexMonitorSensor(CodexMonitorEntity, SensorEntity):
    """A sensor backed by a value in the coordinator snapshot."""

    entity_description: SensorEntityDescription
    _unrecorded_attributes = frozenset(
        {
            "active_sessions",
            "agent_nickname",
            "binary",
            "controllable",
            "cwd",
            "go_version",
            "last_error",
            "last_event_at",
            "last_hook_event",
            "last_hook_event_at",
            "loaded",
            "parent_thread_id",
            "raw",
            "request_id",
            "resets_at",
            "root_thread_id",
            "source",
            "state_confidence",
            "state_source",
            "states",
            "thread_id",
            "thread_role",
            "turn_id",
            "visibility",
            "window_duration_minutes",
        }
    )

    def __init__(
        self,
        coordinator,
        entry: ConfigEntry,
        description: SensorEntityDescription,
    ) -> None:
        super().__init__(coordinator, entry, description.key)
        self.entity_description = description

    @property
    def native_value(self) -> Any:
        """Return the current sensor value."""
        return VALUE_FUNCTIONS[self.entity_description.key](self.coordinator.data)

    @property
    def extra_state_attributes(self) -> Mapping[str, Any] | None:
        """Return compact supporting data for the most useful sensors."""
        data = self.coordinator.data
        key = self.entity_description.key

        if key == "workload_state":
            summary = data.status.get("summary", {})
            hooks = data.status.get("hooks", {})
            return {
                "state_source": summary.get("state_source"),
                "state_confidence": summary.get("state_confidence"),
                "states": summary.get("states"),
                "active_sessions": hooks.get("active_sessions"),
                "last_hook_event_at": hooks.get("last_event_at"),
            }

        if key == "current_task":
            thread = data.current_thread
            if thread is None:
                return None
            return {
                "thread_id": thread.get("id"),
                "turn_id": thread.get("turn_id"),
                "parent_thread_id": thread.get("parent_thread_id"),
                "root_thread_id": thread.get("root_thread_id"),
                "thread_role": thread.get("thread_role"),
                "agent_nickname": thread.get("agent_nickname"),
                "state": str(thread.get("state", "unknown")).lower(),
                "state_source": thread.get("state_source"),
                "state_confidence": thread.get("state_confidence"),
                "source": thread.get("source"),
                "cwd": thread.get("cwd"),
                "loaded": thread.get("loaded"),
                "last_hook_event": thread.get("last_hook_event"),
                "request_id": thread.get("request_id"),
                "controllable": thread.get("controllable"),
            }

        if key == "connection_state":
            codex = data.status.get("codex", {})
            return {
                "visibility": codex.get("visibility"),
                "last_error": codex.get("last_error"),
            }

        if key == "codex_version":
            cli = data.status.get("codex_cli", {})
            return {"raw": cli.get("raw"), "binary": cli.get("binary")}

        if key == "agent_version":
            agent = data.status.get("agent", {})
            return {"go_version": agent.get("go_version")}

        if key.endswith("rate_limit_used"):
            window_name = "primary" if key.startswith("primary") else "secondary"
            window = data.rate_limit_window(window_name)
            return {
                "window_duration_minutes": window.get("windowDurationMins"),
                "resets_at": window.get("resetsAt"),
            }

        return None
