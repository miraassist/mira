"""The integration and the agent may be different versions for a while
(docs/spec/versions.md): a repair says which one to update, and what an
older agent does not have is taken as missing, not as an error."""

import json
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

from homeassistant.helpers import issue_registry as ir
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from custom_components.mira.api import MiraClient
from custom_components.mira.const import DOMAIN, VERSION

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False, "problems": []}


def _minor_apart(delta: int) -> str:
    """A version a minor step away from this integration's."""
    major, minor, _ = VERSION.split(".")
    return f"{major}.{int(minor) + delta}.0"


async def test_older_agent_is_a_repair_until_updated(hass, aioclient_mock, entry):
    """VE-02: an agent a minor version behind asks to update the agent; once it
    is the same version, the repair goes away. A patch apart is fine."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": "v" + _minor_apart(-2)})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    registry = ir.async_get(hass)
    issue = registry.async_get_issue(DOMAIN, "version_mismatch")
    assert issue is not None and issue.translation_key == "version_agent_older"
    assert issue.translation_placeholders == {"integration": VERSION, "agent": _minor_apart(-2)}

    aioclient_mock.clear_requests()
    major, minor, _ = VERSION.split(".")
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": f"{major}.{minor}.7"})
    await entry.runtime_data.coordinator.async_refresh()
    assert registry.async_get_issue(DOMAIN, "version_mismatch") is None


async def test_newer_agent_asks_to_update_the_integration(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": _minor_apart(1)})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    issue = ir.async_get(hass).async_get_issue(DOMAIN, "version_mismatch")
    assert issue is not None and issue.translation_key == "version_agent_newer"


async def test_agent_without_a_version_in_status_uses_health(hass, aioclient_mock, entry):
    """VE-03: an agent that does not say its version in /v1/status (older) is
    judged by what /v1/health said at setup; without any version, no repair."""
    aioclient_mock.clear_requests()
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    aioclient_mock.get(
        f"{AGENT_URL}/v1/health",
        json={"status": "ok", "id": "01agent", "ha_user_id": "mira-user", "version": _minor_apart(-2)},
    )
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    issue = ir.async_get(hass).async_get_issue(DOMAIN, "version_mismatch")
    assert issue is not None and issue.translation_key == "version_agent_older"


async def test_no_version_no_repair(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert ir.async_get(hass).async_get_issue(DOMAIN, "version_mismatch") is None


async def test_agent_without_provider_list_knows_none(hass, aioclient_mock):
    """VE-04: an agent older than the provider list answers 404; then there are
    no providers to pick, only a model of one's own, instead of an error."""
    aioclient_mock.get(f"{AGENT_URL}/v1/setup/providers", status=404, json={"error": "not found"})
    client = MiraClient(async_get_clientsession(hass), AGENT_URL, "secret")
    assert await client.providers() == {}


def test_the_code_version_is_the_manifests():
    # The release raises manifest.json; VERSION is what the integration compares with the agent and shows on the
    # device. 0.16.0 went out with VERSION still 0.15.2 and a wrong "versions do not match" repair.
    manifest = json.loads((Path(__file__).parents[2] / "custom_components/mira/manifest.json").read_text())
    assert VERSION == manifest["version"]


async def test_the_integration_says_its_version_with_every_request(hass, aioclient_mock, entry):
    """A newer service learns the integration is older and asks for its update."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    calls = [c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/status")]
    assert calls and calls[-1][3]["X-Mira-Integration"] == VERSION


async def test_an_older_agent_on_ha_os_reloads_the_app_store_once(hass, aioclient_mock, entry):
    """The app's update is asked for at once, once per integration."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": _minor_apart(-2)})
    client = MagicMock()
    client.store.reload = AsyncMock()
    with (
        patch("custom_components.mira.coordinator.is_hassio", return_value=True),
        patch("homeassistant.components.hassio.get_supervisor_client", return_value=client),
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
        await entry.runtime_data.coordinator.async_refresh()
        await hass.async_block_till_done()
    assert client.store.reload.await_count == 1


async def test_without_ha_os_no_store_is_reloaded(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": _minor_apart(-2)})
    with patch("homeassistant.components.hassio.get_supervisor_client") as get:
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    get.assert_not_called()
