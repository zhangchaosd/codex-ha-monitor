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
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .api import (
    CodexMonitorApi,
    CodexMonitorAuthenticationError,
    CodexMonitorCannotConnect,
    CodexMonitorInvalidResponse,
)
from .const import (
    CONF_NAME,
    CONF_SCAN_INTERVAL,
    CONF_TOKEN,
    CONF_URL,
    DEFAULT_SCAN_INTERVAL,
    DEFAULT_URL,
    DOMAIN,
    MAX_SCAN_INTERVAL,
    MIN_SCAN_INTERVAL,
)
from .util import normalize_url

_LOGGER = logging.getLogger(__name__)


class _EmptyTokenError(ValueError):
    """Internal distinction between an invalid URL and missing credentials."""


async def _async_probe(
    hass,
    url: str,
    token: str,
) -> tuple[Mapping[str, Any], Mapping[str, Any]]:
    api = CodexMonitorApi(async_get_clientsession(hass), url, token)
    return await api.async_probe()


class CodexMonitorConfigFlow(config_entries.ConfigFlow, domain=DOMAIN):
    """Handle a config flow for a Codex Monitor Agent."""

    VERSION = 1

    def __init__(self) -> None:
        """Initialize discovery state."""
        self._discovered_url: str | None = None
        self._discovered_name: str | None = None

    async def async_step_user(
        self,
        user_input: dict[str, Any] | None = None,
    ) -> ConfigFlowResult:
        """Configure a new agent from the UI."""
        errors: dict[str, str] = {}
        if user_input is not None:
            try:
                url = normalize_url(user_input[CONF_URL])
                token = str(user_input[CONF_TOKEN]).strip()
                if not token:
                    raise _EmptyTokenError("token is empty")
                version, status = await _async_probe(self.hass, url, token)
            except _EmptyTokenError:
                errors["base"] = "invalid_auth"
            except ValueError:
                errors["base"] = "invalid_url"
            except CodexMonitorCannotConnect:
                errors["base"] = "cannot_connect"
            except CodexMonitorAuthenticationError:
                errors["base"] = "invalid_auth"
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
                data = {CONF_URL: url, CONF_TOKEN: token}
                if requested_name:
                    data[CONF_NAME] = requested_name
                return self.async_create_entry(title=title, data=data)

        schema = vol.Schema(
            {
                vol.Required(
                    CONF_URL,
                    default=(user_input or {}).get(CONF_URL, DEFAULT_URL),
                ): str,
                vol.Required(CONF_TOKEN): str,
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
                token = str(user_input[CONF_TOKEN]).strip()
                if not token:
                    raise _EmptyTokenError("token is empty")
                version, _ = await _async_probe(self.hass, url, token)
            except _EmptyTokenError:
                errors["base"] = "invalid_auth"
            except ValueError:
                errors["base"] = "invalid_url"
            except CodexMonitorCannotConnect:
                errors["base"] = "cannot_connect"
            except CodexMonitorAuthenticationError:
                errors["base"] = "invalid_auth"
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
                    data_updates={CONF_URL: url, CONF_TOKEN: token},
                )

        return self.async_show_form(
            step_id="reconfigure",
            data_schema=vol.Schema(
                {
                    vol.Required(
                        CONF_URL,
                        default=(user_input or {}).get(CONF_URL, entry.data[CONF_URL]),
                    ): str,
                    vol.Required(
                        CONF_TOKEN,
                        default=(user_input or {}).get(CONF_TOKEN, entry.data.get(CONF_TOKEN, "")),
                    ): str,
                }
            ),
            errors=errors,
        )

    async def async_step_reauth(
        self,
        entry_data: Mapping[str, Any],
    ) -> ConfigFlowResult:
        """Start replacing an invalid agent token."""
        return await self.async_step_reauth_confirm()

    async def async_step_reauth_confirm(
        self,
        user_input: dict[str, Any] | None = None,
    ) -> ConfigFlowResult:
        """Validate and store a replacement bearer token."""
        entry = self._get_reauth_entry()
        errors: dict[str, str] = {}
        if user_input is not None:
            token = str(user_input[CONF_TOKEN]).strip()
            try:
                if not token:
                    raise ValueError("token is empty")
                version, _ = await _async_probe(self.hass, entry.data[CONF_URL], token)
            except ValueError:
                errors["base"] = "invalid_auth"
            except CodexMonitorAuthenticationError:
                errors["base"] = "invalid_auth"
            except CodexMonitorCannotConnect:
                errors["base"] = "cannot_connect"
            except CodexMonitorInvalidResponse:
                errors["base"] = "invalid_response"
            else:
                await self.async_set_unique_id(str(version["installation_id"]))
                self._abort_if_unique_id_mismatch()
                return self.async_update_reload_and_abort(
                    entry,
                    data_updates={CONF_TOKEN: token},
                )
        return self.async_show_form(
            step_id="reauth_confirm",
            data_schema=vol.Schema({vol.Required(CONF_TOKEN): str}),
            errors=errors,
        )

    async def async_step_zeroconf(
        self,
        discovery_info: ZeroconfServiceInfo,
    ) -> ConfigFlowResult:
        """Discover a LAN agent by its stable installation identity."""
        installation_id = discovery_info.properties.get("installation_id")
        if not installation_id:
            return self.async_abort(reason="invalid_discovery")
        host = discovery_info.host
        formatted_host = f"[{host}]" if ":" in host and not host.startswith("[") else host
        self._discovered_url = f"http://{formatted_host}:{discovery_info.port}"
        self._discovered_name = discovery_info.name.split("._codex-monitor", 1)[0]
        await self.async_set_unique_id(installation_id)
        self._abort_if_unique_id_configured(updates={CONF_URL: self._discovered_url})
        self.context["title_placeholders"] = {"name": self._discovered_name}
        return await self.async_step_zeroconf_confirm()

    async def async_step_zeroconf_confirm(
        self,
        user_input: dict[str, Any] | None = None,
    ) -> ConfigFlowResult:
        """Confirm a discovered agent and collect its bearer token."""
        if self._discovered_url is None:
            return self.async_abort(reason="invalid_discovery")
        errors: dict[str, str] = {}
        if user_input is not None:
            token = str(user_input[CONF_TOKEN]).strip()
            try:
                if not token:
                    raise ValueError("token is empty")
                version, status = await _async_probe(self.hass, self._discovered_url, token)
            except ValueError:
                errors["base"] = "invalid_auth"
            except CodexMonitorAuthenticationError:
                errors["base"] = "invalid_auth"
            except CodexMonitorCannotConnect:
                errors["base"] = "cannot_connect"
            except CodexMonitorInvalidResponse:
                errors["base"] = "invalid_response"
            else:
                installation_id = str(version["installation_id"])
                if installation_id != self.unique_id:
                    return self.async_abort(reason="discovery_changed")
                host = status.get("host", {})
                host_name = host.get("name") if isinstance(host, Mapping) else None
                title = (
                    str(user_input.get(CONF_NAME, "")).strip()
                    or (host_name if isinstance(host_name, str) else None)
                    or self._discovered_name
                    or f"Codex Monitor {installation_id[:8]}"
                )
                return self.async_create_entry(
                    title=title,
                    data={CONF_URL: self._discovered_url, CONF_TOKEN: token},
                )
        return self.async_show_form(
            step_id="zeroconf_confirm",
            data_schema=vol.Schema(
                {
                    vol.Required(CONF_TOKEN): str,
                    vol.Optional(CONF_NAME, default=""): str,
                }
            ),
            errors=errors,
            description_placeholders={"url": self._discovered_url},
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
