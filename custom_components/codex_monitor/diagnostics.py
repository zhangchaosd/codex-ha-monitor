"""Diagnostics support for Codex Monitor."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant


async def async_get_config_entry_diagnostics(
    hass: HomeAssistant,
    entry: ConfigEntry,
) -> dict[str, Any]:
    """Return raw read-only agent data for troubleshooting."""
    snapshot = entry.runtime_data.coordinator.data
    return {
        "config_entry": {
            "title": entry.title,
            "unique_id": entry.unique_id,
            "data": dict(entry.data),
            "options": dict(entry.options),
        },
        "status": dict(snapshot.status),
        "threads": [dict(thread) for thread in snapshot.threads],
        "fingerprint": snapshot.fingerprint,
    }
