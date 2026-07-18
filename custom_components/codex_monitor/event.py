"""Event entity for reliable Codex task transitions."""

from __future__ import annotations

from homeassistant.components.event import EventEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .api import CodexMonitorStreamEvent
from .const import TASK_EVENT_TYPES
from .entity import CodexMonitorEntity


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up the task activity event entity."""
    async_add_entities([CodexMonitorTaskEvent(entry.runtime_data.coordinator, entry)])


class CodexMonitorTaskEvent(CodexMonitorEntity, EventEntity):
    """Expose agent task transitions to dashboards and automations."""

    _attr_translation_key = "task_activity"
    _attr_event_types = list(TASK_EVENT_TYPES)
    _attr_icon = "mdi:robot-industrial-outline"

    def __init__(self, coordinator, entry: ConfigEntry) -> None:
        super().__init__(coordinator, entry, "task_activity")

    async def async_added_to_hass(self) -> None:
        """Subscribe only while Home Assistant has loaded this entity."""
        await super().async_added_to_hass()
        self.async_on_remove(
            self.coordinator.async_add_task_event_listener(self._async_handle_task_event)
        )

    @callback
    def _async_handle_task_event(self, event: CodexMonitorStreamEvent) -> None:
        event_type = event.data.get("type")
        if not isinstance(event_type, str) or event_type not in self._attr_event_types:
            return
        event_data = dict(event.data)
        event_data.pop("type", None)
        if event.event_id:
            event_data["event_id"] = event.event_id
        self._trigger_event(event_type, event_data)
        self.async_write_ha_state()
