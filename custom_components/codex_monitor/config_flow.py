"""Config flow for Codex Monitor."""

from __future__ import annotations

import logging
from collections.abc import Mapping
from typing import Any

import voluptuous as vol
from homeassistant import config_entries
from homeassistant.config_entries import ConfigEntry, ConfigFlowResult
from homeassistant.core import callback
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import (
    CodexMonitorApi,
    CodexMonitorCannotConnect,
    CodexMonitorInvalidResponse,
)
from .const import (
    CONF_NAME,
    CONF_SCAN_INTERVAL,
    CONF_URL,
    DEFAULT_SCAN_INTERVAL,
    DEFAULT_URL,
    DOMAIN,
    MAX_SCAN_INTERVAL,
    MIN_SCAN_INTERVAL,
)
from .util import normalize_url

_LOGGER = logging.getLogger(__name__)


async def _async_probe(
    hass,
    url: str,
) -> tuple[Mapping[str, Any], Mapping[str, Any]]:
    api = CodexMonitorApi(async_get_clientsession(hass), url)
    return await api.async_probe()


class CodexMonitorConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for a Codex Monitor Agent."""

    VERSION = 1

    async def async_step_user(
        self,
        user_input: dict[str, Any] | None = None,
    ) -> ConfigFlowResult:
        """Configure a new agent from the UI."""
        errors: dict[str, str] = {}
        if user_input is not None:
            try:
                url = normalize_url(user_input[CONF_URL])
                version, status = await _async_probe(self.hass, url)
            except ValueError:
                errors["base"] = "invalid_url"
            except CodexMonitorCannotConnect:
                errors["base"] = "cannot_connect"
            except CodexMonitorInvalidResponse:
                errors["base"] = "invalid_response"
            except Exception:  # pragma: no cover - defensive config-flow boundary
                _LOGGER.exception("Unexpected error while connecting to Codex Monitor")
                errors["base"] = "unknown"
            else:
                installation_id = str(version["installation_id"])
                await self.async_set_unique_id(installation_id)
                self._abort_if_unique_id_configured()

                requested_name = str(user_input.get(CONF_NAME, "")).strip()
                host = status.get("host", {})
                host_name = host.get("name") if isinstance(host, Mapping) else None
                if not isinstance(host_name, str):
                    host_name = None
                title = requested_name or host_name or f"Codex Monitor {installation_id[:8]}"
                data = {CONF_URL: url}
                if requested_name:
                    data[CONF_NAME] = requested_name
                return self.async_create_entry(title=title, data=data)

        schema = vol.Schema(
            {
                vol.Required(
                    CONF_URL,
                    default=(user_input or {}).get(CONF_URL, DEFAULT_URL),
                ): str,
                vol.Optional(
                    CONF_NAME,
                    default=(user_input or {}).get(CONF_NAME, ""),
                ): str,
            }
        )
        return self.async_show_form(
            step_id="user",
            data_schema=schema,
            errors=errors,
        )

    async def async_step_reconfigure(
        self,
        user_input: dict[str, Any] | None = None,
    ) -> ConfigFlowResult:
        """Allow the agent URL to be changed without removing entities."""
        entry = self._get_reconfigure_entry()
        errors: dict[str, str] = {}
        if user_input is not None:
            try:
                url = normalize_url(user_input[CONF_URL])
                version, _ = await _async_probe(self.hass, url)
            except ValueError:
                errors["base"] = "invalid_url"
            except CodexMonitorCannotConnect:
                errors["base"] = "cannot_connect"
            except CodexMonitorInvalidResponse:
                errors["base"] = "invalid_response"
            except Exception:  # pragma: no cover - defensive config-flow boundary
                _LOGGER.exception("Unexpected error while reconnecting Codex Monitor")
                errors["base"] = "unknown"
            else:
                await self.async_set_unique_id(str(version["installation_id"]))
                self._abort_if_unique_id_mismatch()
                return self.async_update_reload_and_abort(
                    entry,
                    data_updates={CONF_URL: url},
                )

        return self.async_show_form(
            step_id="reconfigure",
            data_schema=vol.Schema(
                {
                    vol.Required(
                        CONF_URL,
                        default=(user_input or {}).get(CONF_URL, entry.data[CONF_URL]),
                    ): str
                }
            ),
            errors=errors,
        )

    @staticmethod
    @callback
    def async_get_options_flow(config_entry: ConfigEntry) -> config_entries.OptionsFlow:
        """Return the options flow handler."""
        return CodexMonitorOptionsFlow(config_entry)


class CodexMonitorOptionsFlow(config_entries.OptionsFlow):
    """Configure polling options."""

    def __init__(self, config_entry: ConfigEntry) -> None:
        self._config_entry = config_entry

    async def async_step_init(
        self,
        user_input: dict[str, Any] | None = None,
    ) -> ConfigFlowResult:
        """Edit the polling interval."""
        if user_input is not None:
            return self.async_create_entry(title="", data=user_input)

        return self.async_show_form(
            step_id="init",
            data_schema=vol.Schema(
                {
                    vol.Required(
                        CONF_SCAN_INTERVAL,
                        default=self._config_entry.options.get(
                            CONF_SCAN_INTERVAL,
                            DEFAULT_SCAN_INTERVAL,
                        ),
                    ): vol.All(
                        vol.Coerce(int),
                        vol.Range(min=MIN_SCAN_INTERVAL, max=MAX_SCAN_INTERVAL),
                    )
                }
            ),
        )
