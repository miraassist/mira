"""Satellites and rooms (docs/spec/satellites-rooms.md): a satellite without
an area is a repair and listed on the page; several satellites in a room."""

from datetime import timedelta

from homeassistant.helpers import (
    area_registry as ar,
    device_registry as dr,
    entity_registry as er,
    issue_registry as ir,
)
from homeassistant.util import dt as dt_util
from pytest_homeassistant_custom_component.common import MockConfigEntry, async_fire_time_changed

from custom_components.mira import RESEND_COOLDOWN
from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}


def _satellite(hass, name, area_name=None, entity_area_name=None):
    """A satellite on a device of its own, as ESPHome registers one; the
    area on the device, on the entity, or none."""
    source = MockConfigEntry(domain="esphome")
    source.add_to_hass(hass)
    areas = ar.async_get(hass)
    devices = dr.async_get(hass)
    device = devices.async_get_or_create(config_entry_id=source.entry_id, identifiers={("esphome", name)}, name=name)
    if area_name:
        devices.async_update_device(device.id, area_id=areas.async_get_or_create(area_name).id)
    registry = er.async_get(hass)
    sat = registry.async_get_or_create(
        "assist_satellite",
        "esphome",
        name,
        device_id=device.id,
        suggested_object_id=name,
        original_name="Assist satellite",
    )
    if entity_area_name:
        registry.async_update_entity(sat.entity_id, area_id=areas.async_get_or_create(entity_area_name).id)
    hass.states.async_set(sat.entity_id, "idle", {"friendly_name": name.title()})
    return sat.entity_id, device.id


def _repairs(hass):
    return {i: v for (d, i), v in ir.async_get(hass).issues.items() if d == DOMAIN and i.startswith("no_area_")}


async def _settle(hass):
    """Registry changes are gathered for a moment, then handled once."""
    async_fire_time_changed(hass, dt_util.utcnow() + timedelta(seconds=RESEND_COOLDOWN + 1), fire_all=True)
    await hass.async_block_till_done(wait_background_tasks=True)


async def test_SR01_a_satellite_without_an_area_raises_a_repair(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    sat, device = _satellite(hass, "kueche")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    repairs = _repairs(hass)
    assert list(repairs) == [f"no_area_{sat}"]
    issue = repairs[f"no_area_{sat}"]
    assert issue.translation_key == "satellite_no_area"
    assert issue.translation_placeholders == {"name": "Kueche", "link": f"/config/devices/device/{device}"}


async def test_SR02_the_repair_goes_when_the_satellite_gets_an_area(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    sat, device = _satellite(hass, "kueche")
    other, _ = _satellite(hass, "flur")
    gone, _ = _satellite(hass, "keller")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert len(_repairs(hass)) == 3
    # an area on the device, a satellite switched off, one removed
    dr.async_get(hass).async_update_device(device, area_id=ar.async_get(hass).async_get_or_create("Küche").id)
    er.async_get(hass).async_update_entity(other, disabled_by=er.RegistryEntryDisabler.USER)
    er.async_get(hass).async_remove(gone)
    await _settle(hass)
    assert _repairs(hass) == {}


async def test_SR03_the_page_lists_a_satellite_without_an_area(
    hass, aioclient_mock, entry, hass_client, hass_admin_user
):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    placed, _ = _satellite(hass, "bad", "Bad")
    unplaced, _ = _satellite(hass, "kueche")
    assert await hass.config_entries.async_setup(entry.entry_id)
    client = await hass_client()
    body = await (await client.get("/api/mira/settings/satellites")).json()
    assert [i["entity"] for i in body["items"]] == [placed]
    assert body["unplaced"] == [{"entity": unplaced, "name": "Kueche"}]
    # one with settings from when it had an area is listed once, without an area
    er.async_get(hass).async_update_entity_options(unplaced, DOMAIN, {"morning": True})
    body = await (await client.get("/api/mira/settings/satellites")).json()
    assert [i["entity"] for i in body["items"]] == [placed] and len(body["unplaced"]) == 1


async def test_SR04_a_satellite_with_an_area_raises_nothing(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    _satellite(hass, "bad", "Bad")
    _satellite(hass, "flur", entity_area_name="Flur")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert _repairs(hass) == {}
    # and nothing is left behind when the integration goes
    sat, _ = _satellite(hass, "kueche")
    await _settle(hass)
    assert len(_repairs(hass)) == 1
    assert await hass.config_entries.async_unload(entry.entry_id)
    assert _repairs(hass) == {}


def _rooms(aioclient_mock):
    """The rooms the integration sent last."""
    puts = [c[2] for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/ha")]
    return {r["area"]: r for r in puts[-1]["rooms"]}


def _marked(hass, entity):
    """Whether the satellite is marked the primary of the area it is in."""
    from custom_components.mira.managed import satellite_area

    mark = (er.async_get(hass).async_get(entity).options.get(DOMAIN) or {}).get("primary")
    return bool(mark) and mark == satellite_area(hass, entity)


async def test_SR10_a_second_satellite_in_an_area_makes_the_first_the_primary(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    older, older_device = _satellite(hass, "keller", "Keller")
    first, _ = _satellite(hass, "sofa", "Wohnzimmer")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert not _marked(hass, first)
    # the second one gets an older entity id, so the order is not by name
    second, _ = _satellite(hass, "essen", "Wohnzimmer")
    await _settle(hass)
    await _settle(hass)
    assert _marked(hass, first) and not _marked(hass, second)
    room = _rooms(aioclient_mock)[ar.async_get(hass).async_get_area_by_name("Wohnzimmer").id]
    assert room["satellite"] == first
    assert [o["satellite"] for o in room["others"]] == [second]
    # one older in the registry, moved in from another room as that room's
    # primary, does not take it: its mark is for the other room
    er.async_get(hass).async_update_entity_options(older, DOMAIN, {"primary": "keller"})
    dr.async_get(hass).async_update_device(
        older_device, area_id=ar.async_get(hass).async_get_area_by_name("Wohnzimmer").id
    )
    await _settle(hass)
    await _settle(hass)
    assert _marked(hass, first) and not _marked(hass, older)


async def test_SR11_the_switch_makes_another_satellite_the_primary(hass, aioclient_mock, entry):
    from custom_components.mira.managed import save_settings

    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    first, _ = _satellite(hass, "sofa", "Wohnzimmer")
    second, _ = _satellite(hass, "essen", "Wohnzimmer")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert _marked(hass, first)
    await save_settings(hass, entry, satellites={second: {"primary": True}})
    assert _marked(hass, second) and not _marked(hass, first)
    room = _rooms(aioclient_mock)[ar.async_get(hass).async_get_area_by_name("Wohnzimmer").id]
    assert room["satellite"] == second and [o["satellite"] for o in room["others"]] == [first]
    # a form without the switch (the panel's of a single room) keeps the mark
    await save_settings(hass, entry, satellites={second: {"morning": True}})
    assert _marked(hass, second)


async def test_SR12_a_room_without_a_primary_gets_one_again(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    first, _ = _satellite(hass, "sofa", "Wohnzimmer")
    second, _ = _satellite(hass, "essen", "Wohnzimmer")
    third, _ = _satellite(hass, "fenster", "Wohnzimmer")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    assert _marked(hass, first)
    er.async_get(hass).async_update_entity(first, disabled_by=er.RegistryEntryDisabler.USER)
    await _settle(hass)
    await _settle(hass)
    assert _marked(hass, second) and not _marked(hass, third)
    room = _rooms(aioclient_mock)[ar.async_get(hass).async_get_area_by_name("Wohnzimmer").id]
    assert room["satellite"] == second and [o["satellite"] for o in room["others"]] == [third]


async def test_SR13_two_satellites_in_an_area_with_settings_are_not_refused(hass, aioclient_mock, entry):
    from custom_components.mira.managed import save_settings

    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    first, _ = _satellite(hass, "sofa", "Wohnzimmer")
    second, _ = _satellite(hass, "essen", "Wohnzimmer")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    await save_settings(
        hass,
        entry,
        satellites={
            first: {"presence": ["binary_sensor.sofa"], "morning": True},
            second: {"presence": ["binary_sensor.essen"], "indicator": "light.essen_ring"},
        },
    )
    room = _rooms(aioclient_mock)[ar.async_get(hass).async_get_area_by_name("Wohnzimmer").id]
    assert room["presence"] == ["binary_sensor.sofa"] and room["morning"]
    assert room["others"] == [
        {"satellite": second, "presence": ["binary_sensor.essen"], "morning": False, "indicator": "light.essen_ring"}
    ]


async def test_SR14_a_room_with_one_satellite_is_sent_as_before(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    sat, _ = _satellite(hass, "bad", "Bad")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    room = _rooms(aioclient_mock)[ar.async_get(hass).async_get_area_by_name("Bad").id]
    assert room == {"area": room["area"], "satellite": sat, "presence": [], "morning": False, "indicator": ""}
    assert not _marked(hass, sat)
