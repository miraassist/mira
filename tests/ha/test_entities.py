"""Status sensor, quiet mode switch and the add_notice service."""

import pytest

from homeassistant.config_entries import ConfigEntryState
from homeassistant.const import STATE_OFF, STATE_ON, STATE_UNAVAILABLE
from homeassistant.exceptions import ServiceValidationError

from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

SENSOR = "sensor.mira_pending_notices"
SWITCH = "switch.mira_quiet_mode"


async def _setup(hass, entry, aioclient_mock, status=None):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=status or {"pending_notices": 2, "quiet_mode": False})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


async def test_status_entities(hass, aioclient_mock, entry):
    await _setup(hass, entry, aioclient_mock)
    assert hass.states.get(SENSOR).state == "2"
    assert hass.states.get(SWITCH).state == STATE_OFF


async def test_device_shows_versions(hass, aioclient_mock, entry):
    """Read at every setup, so a reload after copying new files shows the
    new version (Home Assistant's own manifest cache does not)."""
    import json
    from pathlib import Path

    from homeassistant.helpers import device_registry as dr

    await _setup(hass, entry, aioclient_mock)
    manifest = json.loads((Path(__file__).parents[2] / "custom_components/mira/manifest.json").read_text())
    device = dr.async_get(hass).async_get_device_by_identifier(("mira", entry.entry_id), entry.entry_id)
    assert device.sw_version == f"Integration {manifest['version']} · Agent 0.3.0"


async def test_quiet_mode_switch(hass, aioclient_mock, entry):
    await _setup(hass, entry, aioclient_mock)
    aioclient_mock.put(f"{AGENT_URL}/v1/quiet_mode", json={"quiet_mode": True})
    await hass.services.async_call("switch", "turn_on", {"entity_id": SWITCH}, blocking=True)
    assert hass.states.get(SWITCH).state == STATE_ON
    assert aioclient_mock.mock_calls[-1][2] == {"enabled": True}


async def test_agent_down_at_boot_keeps_conversation(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", exc=TimeoutError())
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert entry.state is ConfigEntryState.LOADED
    assert hass.states.get("conversation.mira") is not None
    assert hass.states.get(SENSOR).state == STATE_UNAVAILABLE


async def test_add_notice_service(hass, aioclient_mock, entry):
    await _setup(hass, entry, aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/notices", json={"id": "01J", "created": True})
    result = await hass.services.async_call(
        DOMAIN,
        "add_notice",
        {"text": "Die Waschmaschine ist fertig.", "importance": 1, "dedupe_key": "washer:done", "confidentiality": "normal"},
        blocking=True,
        return_response=True,
    )
    assert result == {"id": "01J", "created": True}
    sent = [c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/notices")][0][2]
    assert sent == {"text": "Die Waschmaschine ist fertig.", "importance": 1, "dedupe_key": "washer:done", "confidentiality": "normal"}


async def test_add_notice_without_agent(hass):
    from homeassistant.setup import async_setup_component

    assert await async_setup_component(hass, DOMAIN, {})
    with pytest.raises(ServiceValidationError):
        await hass.services.async_call(DOMAIN, "add_notice", {"text": "x"}, blocking=True)
