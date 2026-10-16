"""Status sensor, quiet mode switch and the send_message service."""

import pytest

from homeassistant.config_entries import ConfigEntryState
from homeassistant.const import STATE_OFF, STATE_ON, STATE_UNAVAILABLE
from homeassistant.exceptions import ServiceValidationError

from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

SENSOR = "sensor.mira_pending_messages"
SWITCH = "switch.mira_quiet_mode"


async def _setup(hass, entry, aioclient_mock, status=None):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=status or {"pending_messages": 2, "quiet_mode": False})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


async def test_status_entities(hass, aioclient_mock, entry):
    await _setup(hass, entry, aioclient_mock)
    assert hass.states.get(SENSOR).state == "2"
    assert hass.states.get(SWITCH).state == STATE_OFF


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


async def test_send_message_service(hass, aioclient_mock, entry):
    await _setup(hass, entry, aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/messages", json={"id": "01J", "created": True})
    result = await hass.services.async_call(
        DOMAIN,
        "send_message",
        {"text": "Die Waschmaschine ist fertig.", "importance": 1, "dedupe_key": "washer:done", "confidentiality": "normal"},
        blocking=True,
        return_response=True,
    )
    assert result == {"id": "01J", "created": True}
    sent = [c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/messages")][0][2]
    assert sent == {"text": "Die Waschmaschine ist fertig.", "importance": 1, "dedupe_key": "washer:done", "confidentiality": "normal"}


async def test_send_message_without_agent(hass):
    from homeassistant.setup import async_setup_component

    assert await async_setup_component(hass, DOMAIN, {})
    with pytest.raises(ServiceValidationError):
        await hass.services.async_call(DOMAIN, "send_message", {"text": "x"}, blocking=True)


async def test_notify_entities_send_through_mira(hass, aioclient_mock, entry):
    """notify.send_message on Mira's entities becomes a message to the house
    or a person; HA's own action, no Mira-specific one."""
    from homeassistant.helpers import entity_registry as er

    registry = er.async_get(hass)
    anna = registry.async_get_or_create("person", "person", "anna", suggested_object_id="anna")
    hass.states.async_set(anna.entity_id, "home", {"id": "anna", "friendly_name": "Anna"})
    aioclient_mock.post(f"{AGENT_URL}/v1/messages", json={"id": "m1"})
    await _setup(hass, entry, aioclient_mock)
    ids = {e.unique_id.rsplit("_", 1)[-1]: e.entity_id for e in er.async_entries_for_config_entry(registry, entry.entry_id) if e.domain == "notify"}
    assert set(ids) == {"house", "anna"}
    await hass.services.async_call("notify", "send_message", {"entity_id": ids["anna"], "message": "Waschmaschine fertig."}, blocking=True)
    _, _, sent, _ = next(c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/messages"))
    assert sent == {"text": "Waschmaschine fertig.", "receiver": "anna", "importance": 1}
