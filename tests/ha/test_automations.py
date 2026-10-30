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


async def house(hass):
    """Home Assistant with the actions a task uses, described as HA does,
    and a few things to reach: a scene, scripts, a group, an area, a device."""
    from homeassistant.components.homeassistant.exposed_entities import async_expose_entity
    from homeassistant.helpers import area_registry as ar, device_registry as dr, entity_registry as er
    from pytest_homeassistant_custom_component.common import MockConfigEntry

    for domain in ("light", "cover", "switch", "media_player", "lock", "alarm_control_panel"):
        assert await async_setup_component(hass, domain, {})
    assert await async_setup_component(
        hass,
        "scene",
        {
            "scene": [
                {"name": "Abend", "entities": {"light.flur": "on"}},
                {"name": "Nacht", "entities": {"light.flur": "off", "lock.haustuer": "locked"}},
            ]
        },
    )
    assert await async_setup_component(
        hass,
        "script",
        {"script": {"kaffee": {"sequence": [{"delay": 1}]}, "geheim": {"sequence": [{"delay": 1}]}}},
    )
    async_expose_entity(hass, "conversation", "script.kaffee", True)
    assert await async_setup_component(hass, "group", {"group": {"aussen": {"entities": ["cover.garage", "light.hof"]}}})
    # Actions that name no entities: they could switch anything.
    for domain, service in (("mqtt", "publish"), ("rest_command", "garage_auf"), ("conversation", "process")):
        hass.services.async_register(domain, service, lambda call: None)
    await hass.async_block_till_done()

    config = MockConfigEntry(domain="test")
    config.add_to_hass(hass)
    garage = dr.async_get(hass).async_get_or_create(config_entry_id=config.entry_id, identifiers={("test", "garage")})
    entities = er.async_get(hass)
    door = entities.async_get_or_create("cover", "test", "door", suggested_object_id="garage", device_id=garage.id)
    flur = ar.async_get(hass).async_create("Flur")
    for domain in ("light", "lock"):
        e = entities.async_get_or_create(domain, "test", f"flur_{domain}", suggested_object_id="flur")
        entities.async_update_entity(e.entity_id, area_id=flur.id)
    return {"garage": garage.id, "door": door.id, "flur": flur.id}


async def test_what_a_task_may_call(hass):
    ids = await house(hass)
    domains, entities = set(FORBIDDEN["domains"]), set(FORBIDDEN["entities"])
    allowed = [
        {"variables": {"x": 1}},
        {"action": "mira.decide", "data": {"question": "q"}, "response_variable": "d"},
        {"condition": "template", "value_template": "{{ d.answer }}"},
        {"action": "media_player.turn_on", "target": {"entity_id": "media_player.radio_bad"}},
        {"action": "notify.mobile_app_phone", "data": {"message": "Wasser läuft"}},
        {"action": "light.turn_on", "target": {"area_id": "bad"}},
        # The hall has a lock, but light.turn_on switches lights only.
        {"action": "light.turn_on", "target": {"area_id": ids["flur"]}},
        {"action": "cover.close_cover", "target": {"entity_id": "cover.rollo_wohnzimmer"}},
        {"if": [], "then": [{"action": "mira.assess", "data": {"goal": "g"}}], "else": [{"stop": "done"}]},
        # A script exposed to Assist, by its name or through script.turn_on.
        {"action": "script.kaffee"},
        {"action": "script.turn_on", "target": {"entity_id": "script.kaffee"}},
        {"scene": "scene.abend"},
        {"action": "scene.turn_on", "target": {"entity_id": "scene.abend"}},
    ]
    await check_actions(hass, allowed, domains, entities)
    for forbidden in (
        [{"action": "lock.unlock", "target": {"entity_id": "lock.haustuer"}}],
        [{"service": "alarm_control_panel.alarm_disarm"}],
        [{"action": "cover.open_cover", "target": {"entity_id": "cover.garage"}}],
        [{"action": "cover.open_cover", "target": {"area_id": "garage"}}],
        # A script not exposed to Assist.
        [{"action": "script.turn_on", "target": {"entity_id": "script.geheim"}}],
        [{"action": "script.geheim"}],
        [{"action": "shell_command.wipe"}],
        [{"action": "homeassistant.restart"}],
        [{"choose": [{"sequence": [{"action": "lock.unlock"}]}]}],
        [{"device_id": "abc", "domain": "lock", "type": "unlock"}],
    ):
        with pytest.raises(NotAllowed):
            await check_actions(hass, forbidden, domains, entities)


async def test_the_check_cannot_be_walked_around(hass):
    """A template, a nesting or a step the check does not know must not hide
    what it keeps out; and what is fine stays fine inside any nesting."""
    ids = await house(hass)
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
        [{"if": [], "then": [{"repeat": {"while": [], "sequence": [{"action": "script.geheim"}]}}]}],
        [{"choose": ["not a dict"]}],
        "not a list of steps",
        # A scene sets the lock, applied or stored.
        [{"action": "scene.apply", "data": {"entities": {"lock.haustuer": "unlocked"}}}],
        [{"action": "scene.create", "data": {"scene_id": "x", "entities": {"cover.garage": "open"}}}],
        [{"action": "scene.turn_on", "target": {"entity_id": "scene.nacht"}}],
        [{"scene": "scene.nacht"}],
        # The target where Home Assistant takes it, not only under target.
        [{"action": "cover.open_cover", "entity_id": "cover.garage"}],
        [{"action": "cover.open_cover", "data_template": {"entity_id": "cover.garage"}}],
        [{"action": "cover.open_cover", "target": "{{ {'entity_id': 'cover.garage'} }}"}],
        [{"action": "cover.open_cover", "data": "{{ {'entity_id': 'cover.garage'} }}"}],
        # A group holding the garage door.
        [{"action": "cover.open_cover", "target": {"entity_id": "group.aussen"}}],
        # The garage door's device, by its entity's registry id, or the device itself.
        [{"device_id": ids["garage"], "domain": "cover", "entity_id": ids["door"], "type": "open"}],
        [{"action": "cover.open_cover", "target": {"device_id": ids["garage"]}}],
        # Actions that name no entities: anything could be behind them.
        [{"action": "mqtt.publish", "data": {"topic": "zigbee2mqtt/garage/set", "payload": "OPEN"}}],
        [{"action": "rest_command.garage_auf"}],
        [{"action": "conversation.process", "data": {"text": "Garage auf"}}],
        [{"action": "made_up.action"}],
        [{"device_id": ids["garage"], "domain": "zwave_js", "type": "set_value"}],
    ):
        with pytest.raises(NotAllowed):
            await check_actions(hass, sneaky, domains, {"switch.garage_tor", *entities})
    await check_actions(
        hass,
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
    assert await async_setup_component(hass, "light", {})
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

    from custom_components.mira.labels import LABEL_CONFIRM, LABEL_PRIVATE, LABEL_TASKS, ensure_labels

    hass.config.language = "de"
    ensure_labels(hass)
    labels = lr.async_get(hass)
    assert labels.async_get_label(LABEL_CONFIRM).name == "Mira: nachfragen"
    assert labels.async_get_label(LABEL_TASKS).description == "Aufgaben von Mira"
    labels.async_update(LABEL_CONFIRM, name="Vorsicht")
    ensure_labels(hass)
    assert labels.async_get_label(LABEL_CONFIRM).name == "Vorsicht"
    assert labels.async_get_label(LABEL_PRIVATE).name == "Mira: privater Bereich"
    assert len(labels.labels) == 4  # Mira, Mira: nachfragen, Mira: einmalig, Mira: privater Bereich


async def test_a_task_may_not_switch_what_is_labelled_confirm(hass):
    from homeassistant.helpers import entity_registry as er

    from custom_components.mira.labels import LABEL_CONFIRM, ensure_labels

    assert await async_setup_component(hass, "automation", {})
    assert await async_setup_component(hass, "switch", {})
    ensure_labels(hass)
    registry = er.async_get(hass)
    pump = registry.async_get_or_create("switch", "test", "pump", suggested_object_id="pumpe")
    registry.async_update_entity(pump.entity_id, labels={LABEL_CONFIRM})
    task = TASK | {"actions": [{"action": "switch.turn_on", "target": {"entity_id": pump.entity_id}}]}
    with pytest.raises(NotAllowed):
        await validate(hass, task)


# --- Changing the household's tasks (docs/spec/aufgaben-aendern.md) ---------------------------------------

HOUSEHOLD = {
    "id": "abendlicht",
    "alias": "Abendlicht Wohnzimmer",
    # Triggers without timers: the test framework stops at lingering ones.
    "triggers": [{"trigger": "state", "entity_id": "sun.sun", "to": "below_horizon"}],
    "actions": [
        {"action": "light.turn_on", "target": {"entity_id": "light.wohnzimmer"}},
        {"action": "lock.unlock", "target": {"entity_id": "lock.haustuer"}},
    ],
}


async def editor_house(hass, tmp_path):
    """Home Assistant whose configuration includes automations.yaml, with one
    household task the editor saved, one of Mira's, and one from YAML."""
    from homeassistant.helpers import entity_registry as er
    from homeassistant.util.yaml import dump

    from custom_components.mira.automations import save
    from custom_components.mira.labels import ensure_labels

    hass.config.config_dir = str(tmp_path)
    (tmp_path / "configuration.yaml").write_text(
        "automation editor: !include automations.yaml\n"
        "automation yaml:\n"
        "  - alias: Aus YAML\n"
        "    triggers: [{trigger: state, entity_id: sun.sun, to: above_horizon}]\n"
        "    actions: [{action: light.turn_off, target: {entity_id: light.wohnzimmer}}]\n"
    )
    (tmp_path / "automations.yaml").write_text(dump([HOUSEHOLD]))
    for domain in ("light", "lock"):
        assert await async_setup_component(hass, domain, {})
    ensure_labels(hass)
    from homeassistant import config as conf_util

    assert await async_setup_component(hass, "automation", await conf_util.async_hass_config_yaml(hass))
    await hass.async_block_till_done()
    entities = er.async_get(hass)
    household = entities.async_get_entity_id("automation", "automation", "abendlicht")
    entities.async_update_entity(household, labels={"licht"})
    mira = await save(hass, TASK | {"triggers": HOUSEHOLD["triggers"]}, None, FORBIDDEN)
    return household, mira["entity_id"]


async def test_AE01_mira_reads_a_task_the_editor_saved(hass, tmp_path):
    from custom_components.mira.automations import get_task

    household, _ = await editor_house(hass, tmp_path)
    got = await get_task(hass, household)
    assert got["id"] == "abendlicht" and got["config"]["triggers"] == HOUSEHOLD["triggers"]
    assert got["config"]["actions"] == HOUSEHOLD["actions"]


async def test_AE02_a_task_from_yaml_is_not_editable(hass, tmp_path):
    from custom_components.mira.automations import NotEditable, get_task

    await editor_house(hass, tmp_path)
    with pytest.raises(NotEditable):
        await get_task(hass, "automation.aus_yaml")


async def test_AE03_saving_a_household_task_keeps_its_labels(hass, tmp_path):
    from homeassistant.helpers import entity_registry as er

    from custom_components.mira.automations import save
    from custom_components.mira.labels import LABEL_TASKS

    household, _ = await editor_house(hass, tmp_path)
    changed = HOUSEHOLD | {"triggers": [HOUSEHOLD["triggers"][0] | {"for": "00:30:00"}]}
    # The unlock was there before and stays unchanged: the admin's decision.
    await save(hass, changed, "abendlicht", FORBIDDEN, None)
    entry = er.async_get(hass).async_get(household)
    assert entry.labels == {"licht"} and LABEL_TASKS not in entry.labels
    assert hass.states.get(household) is not None
    # A new or changed action is checked as when creating.
    new_lock = changed | {"actions": [HOUSEHOLD["actions"][0], {"action": "lock.unlock", "target": {"entity_id": "lock.keller"}}]}
    with pytest.raises(NotAllowed):
        await save(hass, new_lock, "abendlicht", FORBIDDEN, None)


async def test_AE04_the_list_with_all_names_every_task(hass, tmp_path, aioclient_mock, entry, hass_ws_client):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    household, mira = await editor_house(hass, tmp_path)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    client = await hass_ws_client(hass)

    await client.send_json_auto_id({"type": "mira/automation/list", "all": True})
    found = {a["entity_id"]: a for a in (await client.receive_json())["result"]["automations"]}
    assert found[household]["editable"] and found[household]["name"] == "Abendlicht Wohnzimmer"
    assert found[household]["labels"] == ["licht"]
    assert found[mira]["editable"] and not found["automation.aus_yaml"]["editable"]

    await client.send_json_auto_id({"type": "mira/automation/list"})
    assert [a["entity_id"] for a in (await client.receive_json())["result"]["automations"]] == [mira]

    await client.send_json_auto_id({"type": "mira/automation/get", "entity_id": "automation.aus_yaml"})
    msg = await client.receive_json()
    assert not msg["success"] and msg["error"]["code"] == "not_editable"


async def test_AE05_checking_a_change_knows_the_task_before(hass, tmp_path, aioclient_mock, entry, hass_ws_client):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    await editor_house(hass, tmp_path)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    client = await hass_ws_client(hass)
    changed = HOUSEHOLD | {"triggers": [HOUSEHOLD["triggers"][0] | {"for": "00:30:00"}]}

    # The unlock was there before: the change is fine.
    await client.send_json_auto_id(
        {"type": "mira/automation/validate", "config": changed, "forbidden": FORBIDDEN, "automation_id": "abendlicht"}
    )
    assert (await client.receive_json())["result"] == {"valid": True}
    # Without the task it is checked as new, and the unlock is refused.
    await client.send_json_auto_id({"type": "mira/automation/validate", "config": changed, "forbidden": FORBIDDEN})
    assert (await client.receive_json())["result"]["valid"] is False
