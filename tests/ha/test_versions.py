"""The integration and the agent may be different versions for a while
(docs/spec/versionen.md): a repair says which one to update, and what an
older agent does not have is taken as missing, not as an error."""

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
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": "01agent", "ha_user_id": "mira-user",
                                                       "version": _minor_apart(-2)})
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
