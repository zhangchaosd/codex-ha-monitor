"""Home Assistant config, discovery, and authentication flow tests."""

from __future__ import annotations

from ipaddress import ip_address

from homeassistant.config_entries import SOURCE_REAUTH, SOURCE_USER, SOURCE_ZEROCONF
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.codex_monitor.const import CONF_TOKEN, CONF_URL, DOMAIN


def _mock_probe(aioclient_mock, base_url: str) -> None:
    aioclient_mock.get(
        f"{base_url}/api/v1/version",
        json={
            "schema_version": "1.1",
            "installation_id": "installation",
            "agent": {"version": "0.4.0"},
            "codex_cli": {"version": "0.145.0"},
            "app_server": {},
        },
    )
    aioclient_mock.get(
        f"{base_url}/api/v1/status",
        json={
            "schema_version": "1.1",
            "installation_id": "installation",
            "host": {"name": "Discovered Mac"},
            "summary": {},
            "codex": {},
        },
    )


async def test_user_flow_creates_stable_entry(hass, aioclient_mock) -> None:
    """Manual setup should validate both endpoints and store the stable identity."""
    _mock_probe(aioclient_mock, "http://agent:8765")

    result = await hass.config_entries.flow.async_init(
        DOMAIN,
        context={"source": SOURCE_USER},
        data={CONF_URL: "http://agent:8765/", CONF_TOKEN: "secret"},
    )

    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["title"] == "Discovered Mac"
    assert result["data"] == {CONF_URL: "http://agent:8765", CONF_TOKEN: "secret"}
    assert result["result"].unique_id == "installation"


async def test_empty_token_is_auth_error_not_url_error(hass) -> None:
    """Credential mistakes should lead users to the authentication field."""
    result = await hass.config_entries.flow.async_init(
        DOMAIN,
        context={"source": SOURCE_USER},
        data={CONF_URL: "http://agent:8765", CONF_TOKEN: ""},
    )

    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "invalid_auth"}


async def test_zeroconf_discovery_collects_token_and_creates_entry(hass, aioclient_mock) -> None:
    """mDNS should supply identity/address while the user supplies credentials."""
    discovery = ZeroconfServiceInfo(
        ip_address=ip_address("192.168.1.20"),
        ip_addresses=[ip_address("192.168.1.20")],
        port=8765,
        hostname="mac.local.",
        type="_codex-monitor._tcp.local.",
        name="Codex Monitor Mac._codex-monitor._tcp.local.",
        properties={"installation_id": "installation", "schema_version": "1.1"},
    )
    result = await hass.config_entries.flow.async_init(
        DOMAIN,
        context={"source": SOURCE_ZEROCONF},
        data=discovery,
    )
    assert result["type"] is FlowResultType.FORM
    assert result["step_id"] == "zeroconf_confirm"

    _mock_probe(aioclient_mock, "http://192.168.1.20:8765")
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"],
        {CONF_TOKEN: "secret"},
    )

    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["result"].unique_id == "installation"
    assert result["data"][CONF_URL] == "http://192.168.1.20:8765"


async def test_reauth_replaces_rejected_token_without_replacing_entry(hass, aioclient_mock) -> None:
    """A rejected token should be replaceable while stable entity identity remains."""
    entry = MockConfigEntry(
        domain=DOMAIN,
        title="Codex Mac",
        unique_id="installation",
        data={CONF_URL: "http://agent:8765", CONF_TOKEN: "old-token"},
    )
    entry.add_to_hass(hass)

    result = await hass.config_entries.flow.async_init(
        DOMAIN,
        context={"source": SOURCE_REAUTH, "entry_id": entry.entry_id},
        data=entry.data,
    )
    assert result["type"] is FlowResultType.FORM
    assert result["step_id"] == "reauth_confirm"

    _mock_probe(aioclient_mock, "http://agent:8765")
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"],
        {CONF_TOKEN: "new-token"},
    )

    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "reauth_successful"
    assert entry.data[CONF_TOKEN] == "new-token"
