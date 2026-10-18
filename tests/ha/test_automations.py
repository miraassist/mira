"""Mira's tasks as automations: the action rule, validation, who may manage them."""

import pytest

from homeassistant.exceptions import HomeAssistantError
from homeassistant.setup import async_setup_component

from custom_components.mira.automations import NotAllowed, check_actions, validate

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}

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


FORBIDDEN = {"domains": ["lock", "alarm_control_panel"], "entities": ["cover.garage"]}


def test_what_a_task_may_call():
    domains, entities = set(FORBIDDEN["domains"]), set(FORBIDDEN["entities"])
    allowed = [
        {"variables": {"x": 1}},
        {"action": "mira.decide", "data": {"question": "q"}, "response_variable": "d"},
        {"condition": "template", "value_template": "{{ d.answer }}"},
        {"action": "media_player.turn_on", "target": {"entity_id": "media_player.radio_bad"}},
        {"action": "notify.mobile_app_phone", "data": {"message": "Wasser läuft"}},
        {"action": "light.turn_on", "target": {"area_id": "bad"}},
        {"action": "cover.close_cover", "target": {"entity_id": "cover.rollo_wohnzimmer"}},
        {"if": [], "then": [{"action": "mira.assess", "data": {"goal": "g"}}], "else": [{"stop": "done"}]},
    ]
    check_actions(allowed, domains, entities)
    for forbidden in (
        [{"action": "lock.unlock", "target": {"entity_id": "lock.haustuer"}}],
        [{"service": "alarm_control_panel.alarm_disarm"}],
        [{"action": "cover.open_cover", "target": {"entity_id": "cover.garage"}}],
        [{"action": "cover.open_cover", "target": {"area_id": "garage"}}],
        [{"action": "script.turn_on", "target": {"entity_id": "script.x"}}],
        [{"action": "shell_command.wipe"}],
        [{"action": "homeassistant.restart"}],
        [{"choose": [{"sequence": [{"action": "lock.unlock"}]}]}],
        [{"device_id": "abc", "domain": "lock", "type": "unlock"}],
    ):
        with pytest.raises(NotAllowed):
            check_actions(forbidden, domains, entities)


def test_the_check_cannot_be_walked_around():
    """A template, a nesting or a step the check does not know must not hide
    what it keeps out; and what is fine stays fine inside any nesting."""
    domains, entities = set(FORBIDDEN["domains"]), set(FORBIDDEN["entities"])
    for sneaky in (
        [{"repeat": {"count": 2, "sequence": [{"action": "shell_command.x"}]}}],
        [{"action": "{{ 'shell' ~ '_command.x' }}"}],
        [{"service_template": "{{ 'lock.unlock' }}"}],
        [{"action": "light.turn_on", "target": {"entity_id": "{{ states('input_text.x') }}"}}],
        [{"action": "switch.turn_on", "target": {"entity_id": "all"}}],
        [{"event": "call_service", "event_data": {}}],
        [{"made_up_step": {"x": 1}}],
        [{"parallel": [{"sequence": [{"action": "lock.unlock"}]}]}],
        [{"if": [], "then": [{"repeat": {"while": [], "sequence": [{"action": "script.turn_on"}]}}]}],
        [{"choose": ["not a dict"]}],
        "not a list of steps",
    ):
        with pytest.raises(NotAllowed):
            check_actions(sneaky, domains, {"switch.garage_tor", *entities})
    check_actions(
        [
            {"repeat": {"count": 2, "sequence": [{"action": "light.toggle", "target": {"entity_id": "light.flur"}}]}},
            {"parallel": [{"sequence": [{"delay": "00:00:05"}, {"action": "notify.phone", "data": {"message": "{{ trigger.to_state.state }}"}}]}]},
            {"action": "mira.assess", "data": {"task": "{{ this.attributes.id }}", "goal": "g"}},
            {"scene": "scene.abend"},
            {"wait_for_trigger": [], "timeout": 5},
        ],
        domains,
        entities,
    )


async def test_validate_uses_home_assistant(hass):
    assert await async_setup_component(hass, "automation", {})
    await validate(hass, TASK)
    with pytest.raises(HomeAssistantError):
        await validate(hass, TASK | {"triggers": [{"trigger": "no_such_trigger"}]})
    with pytest.raises(HomeAssistantError):
        await validate(hass, TASK | {"actions": [{"action": "lock.unlock"}]}, FORBIDDEN)
    # Switching something harmless is the user's choice.
    await validate(hass, TASK | {"actions": [{"action": "light.turn_on", "target": {"entity_id": "light.bad"}}]}, FORBIDDEN)


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
        {
            "type": "mira/automation/validate",
            "config": TASK | {"actions": [{"action": "lock.unlock", "target": {"entity_id": "lock.tuer"}}]},
            "forbidden": FORBIDDEN,
        }
    )
    msg = await admin.receive_json()
    assert msg["result"]["valid"] is False and "lock.unlock" in msg["result"]["error"]

    # Another restricted user may not: only the agent's own user.
    other = await hass_ws_client(hass, hass_read_only_access_token)
    await other.send_json_auto_id({"type": "mira/automation/list"})
    msg = await other.receive_json()
    assert not msg["success"] and msg["error"]["code"] == "unauthorized"


async def test_labels_by_id_in_home_assistants_language(hass):
    """Mira knows her labels by id; their names follow HA's language and
    may be renamed without breaking anything."""
    from homeassistant.helpers import label_registry as lr

    from custom_components.mira.labels import LABEL_CONFIRM, LABEL_TASKS, ensure_labels

    hass.config.language = "de"
    ensure_labels(hass)
    labels = lr.async_get(hass)
    assert labels.async_get_label(LABEL_CONFIRM).name == "Mira: nachfragen"
    assert labels.async_get_label(LABEL_TASKS).description == "Aufgaben von Mira"
    labels.async_update(LABEL_CONFIRM, name="Vorsicht")
    ensure_labels(hass)
    assert labels.async_get_label(LABEL_CONFIRM).name == "Vorsicht"
    assert len(labels.labels) == 3  # Mira, Mira: nachfragen, Mira: einmalig


async def test_a_task_may_not_switch_what_is_labelled_confirm(hass):
    from homeassistant.helpers import entity_registry as er

    from custom_components.mira.labels import LABEL_CONFIRM, ensure_labels

    assert await async_setup_component(hass, "automation", {})
    ensure_labels(hass)
    registry = er.async_get(hass)
    pump = registry.async_get_or_create("switch", "test", "pump", suggested_object_id="pumpe")
    registry.async_update_entity(pump.entity_id, labels={LABEL_CONFIRM})
    task = TASK | {"actions": [{"action": "switch.turn_on", "target": {"entity_id": pump.entity_id}}]}
    with pytest.raises(NotAllowed):
        await validate(hass, task)
