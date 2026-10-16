"""Discovery, the compact button, rooms as subentries and person settings
on Home Assistant's persons."""

from ipaddress import ip_address

import pytest

from homeassistant import config_entries
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers import area_registry as ar, entity_registry as er
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.const import CONF_AGENT_USER, CONF_IMPORTED, DOMAIN

from .conftest import AGENT_ID, AGENT_URL

STATUS = {"pending_notices": 0, "quiet_mode": False}

# What the agent's GET /v1/ui/ha returns: the shape of mira.yaml sections.
AGENT = {
    "managed": False,
    "persons": {"anna": {"entity": "person.anna", "telegram_user_id": 42, "notify": ""}},
    "rooms": [{"area": "bad", "satellite": "assist_satellite.bad", "presence": "binary_sensor.bad", "morning": True}],
}


def _discovery(agent_id=AGENT_ID, host="192.168.1.20"):
    return ZeroconfServiceInfo(
        ip_address=ip_address(host),
        ip_addresses=[ip_address(host)],
        hostname="mira.local.",
        name="Mira._mira._tcp.local.",
        port=8787,
        type="_mira._tcp.local.",
        properties={"id": agent_id, "version": "dev"} if agent_id else {},
    )


@pytest.fixture
def fresh_entry(hass, aioclient_mock):
    """An entry set up for the first time: nothing imported yet."""
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID, "ha_user_id": "mira-user"})
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.get(f"{AGENT_URL}/v1/ui/ha", json=AGENT)
    e = MockConfigEntry(domain=DOMAIN, title="Mira", data={CONF_URL: AGENT_URL, CONF_TOKEN: "secret"})
    e.add_to_hass(hass)
    return e


def _puts(aioclient_mock):
    return [c[2] for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/ha")]


def _person(hass, key, name):
    """A HA person in the entity registry, with a state like HA's."""
    entry = er.async_get(hass).async_get_or_create("person", "person", key, suggested_object_id=key)
    hass.states.async_set(entry.entity_id, "home", {"id": key, "friendly_name": name})
    return entry.entity_id


def _satellite(hass, object_id, area_name):
    """An Assist satellite in an area, as HA registers it."""
    area = ar.async_get(hass).async_get_or_create(area_name)
    registry = er.async_get(hass)
    entry = registry.async_get_or_create("assist_satellite", "test", object_id, suggested_object_id=object_id)
    registry.async_update_entity(entry.entity_id, area_id=area.id)
    return entry.entity_id, area.id


async def test_first_setup_imports_and_takes_over(hass, aioclient_mock, fresh_entry):
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    _person(hass, "anna", "Anna")
    satellite, area = _satellite(hass, "bad", "Bad")
    assert area == "bad"
    assert await hass.config_entries.async_setup(fresh_entry.entry_id)
    await hass.async_block_till_done()

    assert fresh_entry.unique_id == AGENT_ID
    assert fresh_entry.data[CONF_IMPORTED] is True
    # The agent's own user may manage Mira's automations.
    assert fresh_entry.data[CONF_AGENT_USER] == "mira-user"
    assert not fresh_entry.subentries
    # The settings went onto the person and the satellite themselves.
    registry = er.async_get(hass)
    assert registry.async_get("person.anna").options[DOMAIN] == {"telegram_user_id": "42"}
    assert registry.async_get(satellite).options[DOMAIN] == {"presence": ["binary_sensor.bad"], "morning": True}

    # Everything goes back unchanged.
    sent = _puts(aioclient_mock)[-1]
    assert sent["persons"] == {"anna": {"entity": "person.anna", "telegram_user_id": 42}}
    assert sent["rooms"] == [
        {"area": "bad", "satellite": satellite, "presence": ["binary_sensor.bad"], "morning": True,
         "mode": "announce", "indicator": ""}
    ]


async def _satellite_form(hass, entry, entity):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"next_step_id": "satellite_pick"})
    return await hass.config_entries.options.async_configure(result["flow_id"], {"entity": entity})


async def test_satellite_settings_live_on_the_satellite(hass, aioclient_mock, entry):
    """A room is a satellite's area; several motion sensors may say someone
    came in. Every Assist satellite with an area speaks there anyway."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    wz, _ = _satellite(hass, "wohnzimmer", "Wohnzimmer")
    kueche, _ = _satellite(hass, "kueche", "Küche")
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await _satellite_form(hass, entry, wz)
    assert result["step_id"] == "satellite"
    result = await hass.config_entries.options.async_configure(
        result["flow_id"],
        {"presence": ["binary_sensor.flur", "binary_sensor.wz"], "morning": True, "mode": "indicate",
         "indicator": "light.ring"},
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert er.async_get(hass).async_get(wz).options[DOMAIN]["presence"] == ["binary_sensor.flur", "binary_sensor.wz"]
    rooms = {r["area"]: r for r in _puts(aioclient_mock)[-1]["rooms"]}
    assert rooms["wohnzimmer"]["presence"] == ["binary_sensor.flur", "binary_sensor.wz"]
    assert rooms["wohnzimmer"]["mode"] == "indicate"
    # The kitchen satellite has no settings and still speaks for its area.
    assert {r["satellite"] for r in rooms.values()} == {wz, kueche}


async def test_satellite_without_area(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    registry = er.async_get(hass)
    lost = registry.async_get_or_create("assist_satellite", "test", "lost", suggested_object_id="lost").entity_id
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await _satellite_form(hass, entry, lost)
    assert result["errors"] == {"base": "no_area"}


async def test_agent_refusal_shows_in_the_form(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", status=422, json={"error": "notifier: indicator light.x unknown"})
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID})
    e = MockConfigEntry(
        domain=DOMAIN, unique_id=AGENT_ID, data={CONF_URL: AGENT_URL, CONF_TOKEN: "s", CONF_IMPORTED: True}
    )
    e.add_to_hass(hass)
    keller, _ = _satellite(hass, "keller", "Keller")
    assert await hass.config_entries.async_setup(e.entry_id)
    result = await _satellite_form(hass, e, keller)
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {"morning": True, "mode": "indicate", "indicator": "light.x"}
    )
    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "invalid_config"}
    assert result["description_placeholders"]["error"] == "notifier: indicator light.x unknown"
    assert DOMAIN not in er.async_get(hass).async_get(keller).options


async def test_old_subentries_are_removed(hass, aioclient_mock, entry):
    from homeassistant.config_entries import ConfigSubentry

    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    for kind in ("person", "room"):
        hass.config_entries.async_add_subentry(
            entry, ConfigSubentry(data={"key": "x"}, subentry_type=kind, title="X", unique_id=f"{kind}:x")
        )
    assert await hass.config_entries.async_setup(entry.entry_id)
    assert not entry.subentries


async def _person_form(hass, entry, entity):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"next_step_id": "person_pick"})
    return await hass.config_entries.options.async_configure(result["flow_id"], {"entity": entity})


async def test_person_settings_live_on_the_person(hass, aioclient_mock, entry):
    """Mira's settings for a HA person are entity registry options, keyed
    for the agent by the person's own id, which stays on a rename."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    entity = _person(hass, "ben_1", "Ben")
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await _person_form(hass, entry, entity)
    assert result["step_id"] == "person"
    assert result["description_placeholders"]["person"] == "Ben"
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {"telegram_user_id": "@ben"}
    )
    assert result["errors"] == {"base": "invalid_config"}
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {"telegram_user_id": "77", "alone_is_me": True}
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert er.async_get(hass).async_get(entity).options[DOMAIN] == {"telegram_user_id": "77", "alone_is_me": True}
    assert _puts(aioclient_mock)[-1]["persons"] == {
        "ben_1": {"entity": entity, "telegram_user_id": 77, "alone_is_me": True}
    }
    # Emptied, the settings go away again.
    result = await _person_form(hass, entry, entity)
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"alone_is_me": False})
    assert DOMAIN not in er.async_get(hass).async_get(entity).options
    assert _puts(aioclient_mock)[-1]["persons"] == {}


async def test_discovery_asks_only_for_the_token(hass, aioclient_mock):
    aioclient_mock.get("http://192.168.1.20:8787/v1/health", json={"status": "ok", "id": AGENT_ID})
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_discovery()
    )
    assert result["type"] is FlowResultType.FORM
    assert result["step_id"] == "zeroconf_confirm"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_TOKEN: "secret"})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["data"] == {CONF_URL: "http://192.168.1.20:8787", CONF_TOKEN: "secret"}
    assert result["result"].unique_id == AGENT_ID


async def test_discovery_of_a_known_agent_updates_its_address(hass, entry):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_discovery(host="10.0.0.5")
    )
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"
    assert entry.data[CONF_URL] == "http://10.0.0.5:8787"


async def test_discovery_without_id(hass):
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_discovery(agent_id=None)
    )
    assert result["reason"] == "no_id"


async def test_removing_the_entry_hands_config_back(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.delete(f"{AGENT_URL}/v1/ui/ha", json={"managed": False})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.config_entries.async_remove(entry.entry_id)
    assert any(c[0] == "DELETE" and str(c[1]).endswith("/v1/ui/ha") for c in aioclient_mock.mock_calls)


async def test_compact_button(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.post(f"{AGENT_URL}/v1/ui/memory/compact", json={"changed": True})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    await hass.services.async_call("button", "press", {"entity_id": "button.mira_compact_profile"}, blocking=True)
    assert str(aioclient_mock.mock_calls[-1][1]).endswith("/v1/ui/memory/compact")
