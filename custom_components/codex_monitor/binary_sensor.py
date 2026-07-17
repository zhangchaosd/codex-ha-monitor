"""Binary sensor entities for Codex Monitor."""

from __future__ import annotations

from collections.abc import Callable

from homeassistant.components.binary_sensor import (
    BinarySensorDeviceClass,
    BinarySensorEntity,
    BinarySensorEntityDescription,
)
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from .entity import CodexMonitorEntity
from .models import MonitorSnapshot

BINARY_SENSOR_DESCRIPTIONS = (
    BinarySensorEntityDescription(
        key="connected",
        translation_key="connected",
        device_class=BinarySensorDeviceClass.CONNECTIVITY,
        entity_category=EntityCategory.DIAGNOSTIC,
    ),
    BinarySensorEntityDescription(
        key="running",
        translation_key="running",
        device_class=BinarySensorDeviceClass.RUNNING,
    ),
    BinarySensorEntityDescription(
        key="attention_required",
        translation_key="attention_required",
        device_class=BinarySensorDeviceClass.PROBLEM,
        icon="mdi:account-alert-outline",
    ),
    BinarySensorEntityDescription(
        key="stale",
        translation_key="stale",
        device_class=BinarySensorDeviceClass.PROBLEM,
        entity_category=EntityCategory.DIAGNOSTIC,
        entity_registry_enabled_default=False,
    ),
)


IS_ON_FUNCTIONS: dict[str, Callable[[MonitorSnapshot], bool]] = {
    "connected": lambda data: data.connection_state == "connected",
    "running": lambda data: data.workload_state == "running",
    "attention_required": lambda data: data.workload_state in ("waiting_approval", "waiting_input"),
    "stale": lambda data: data.is_stale,
}


async def async_setup_entry(
    hass: HomeAssistant,
    entry: ConfigEntry,
    async_add_entities: AddEntitiesCallback,
) -> None:
    """Set up Codex Monitor binary sensor entities."""
    coordinator = entry.runtime_data.coordinator
    async_add_entities(
        CodexMonitorBinarySensor(coordinator, entry, description)
        for description in BINARY_SENSOR_DESCRIPTIONS
    )


class CodexMonitorBinarySensor(CodexMonitorEntity, BinarySensorEntity):
    """A binary sensor backed by the coordinator snapshot."""

    entity_description: BinarySensorEntityDescription

    def __init__(
        self,
        coordinator,
        entry: ConfigEntry,
        description: BinarySensorEntityDescription,
    ) -> None:
        super().__init__(coordinator, entry, description.key)
        self.entity_description = description

    @property
    def is_on(self) -> bool:
        """Return the binary state."""
        return IS_ON_FUNCTIONS[self.entity_description.key](self.coordinator.data)
