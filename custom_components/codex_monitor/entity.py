"""Base entity for Codex Monitor."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import CONF_URL, DOMAIN
from .coordinator import CodexMonitorCoordinator


class CodexMonitorEntity(CoordinatorEntity[CodexMonitorCoordinator]):
    """Common device identity and availability handling."""

    _attr_has_entity_name = True

    def __init__(
        self,
        coordinator: CodexMonitorCoordinator,
        entry: ConfigEntry,
        entity_key: str,
    ) -> None:
        super().__init__(coordinator)
        self._entry = entry
        self._attr_unique_id = f"{coordinator.data.installation_id}_{entity_key}"

    @property
    def device_info(self) -> DeviceInfo:
        """Return the agent-backed Home Assistant device."""
        status = self.coordinator.data.status
        host: dict[str, Any] = status.get("host", {})
        host_os = host.get("os")
        host_arch = host.get("arch")
        model_suffix = "/".join(str(part) for part in (host_os, host_arch) if part)
        model = "Codex Monitor Agent"
        if model_suffix:
            model = f"{model} ({model_suffix})"

        return DeviceInfo(
            identifiers={(DOMAIN, self.coordinator.data.installation_id)},
            name=self._entry.title,
            model=model,
            sw_version=self.coordinator.data.agent_version,
            configuration_url=self._entry.data[CONF_URL],
        )
