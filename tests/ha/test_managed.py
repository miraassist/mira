"""Discovery, the compact button, and watchers/persons/rooms as subentries."""

from ipaddress import ip_address

import pytest

from homeassistant import config_entries
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.data_entry_flow import FlowResultType
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.const import CONF_IMPORTED, DOMAIN

from .conftest import AGENT_ID, AGENT_URL

STATUS = {"pending_notices": 0, "quiet_mode": False}

# What the agent's GET /v1/ui/ha returns: the shape of mira.yaml sections.
AGENT = {
    "managed": False,
    "watchers": [
        {
            "name": "wasserleck",
            "trigger": {"entity_id": ["binary_sensor.leck"], "to": ["on"], "from": None, "not_to": None},
            "schedule": "",
            "kind": "",
            "url": "",
            "dedupe_key": "",
            "description": "Ein Lecksensor meldet Wasser.",
            "importance": 3,
            "audience": "",
            "confidentiality": "",
            "expires_in": "0s",
        },
        {
            "name": "modelle",
            "trigger": None,
            "schedule": "0 8 * * *",
            "kind": "rss",
            "url": "https://example.org/feed.xml",
            "dedupe_key": "",
            "description": "Neue Modelle.",
            "importance": None,
            "audience": "anna",
            "confidentiality": "",
            "expires_in": "12h",
        },
    ],
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
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID})
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.get(f"{AGENT_URL}/v1/ui/ha", json=AGENT)
    e = MockConfigEntry(domain=DOMAIN, title="Mira", data={CONF_URL: AGENT_URL, CONF_TOKEN: "secret"})
    e.add_to_hass(hass)
    return e


def _puts(aioclient_mock):
    return [c[2] for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/ha")]


async def test_first_setup_imports_and_takes_over(hass, aioclient_mock, fresh_entry):
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    assert await hass.config_entries.async_setup(fresh_entry.entry_id)
    await hass.async_block_till_done()

    assert fresh_entry.unique_id == AGENT_ID
    assert fresh_entry.data[CONF_IMPORTED] is True
    by_type = {s.unique_id: s for s in fresh_entry.subentries.values()}
    assert set(by_type) == {"watcher:wasserleck", "watcher:modelle", "person:anna", "room:bad"}
    assert by_type["watcher:wasserleck"].subentry_type == "watcher"
    assert by_type["watcher:modelle"].subentry_type == "feed"

    # Everything goes back unchanged, extras included.
    sent = _puts(aioclient_mock)[-1]
    watchers = {w["name"]: w for w in sent["watchers"]}
    assert watchers["wasserleck"] == {
        "name": "wasserleck",
        "description": "Ein Lecksensor meldet Wasser.",
        "trigger": {"entity_id": ["binary_sensor.leck"], "to": ["on"]},
        "importance": 3,
    }
    assert watchers["modelle"] == {
        "name": "modelle",
        "description": "Neue Modelle.",
        "kind": "rss",
        "url": "https://example.org/feed.xml",
        "schedule": "0 8 * * *",
        "audience": "anna",
        "expires_in": "12h",
    }
    assert sent["persons"] == {"anna": {"entity": "person.anna", "telegram_user_id": 42}}
    assert sent["rooms"] == [AGENT["rooms"][0] | {"mode": "announce", "indicator": ""}]


async def test_add_watcher_is_checked_by_the_agent(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    result = await hass.config_entries.subentries.async_init(
        (entry.entry_id, "watcher"), context={"source": config_entries.SOURCE_USER}
    )
    assert result["type"] is FlowResultType.FORM
    result = await hass.config_entries.subentries.async_configure(
        result["flow_id"],
        {
            "name": "fenster",
            "entity_id": ["binary_sensor.fenster"],
            "to": ["on"],
            "importance": "auto",
            "audience": "household",
            "confidentiality": "normal",
        },
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    await hass.async_block_till_done()
    # [0] is the sync after setup, [1] the check of the form.
    assert _puts(aioclient_mock)[1]["watchers"] == [
        {"name": "fenster", "description": "", "trigger": {"entity_id": ["binary_sensor.fenster"], "to": ["on"]}}
    ]
    assert [s.title for s in entry.subentries.values()] == ["fenster"]


async def test_agent_refusal_shows_in_the_form(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", status=422, json={"error": "satellites: area keller unknown"})
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


async def test_person_needs_a_numeric_telegram_id(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    result = await hass.config_entries.subentries.async_init(
        (entry.entry_id, "person"), context={"source": config_entries.SOURCE_USER}
    )
    result = await hass.config_entries.subentries.async_configure(
        result["flow_id"], {"entity": "person.ben", "telegram_user_id": "@ben"}
    )
    assert result["errors"] == {"base": "invalid_config"}
    result = await hass.config_entries.subentries.async_configure(
        result["flow_id"], {"entity": "person.ben", "telegram_user_id": "77"}
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["unique_id"] == "person:ben"


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
