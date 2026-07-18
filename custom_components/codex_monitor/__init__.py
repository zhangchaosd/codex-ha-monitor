"""Codex Monitor integration."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import voluptuous as vol
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, ServiceCall, SupportsResponse
from homeassistant.exceptions import ServiceValidationError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import CodexMonitorApi, CodexMonitorApiError
from .const import (
    ATTR_ANSWERS,
    ATTR_CANCEL_TURN,
    ATTR_DEVICE_ID,
    ATTR_FOR_SESSION,
    ATTR_REQUEST_ID,
    ATTR_TEXT,
    ATTR_THREAD_ID,
    ATTR_TURN_ID,
    CONF_SCAN_INTERVAL,
    CONF_TOKEN,
    CONF_URL,
    DEFAULT_SCAN_INTERVAL,
    DOMAIN,
    PLATFORMS,
    SERVICE_APPROVE_REQUEST,
    SERVICE_INTERRUPT_TURN,
    SERVICE_REJECT_REQUEST,
    SERVICE_SUBMIT_INPUT,
)
from .coordinator import CodexMonitorCoordinator


@dataclass
class CodexMonitorRuntimeData:
    """Objects owned by one config entry."""

    api: CodexMonitorApi
    coordinator: CodexMonitorCoordinator


async def async_setup(hass: HomeAssistant, config: dict[str, Any]) -> bool:
    """Register integration-wide Home Assistant actions."""

    async def handle_approve(call: ServiceCall) -> dict[str, Any]:
        api = _api_for_device(hass, call.data[ATTR_DEVICE_ID])
        return await _run_action(
            api.async_approve_request(
                call.data[ATTR_REQUEST_ID],
                call.data[ATTR_THREAD_ID],
                call.data[ATTR_TURN_ID],
                for_session=call.data[ATTR_FOR_SESSION],
            )
        )

    async def handle_reject(call: ServiceCall) -> dict[str, Any]:
        api = _api_for_device(hass, call.data[ATTR_DEVICE_ID])
        return await _run_action(
            api.async_reject_request(
                call.data[ATTR_REQUEST_ID],
                call.data[ATTR_THREAD_ID],
                call.data[ATTR_TURN_ID],
                cancel_turn=call.data[ATTR_CANCEL_TURN],
            )
        )

    async def handle_submit_input(call: ServiceCall) -> dict[str, Any]:
        api = _api_for_device(hass, call.data[ATTR_DEVICE_ID])
        return await _run_action(
            api.async_submit_input(
                call.data[ATTR_REQUEST_ID],
                call.data[ATTR_THREAD_ID],
                call.data[ATTR_TURN_ID],
                text=call.data.get(ATTR_TEXT),
                answers=call.data.get(ATTR_ANSWERS),
            )
        )

    async def handle_interrupt(call: ServiceCall) -> dict[str, Any]:
        api = _api_for_device(hass, call.data[ATTR_DEVICE_ID])
        return await _run_action(
            api.async_interrupt_turn(
                call.data[ATTR_THREAD_ID],
                call.data[ATTR_TURN_ID],
            )
        )

    request_schema = vol.Schema(
        {
            vol.Required(ATTR_DEVICE_ID): cv.string,
            vol.Required(ATTR_REQUEST_ID): cv.string,
            vol.Required(ATTR_THREAD_ID): cv.string,
            vol.Required(ATTR_TURN_ID): cv.string,
        }
    )
    hass.services.async_register(
        DOMAIN,
        SERVICE_APPROVE_REQUEST,
        handle_approve,
        schema=request_schema.extend({vol.Optional(ATTR_FOR_SESSION, default=False): cv.boolean}),
        supports_response=SupportsResponse.OPTIONAL,
    )
    hass.services.async_register(
        DOMAIN,
        SERVICE_REJECT_REQUEST,
        handle_reject,
        schema=request_schema.extend({vol.Optional(ATTR_CANCEL_TURN, default=False): cv.boolean}),
        supports_response=SupportsResponse.OPTIONAL,
    )
    hass.services.async_register(
        DOMAIN,
        SERVICE_SUBMIT_INPUT,
        handle_submit_input,
        schema=request_schema.extend(
            {
                vol.Optional(ATTR_TEXT): cv.string,
                vol.Optional(ATTR_ANSWERS): dict,
            }
        ),
        supports_response=SupportsResponse.OPTIONAL,
    )
    hass.services.async_register(
        DOMAIN,
        SERVICE_INTERRUPT_TURN,
        handle_interrupt,
        schema=vol.Schema(
            {
                vol.Required(ATTR_DEVICE_ID): cv.string,
                vol.Required(ATTR_THREAD_ID): cv.string,
                vol.Required(ATTR_TURN_ID): cv.string,
            }
        ),
        supports_response=SupportsResponse.OPTIONAL,
    )
    return True


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
    coordinator.async_start_stream()
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """Unload a Codex Monitor config entry."""
    unloaded = await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
    if unloaded:
        await entry.runtime_data.coordinator.async_shutdown()
    return unloaded


async def _async_reload_entry(hass: HomeAssistant, entry: ConfigEntry) -> None:
    """Reload an entry after options change."""
    await hass.config_entries.async_reload(entry.entry_id)


def _api_for_device(hass: HomeAssistant, device_id: str) -> CodexMonitorApi:
    """Resolve one configured agent from an HA device target."""
    device = dr.async_get(hass).async_get(device_id)
    if device is not None:
        for entry_id in device.config_entries:
            entry = hass.config_entries.async_get_entry(entry_id)
            if entry is not None and entry.domain == DOMAIN and entry.runtime_data is not None:
                return entry.runtime_data.api
    raise ServiceValidationError(
        translation_domain=DOMAIN,
        translation_key="device_not_loaded",
    )


async def _run_action(awaitable) -> dict[str, Any]:
    """Translate agent action failures into user-facing HA action errors."""
    try:
        return dict(await awaitable)
    except CodexMonitorApiError as err:
        raise ServiceValidationError(
            translation_domain=DOMAIN,
            translation_key="action_failed",
            translation_placeholders={"error": str(err)},
        ) from err
