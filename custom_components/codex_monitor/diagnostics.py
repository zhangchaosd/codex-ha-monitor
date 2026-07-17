"""Diagnostics support for Codex Monitor."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant

from .const import CONF_TOKEN


async def async_get_config_entry_diagnostics(
    hass: HomeAssistant,
    entry: ConfigEntry,
) -> dict[str, Any]:
    """Return raw read-only agent data for troubleshooting."""
    snapshot = entry.runtime_data.coordinator.data
    config_data = dict(entry.data)
    if CONF_TOKEN in config_data:
        config_data[CONF_TOKEN] = "**REDACTED**"
    return {
        "config_entry": {
            "title": entry.title,
            "unique_id": entry.unique_id,
            "data": config_data,
            "options": dict(entry.options),
        },
        "status": dict(snapshot.status),
        "threads": [dict(thread) for thread in snapshot.threads],
        "fingerprint": snapshot.fingerprint,
    }
