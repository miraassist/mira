"""Discovery, the compact button, rooms as subentries and person settings
on Home Assistant's persons."""

from ipaddress import ip_address

from unittest.mock import AsyncMock, patch

import pytest

from homeassistant import config_entries
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers import area_registry as ar, entity_registry as er
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.config_flow import FIELD_NAMES
from custom_components.mira.const import CONF_AGENT_USER, CONF_IMPORTED, DOMAIN
from custom_components.mira.managed import notify_targets

from .conftest import AGENT_ID, AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}

# What the agent's GET /v1/ui/ha returns: the shape of mira.yaml sections.
AGENT = {
    "managed": False,
    "persons": {"anna": {"entity": "person.anna", "channels": ["notify.anna_phone"]}},
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
    assert registry.async_get("person.anna").options[DOMAIN] == {"channels": ["notify.anna_phone"]}
    assert registry.async_get(satellite).options[DOMAIN] == {"presence": ["binary_sensor.bad"], "morning": True}

    # Everything goes back unchanged.
    sent = _puts(aioclient_mock)[-1]
    assert sent["persons"] == {"anna": {"entity": "person.anna", "channels": ["notify.anna_phone"]}}
    assert sent["rooms"] == [
        {"area": "bad", "satellite": satellite, "presence": ["binary_sensor.bad"], "morning": True,
         "indicator": ""}
    ]


def _fields(data):
    """Form fields under their readable (English) names."""
    return {FIELD_NAMES["en"][k]: v for k, v in data.items()}


async def _form(hass, entry, step):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    return await hass.config_entries.options.async_configure(result["flow_id"], {"next_step_id": step})


async def test_general_lives_at_the_agent(hass, aioclient_mock, entry):
    """The general options show the agent's persona and web search switch
    and send a change back; the entry keeps neither. Unchanged, nothing is
    sent."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.get(
        f"{AGENT_URL}/v1/ui/config",
        json={
            "persona": {"value": "Trocken."},
            "web_search": {"value": {"enabled": True}},
            "notifier": {"value": {"cooldown": "30m", "own_message_hold": "10m"}},
        },
    )
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/config", json={})
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await _form(hass, entry, "general")
    keys = {str(k): k for k in result["data_schema"].schema}
    assert keys["persona"].description == {"suggested_value": "Trocken."}
    assert keys["web_search"].default() is True
    assert keys["own_message_hold"].default() == 10
    same = {"timeout": 20, "sidebar": True, "web_search": True, "persona": "Trocken.", "own_message_hold": 10}
    await hass.config_entries.options.async_configure(result["flow_id"], same)
    assert not [c for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/config")]
    await hass.async_block_till_done()  # the options update reloads the entry
    result = await _form(hass, entry, "general")
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {**same, "web_search": False, "persona": "  Frech.  ", "own_message_hold": 0}
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    puts = [c[2] for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/config")]
    # The notifier section goes back whole, only the hold changed.
    assert puts == [{
        "persona": "Frech.", "web_search": {"enabled": False},
        "notifier": {"cooldown": "30m", "own_message_hold": "0m"},
    }]
    assert "persona" not in entry.options and "own_message_hold" not in entry.options

async def test_satellite_settings_live_on_the_satellite(hass, aioclient_mock, entry):
    """A room is a satellite's area; several motion sensors may say someone
    came in. Every Assist satellite with an area speaks there anyway. One
    form, a section per satellite."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    wz, _ = _satellite(hass, "wohnzimmer", "Wohnzimmer")
    kueche, _ = _satellite(hass, "kueche", "Küche")
    hass.states.async_set(wz, "idle", {"friendly_name": "Wohnzimmer-Echo"})
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await _form(hass, entry, "satellites")
    assert result["step_id"] == "satellites"
    sections = [str(k) for k in result["data_schema"].schema]
    assert "Wohnzimmer-Echo" in sections and kueche in sections
    result = await hass.config_entries.options.async_configure(
        result["flow_id"],
        {
            "Wohnzimmer-Echo": _fields({"presence": ["binary_sensor.flur", "binary_sensor.wz"], "morning": True,
                                        "indicator": "light.ring"}),
            kueche: _fields({"morning": False}),
        },
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert er.async_get(hass).async_get(wz).options[DOMAIN]["presence"] == ["binary_sensor.flur", "binary_sensor.wz"]
    rooms = {r["area"]: r for r in _puts(aioclient_mock)[-1]["rooms"]}
    assert rooms["wohnzimmer"]["presence"] == ["binary_sensor.flur", "binary_sensor.wz"]
    assert rooms["wohnzimmer"]["indicator"] == "light.ring"
    assert {r["satellite"] for r in rooms.values()} == {wz, kueche}


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
    result = await _form(hass, e, "satellites")
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {keller: _fields({"morning": True, "indicator": "light.x"})}
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


async def test_person_settings_live_on_the_person(hass, aioclient_mock, entry):
    """Mira's settings for a HA person are entity registry options, keyed
    for the agent by the person's own id, which stays on a rename. One form,
    a section per person."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    entity = _person(hass, "ben_1", "Ben")
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await _form(hass, entry, "persons")
    assert [str(k) for k in result["data_schema"].schema] == ["Ben"]
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"Ben": _fields({"channels": ["telegram ben"]})})
    assert result["errors"] == {"base": "invalid_config"}
    result = await hass.config_entries.options.async_configure(
        result["flow_id"], {"Ben": _fields({"channels": ["notify.telegram_ben"], "accept_from": "persons", "alone_is_me": True})}
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert er.async_get(hass).async_get(entity).options[DOMAIN] == {"channels": ["notify.telegram_ben"], "accept_from": "persons", "alone_is_me": True}
    assert _puts(aioclient_mock)[-1]["persons"] == {
        "ben_1": {"entity": entity, "channels": ["notify.telegram_ben"], "accept_from": "persons", "alone_is_me": True}
    }
    # Mira's own notify entities take messages for her; they are no channel.
    own = next(e.entity_id for e in er.async_get(hass).entities.values() if e.platform == DOMAIN and e.domain == "notify")
    assert own not in notify_targets(hass)
    result = await _form(hass, entry, "persons")
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"Ben": _fields({"channels": [own]})})
    assert result["errors"] == {"base": "invalid_config"}
    # Emptied, the settings go away again.
    result = await _form(hass, entry, "persons")
    await hass.config_entries.options.async_configure(result["flow_id"], {"Ben": _fields({"alone_is_me": False})})
    assert DOMAIN not in er.async_get(hass).async_get(entity).options
    assert _puts(aioclient_mock)[-1]["persons"] == {}


async def test_panel_edits_the_same_settings(hass, aioclient_mock, entry, hass_client, hass_admin_user):
    """The panel's forms read and save through the integration, which checks
    with the agent and stores on the person or satellite."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    anna = _person(hass, "anna", "Anna")
    wz, _ = _satellite(hass, "wohnzimmer", "Wohnzimmer")
    assert await hass.config_entries.async_setup(entry.entry_id)
    client = await hass_client()
    body = await (await client.get("/api/mira/settings/persons")).json()
    assert [i["name"] for i in body["items"]] == ["Anna"]
    resp = await client.put("/api/mira/settings/persons", json={anna: {"channels": "notify.anna_phone, notify.telegram_anna"}})
    assert resp.status == 200
    assert er.async_get(hass).async_get(anna).options[DOMAIN] == {"channels": ["notify.anna_phone", "notify.telegram_anna"]}
    body = await (await client.get("/api/mira/settings/satellites")).json()
    assert body["items"][0]["entity"] == wz and body["items"][0]["area"] == "Wohnzimmer"
    resp = await client.put(
        "/api/mira/settings/satellites", json={wz: {"presence": "binary_sensor.a, binary_sensor.b", "morning": True}}
    )
    assert resp.status == 200
    assert er.async_get(hass).async_get(wz).options[DOMAIN]["presence"] == ["binary_sensor.a", "binary_sensor.b"]
    resp = await client.put("/api/mira/settings/persons", json={anna: {"channels": ["x"]}})
    assert resp.status == 422
    assert resp.status == 422 and "not a notify target" in (await resp.json())["error"]
    resp = await client.put("/api/mira/settings/persons", json={"person.nobody": {}})
    assert resp.status == 422
    hass_admin_user.groups = []
    assert (await client.get("/api/mira/settings/persons")).status == 401


async def test_discovery_asks_only_to_add(hass, aioclient_mock, hass_owner_user):
    """A discovered agent that was never paired (here with its model kept):
    one click, a summary, "Pair"; nothing is sent before that."""
    aioclient_mock.get(
        "http://192.168.1.20:8787/v1/pair", json={"id": AGENT_ID, "pairs": True, "open": True, "model": "qwen3"}
    )
    aioclient_mock.post("http://192.168.1.20:8787/v1/pair", json={"token": "secret"})
    aioclient_mock.get("http://192.168.1.20:8787/v1/health", json={"status": "ok", "id": AGENT_ID})
    result = await hass.config_entries.flow.async_init(
        DOMAIN, context={"source": config_entries.SOURCE_ZEROCONF}, data=_discovery()
    )
    assert result["type"] is FlowResultType.FORM
    assert result["step_id"] == "confirm"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {})
    assert result["step_id"] == "summary"
    assert not any(c[0] == "POST" for c in aioclient_mock.mock_calls)
    with patch("custom_components.mira.config_flow.ensure_mcp_server", AsyncMock()):
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {"next_step_id": "finish"})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["data"][CONF_URL] == "http://192.168.1.20:8787" and result["data"][CONF_TOKEN] == "secret"
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
    aioclient_mock.post(f"{AGENT_URL}/v1/unpair", json={"status": "restarting"})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.config_entries.async_remove(entry.entry_id)
    assert any(c[0] == "DELETE" and str(c[1]).endswith("/v1/ui/ha") for c in aioclient_mock.mock_calls)
    # The agent lets go, so it pairs again without a code.
    assert any(c[0] == "POST" and str(c[1]).endswith("/v1/unpair") for c in aioclient_mock.mock_calls)


async def test_compact_button(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.post(f"{AGENT_URL}/v1/ui/memory/compact", json={"changed": True})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    await hass.services.async_call("button", "press", {"entity_id": "button.mira_compact_profile"}, blocking=True)
    assert str(aioclient_mock.mock_calls[-1][1]).endswith("/v1/ui/memory/compact")


async def test_exposed_entities_reach_the_agent(hass, aioclient_mock, entry):
    """The agent's user may not read what Assist sees; the integration
    sends it along, and again when it changes."""
    from homeassistant.components.homeassistant.exposed_entities import async_expose_entity

    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    hass.states.async_set("calendar.familie", "off", {"friendly_name": "Familie"})
    hass.states.async_set("calendar.geheim", "off", {"friendly_name": "Geheim"})
    async_expose_entity(hass, "conversation", "calendar.familie", True)
    async_expose_entity(hass, "conversation", "calendar.geheim", False)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    exposed = _puts(aioclient_mock)[-1]["exposed"]
    assert "calendar.familie" in exposed and "calendar.geheim" not in exposed
    async_expose_entity(hass, "conversation", "calendar.geheim", True)
    await hass.async_block_till_done()
    assert "calendar.geheim" in _puts(aioclient_mock)[-1]["exposed"]
