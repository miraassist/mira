"""Mira's tasks as automations: the action rule, validation, who may manage them."""

import pytest

from homeassistant.exceptions import HomeAssistantError
from homeassistant.setup import async_setup_component

from custom_components.mira.automations import NotAllowed, check_actions, validate

from .conftest import AGENT_URL

STATUS = {"pending_notices": 0, "quiet_mode": False}

TASK = {
    "alias": "Wasser",
    "triggers": [{"trigger": "time_pattern", "minutes": 15}],
    "actions": [
        {
            "action": "mira.assess",
            "data": {"goal": "Achte auf den Wasserverbrauch.", "data": [{"entity_id": "sensor.w", "view": "same_hour"}]},
        }
    ],
}


def test_only_mira_actions():
    check_actions(TASK["actions"])
    check_actions(
        [
            {"variables": {"x": 1}},
            {"action": "mira.decide", "data": {"question": "q"}, "response_variable": "d"},
            {"condition": "template", "value_template": "{{ d.answer }}"},
            {"choose": [{"conditions": [], "sequence": [{"action": "mira.add_notice", "data": {"text": "t"}}]}]},
            {"if": [], "then": [{"action": "mira.assess", "data": {"goal": "g"}}], "else": [{"stop": "done"}]},
        ]
    )
    for forbidden in (
        [{"action": "light.turn_on", "target": {"entity_id": "light.x"}}],
        [{"service": "lock.unlock"}],
        [{"choose": [{"sequence": [{"action": "switch.turn_off"}]}]}],
        [{"event": "something"}],
        [{"device_id": "abc", "domain": "light", "type": "turn_on"}],
    ):
        with pytest.raises(NotAllowed):
            check_actions(forbidden)


async def test_validate_uses_home_assistant(hass):
    assert await async_setup_component(hass, "automation", {})
    await validate(hass, TASK)
    with pytest.raises(HomeAssistantError):
        await validate(hass, TASK | {"triggers": [{"trigger": "no_such_trigger"}]})
    with pytest.raises(HomeAssistantError):
        await validate(hass, TASK | {"actions": [{"action": "light.turn_on"}]})


async def test_commands_only_for_the_agent(hass, aioclient_mock, entry, hass_ws_client, hass_read_only_access_token):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await async_setup_component(hass, "automation", {})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # An admin may (the default test client is one).
    admin = await hass_ws_client(hass)
    await admin.send_json_auto_id({"type": "mira/automation/validate", "config": TASK})
    msg = await admin.receive_json()
    assert msg["success"] and msg["result"] == {"valid": True}
    await admin.send_json_auto_id(
        {"type": "mira/automation/validate", "config": TASK | {"actions": [{"action": "light.turn_on"}]}}
    )
    msg = await admin.receive_json()
    assert msg["result"]["valid"] is False and "mira" in msg["result"]["error"]

    # Another restricted user may not: only the agent's own user.
    other = await hass_ws_client(hass, hass_read_only_access_token)
    await other.send_json_auto_id({"type": "mira/automation/list"})
    msg = await other.receive_json()
    assert not msg["success"] and msg["error"]["code"] == "unauthorized"
