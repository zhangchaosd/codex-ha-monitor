"""DataUpdateCoordinator for Codex Monitor."""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Callable
from datetime import timedelta

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import ConfigEntryAuthFailed
from homeassistant.helpers import issue_registry as ir
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import (
    CodexMonitorApi,
    CodexMonitorApiError,
    CodexMonitorAuthenticationError,
    CodexMonitorInvalidResponse,
    CodexMonitorStreamEvent,
)
from .const import DOMAIN, THREAD_LIMIT
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
        self._event_listeners: set[Callable[[CodexMonitorStreamEvent], None]] = set()
        self._stream_task: asyncio.Task[None] | None = None
        self._last_event_id: str | None = None
        self._event_gap_retry: tuple[str, str] | None = None
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
            snapshot = await self.api.async_get_snapshot(thread_limit=THREAD_LIMIT)
        except CodexMonitorAuthenticationError as err:
            raise ConfigEntryAuthFailed("Agent rejected the configured API token") from err
        except CodexMonitorInvalidResponse as err:
            ir.async_create_issue(
                self.hass,
                DOMAIN,
                f"incompatible_agent_{self.config_entry.entry_id}",
                is_fixable=False,
                severity=ir.IssueSeverity.ERROR,
                translation_key="incompatible_agent",
                translation_placeholders={"error": str(err)},
            )
            raise UpdateFailed(f"Agent protocol is incompatible: {err}") from err
        except CodexMonitorApiError as err:
            raise UpdateFailed(f"Unable to update from {self.api.base_url}: {err}") from err
        ir.async_delete_issue(
            self.hass,
            DOMAIN,
            f"incompatible_agent_{self.config_entry.entry_id}",
        )
        return snapshot

    @callback
    def async_add_task_event_listener(
        self, listener: Callable[[CodexMonitorStreamEvent], None]
    ) -> Callable[[], None]:
        """Subscribe an event entity to task transition events."""
        self._event_listeners.add(listener)

        @callback
        def remove_listener() -> None:
            self._event_listeners.discard(listener)

        return remove_listener

    @callback
    def async_start_stream(self) -> None:
        """Start one shared push stream for all entities in this config entry."""
        if self._stream_task is not None:
            return
        self._stream_task = self.hass.async_create_background_task(
            self._async_stream_loop(),
            f"{DOMAIN}-{self.config_entry.entry_id}-events",
            eager_start=True,
        )

    async def async_shutdown(self) -> None:
        """Stop the push stream when the config entry unloads."""
        if self._stream_task is None:
            return
        self._stream_task.cancel()
        try:
            await self._stream_task
        except asyncio.CancelledError:
            pass
        self._stream_task = None

    async def _async_stream_loop(self) -> None:
        backoff = 1
        unavailable_logged = False
        while True:
            try:
                async for event in self.api.async_listen_events(self._last_event_id):
                    backoff = 1
                    if unavailable_logged:
                        _LOGGER.info("Codex Monitor event stream recovered")
                        unavailable_logged = False
                    if event.event == "snapshot":
                        snapshot = MonitorSnapshot.from_payloads(
                            event.data,
                            {"threads": event.data.get("threads", [])},
                        )
                        if snapshot.installation_id != self.data.installation_id:
                            raise CodexMonitorInvalidResponse(
                                "agent installation_id changed on the event stream"
                            )
                        self.async_set_updated_data(snapshot)
                    elif event.event == "task_activity":
                        if event.event_id:
                            cursor, retry, gap = _advance_event_cursor(
                                self._last_event_id,
                                event.event_id,
                                self._event_gap_retry,
                            )
                            self._last_event_id = cursor
                            self._event_gap_retry = retry
                            if gap:
                                raise CodexMonitorApiError(
                                    "SSE task event sequence gap; reconnecting for replay"
                                )
                        for listener in tuple(self._event_listeners):
                            listener(event)
            except asyncio.CancelledError:
                raise
            except CodexMonitorAuthenticationError:
                await self.async_request_refresh()
                return
            except (CodexMonitorApiError, ValueError) as err:
                if not unavailable_logged:
                    _LOGGER.warning("Codex Monitor event stream unavailable: %s", err)
                    unavailable_logged = True
                await self.async_request_refresh()
            await asyncio.sleep(backoff)
            backoff = min(backoff * 2, 30)


def _advance_event_cursor(
    previous: str | None,
    current: str,
    repeated_gap: tuple[str, str] | None,
) -> tuple[str, tuple[str, str] | None, bool]:
    """Advance an SSE cursor or force one replay retry when IDs have a gap."""
    if previous is None:
        return current, None, False
    try:
        previous_number = int(previous)
        current_number = int(current)
    except ValueError:
        return current, None, False
    if current_number <= previous_number + 1:
        return current, None, False

    gap = (previous, current)
    if repeated_gap == gap:
        # The missing range has already fallen outside the agent's bounded
        # history. Reconcile current state, then reconnect immediately before
        # this first retained event so delivery can continue.
        return str(current_number - 1), None, True
    return previous, gap, True
