"""Codex Monitor integration."""

from __future__ import annotations

from dataclasses import dataclass

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import CodexMonitorApi
from .const import (
    CONF_SCAN_INTERVAL,
    CONF_TOKEN,
    CONF_URL,
    DEFAULT_SCAN_INTERVAL,
    PLATFORMS,
)
from .coordinator import CodexMonitorCoordinator


@dataclass
class CodexMonitorRuntimeData:
    """Objects owned by one config entry."""

    api: CodexMonitorApi
    coordinator: CodexMonitorCoordinator


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Set up Codex Monitor from a config entry."""
    api = CodexMonitorApi(
        async_get_clientsession(hass),
        entry.data[CONF_URL],
        entry.data.get(CONF_TOKEN, ""),
    )
    coordinator = CodexMonitorCoordinator(
        hass,
        api,
        entry.options.get(CONF_SCAN_INTERVAL, DEFAULT_SCAN_INTERVAL),
        entry,
    )
    await coordinator.async_config_entry_first_refresh()

    entry.runtime_data = CodexMonitorRuntimeData(api=api, coordinator=coordinator)
    entry.async_on_unload(entry.add_update_listener(_async_reload_entry))
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a Codex Monitor config entry."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def _async_reload_entry(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Reload an entry after options change."""
    await hass.config_entries.async_reload(entry.entry_id)
