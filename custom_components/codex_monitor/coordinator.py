"""DataUpdateCoordinator for Codex Monitor."""

from __future__ import annotations

import logging
from datetime import timedelta

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import CodexMonitorApi, CodexMonitorApiError
from .const import THREAD_LIMIT
from .models import MonitorSnapshot

_LOGGER = logging.getLogger(__name__)


class CodexMonitorCoordinator(DataUpdateCoordinator[MonitorSnapshot]):
    """Poll the local agent and distribute a compact snapshot."""

    def __init__(
        self,
        hass: HomeAssistant,
        api: CodexMonitorApi,
        scan_interval: int,
        config_entry: ConfigEntry,
    ) -> None:
        self.api = api
        super().__init__(
            hass,
            _LOGGER,
            name="Codex Monitor",
            update_interval=timedelta(seconds=scan_interval),
            always_update=False,
            config_entry=config_entry,
        )

    async def _async_update_data(self) -> MonitorSnapshot:
        try:
            return await self.api.async_get_snapshot(thread_limit=THREAD_LIMIT)
        except CodexMonitorApiError as err:
            raise UpdateFailed(f"Unable to update from {self.api.base_url}: {err}") from err
