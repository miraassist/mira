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


async def test_first_setup_imports_and_takes_over(hass, aioclient_mock, fresh_entry):
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    _person(hass, "anna", "Anna")
    assert await hass.config_entries.async_setup(fresh_entry.entry_id)
    await hass.async_block_till_done()

    assert fresh_entry.unique_id == AGENT_ID
    assert fresh_entry.data[CONF_IMPORTED] is True
    # The agent's own user may manage Mira's automations.
    assert fresh_entry.data[CONF_AGENT_USER] == "mira-user"
    by_type = {s.unique_id: s for s in fresh_entry.subentries.values()}
    assert set(by_type) == {"room:bad"}
    # The person's settings went onto the person itself.
    assert er.async_get(hass).async_get("person.anna").options[DOMAIN] == {"telegram_user_id": "42"}

    # Everything goes back unchanged.
    sent = _puts(aioclient_mock)[-1]
    assert "watchers" not in sent
    assert sent["persons"] == {"anna": {"entity": "person.anna", "telegram_user_id": 42}}
    assert sent["rooms"] == [AGENT["rooms"][0] | {"mode": "announce", "indicator": ""}]


async def test_agent_refusal_shows_in_the_form(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", status=422, json={"error": "satellites: area keller unknown"})
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID})
    e = MockConfigEntry(
        domain=DOMAIN, unique_id=AGENT_ID, data={CONF_URL: AGENT_URL, CONF_TOKEN: "s", CONF_IMPORTED: True}
    )
    e.add_to_hass(hass)
    assert await hass.config_entries.async_setup(e.entry_id)
    area = ar.async_get(hass).async_create("Keller")

    result = await hass.config_entries.subentries.async_init(
        (e.entry_id, "room"), context={"source": config_entries.SOURCE_USER}
    )
    result = await hass.config_entries.subentries.async_configure(
        result["flow_id"], {"area": area.id, "satellite": "assist_satellite.keller", "morning": True}
    )
    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "invalid_config"}
    assert result["description_placeholders"]["error"] == "satellites: area keller unknown"
    assert not e.subentries


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


async def test_old_person_subentries_are_removed(hass, aioclient_mock, entry):
    from homeassistant.config_entries import ConfigSubentry

    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    hass.config_entries.async_add_subentry(
        entry, ConfigSubentry(data={"key": "x"}, subentry_type="person", title="X", unique_id="person:x")
    )
    assert await hass.config_entries.async_setup(entry.entry_id)
    assert not entry.subentries


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
