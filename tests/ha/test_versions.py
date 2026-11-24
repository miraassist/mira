"""The integration and the service have their own versions; each names the
oldest of the other it works with, and a repair says when one is below it
(docs/spec/versions.md, Minimum versions instead of one version). What an
older service does not have is taken as missing, not as an error."""

import json
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

from homeassistant.helpers import issue_registry as ir
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from custom_components.mira.api import MiraClient
from custom_components.mira.const import DOMAIN, MIN_SERVICE, VERSION

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False, "problems": []}


def _apart(version: str, delta: int) -> str:
    """A version a minor step away from version."""
    major, minor, _ = version.split(".")
    return f"{major}.{int(minor) + delta}.0"


BELOW_SERVICE = _apart(MIN_SERVICE, -2)  # a service below this integration's minimum


async def test_VE10_a_service_below_the_minimum_is_a_repair_until_updated(hass, aioclient_mock, entry):
    """VE-10: a service below MIN_SERVICE asks to update the service (its container
    here); at or above the minimum the repair goes away."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": "v" + BELOW_SERVICE})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    registry = ir.async_get(hass)
    issue = registry.async_get_issue(DOMAIN, "service_too_old")
    assert issue is not None and issue.translation_key == "service_too_old"
    assert issue.translation_placeholders == {"minimum": MIN_SERVICE, "service": BELOW_SERVICE}

    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": MIN_SERVICE})
    await entry.runtime_data.coordinator.async_refresh()
    assert registry.async_get_issue(DOMAIN, "service_too_old") is None


async def test_VE10_on_ha_os_the_repair_names_the_app(hass, aioclient_mock, entry):
    """VE-10 (and VE-06's rule): on Home Assistant OS the repair for a service below
    the minimum names only the HA app's update."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": BELOW_SERVICE})
    client = MagicMock()
    client.store.reload = AsyncMock()
    with (
        patch("custom_components.mira.coordinator.is_hassio", return_value=True),
        patch("homeassistant.components.hassio.get_supervisor_client", return_value=client),
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    issue = ir.async_get(hass).async_get_issue(DOMAIN, "service_too_old")
    assert issue is not None and issue.translation_key == "service_too_old_app"


async def test_VE09_an_integration_below_the_services_minimum_is_a_repair(hass, aioclient_mock, entry):
    """VE-09: the service's min_integration above this integration asks to update the
    integration; at or above it none."""
    aioclient_mock.get(
        f"{AGENT_URL}/v1/status", json={**STATUS, "version": _apart(VERSION, 3), "min_integration": _apart(VERSION, 1)}
    )
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    registry = ir.async_get(hass)
    issue = registry.async_get_issue(DOMAIN, "integration_too_old")
    assert issue is not None and issue.translation_key == "integration_too_old"
    assert issue.translation_placeholders == {"minimum": _apart(VERSION, 1), "integration": VERSION}

    aioclient_mock.clear_requests()
    aioclient_mock.get(
        f"{AGENT_URL}/v1/status", json={**STATUS, "version": _apart(VERSION, 3), "min_integration": VERSION}
    )
    await entry.runtime_data.coordinator.async_refresh()
    assert registry.async_get_issue(DOMAIN, "integration_too_old") is None


async def test_VE09_a_service_without_min_integration_asks_nothing(hass, aioclient_mock, entry):
    """VE-09: an older service sends no min_integration: no repair for the integration."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": _apart(VERSION, 3)})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert ir.async_get(hass).async_get_issue(DOMAIN, "integration_too_old") is None


async def test_VE11_being_apart_is_no_problem_and_the_old_repair_goes(hass, aioclient_mock, entry):
    """VE-11: a service several versions ahead whose minimum this integration meets
    has no repair, and the old repair for different versions is deleted."""
    ir.async_create_issue(
        hass, DOMAIN, "version_mismatch", is_fixable=False, severity=ir.IssueSeverity.WARNING, translation_key="x"
    )
    aioclient_mock.get(
        f"{AGENT_URL}/v1/status", json={**STATUS, "version": _apart(VERSION, 5), "min_integration": VERSION}
    )
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    registry = ir.async_get(hass)
    for issue in ("version_mismatch", "service_too_old", "integration_too_old"):
        assert registry.async_get_issue(DOMAIN, issue) is None, issue


async def test_VE12_a_version_that_is_no_xyz_makes_no_repair(hass, aioclient_mock, entry):
    """VE-12: "dev" (a build without a version) is never below a minimum, nor is an
    odd min_integration."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": "dev", "min_integration": "soon"})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    registry = ir.async_get(hass)
    assert registry.async_get_issue(DOMAIN, "service_too_old") is None
    assert registry.async_get_issue(DOMAIN, "integration_too_old") is None


async def test_agent_without_a_version_in_status_uses_health(hass, aioclient_mock, entry):
    """VE-03: a service that does not say its version in /v1/status (older) is
    judged by what /v1/health said at setup; without any version, no repair."""
    aioclient_mock.clear_requests()
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    aioclient_mock.get(
        f"{AGENT_URL}/v1/health",
        json={"status": "ok", "id": "01agent", "ha_user_id": "mira-user", "version": BELOW_SERVICE},
    )
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    issue = ir.async_get(hass).async_get_issue(DOMAIN, "service_too_old")
    assert issue is not None and issue.translation_key == "service_too_old"


async def test_no_version_no_repair(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    registry = ir.async_get(hass)
    assert registry.async_get_issue(DOMAIN, "service_too_old") is None
    assert registry.async_get_issue(DOMAIN, "integration_too_old") is None


async def test_agent_without_provider_list_knows_none(hass, aioclient_mock):
    """VE-04: a service older than the provider list answers 404; then there are
    no providers to pick, only a model of one's own, instead of an error."""
    aioclient_mock.get(f"{AGENT_URL}/v1/setup/providers", status=404, json={"error": "not found"})
    client = MiraClient(async_get_clientsession(hass), AGENT_URL, "secret")
    assert await client.providers() == {}


def test_the_code_version_is_the_manifests():
    # The release raises manifest.json; VERSION is what the integration compares with the service and shows on the
    # device. 0.16.0 went out with VERSION still 0.15.2 and a wrong "versions do not match" repair.
    manifest = json.loads((Path(__file__).parents[2] / "custom_components/mira/manifest.json").read_text())
    assert VERSION == manifest["version"]


async def test_the_integration_says_its_version_with_every_request(hass, aioclient_mock, entry):
    """The service learns the integration's version and asks for its update when it
    is below its minimum."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    calls = [c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/status")]
    assert calls and calls[-1][3]["X-Mira-Integration"] == VERSION


async def test_a_service_below_the_minimum_on_ha_os_reloads_the_app_store_once(hass, aioclient_mock, entry):
    """The app's update is asked for at once, once per integration."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": BELOW_SERVICE})
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


async def test_a_service_older_but_not_below_the_minimum_reloads_no_app_store(hass, aioclient_mock, entry):
    """Older than this integration is no reason any more: only below MIN_SERVICE."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": "0.40.0"})
    client = MagicMock()
    client.store.reload = AsyncMock()
    with (
        patch("custom_components.mira.coordinator.VERSION", "0.43.2"),
        patch("custom_components.mira.coordinator.MIN_SERVICE", "0.40.0"),
        patch("custom_components.mira.coordinator.is_hassio", return_value=True),
        patch("homeassistant.components.hassio.get_supervisor_client", return_value=client),
    ):
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    assert client.store.reload.await_count == 0
    assert ir.async_get(hass).async_get_issue(DOMAIN, "service_too_old") is None


async def test_without_ha_os_no_store_is_reloaded(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={**STATUS, "version": BELOW_SERVICE})
    with patch("homeassistant.components.hassio.get_supervisor_client") as get:
        assert await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    get.assert_not_called()
