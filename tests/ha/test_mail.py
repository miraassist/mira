"""The mails of IMAP entries whose device is named like a mail application
go to the agent; other entries and repeated last mails do not. The entry's
own title does not count."""

from unittest.mock import AsyncMock, patch

from homeassistant.components.homeassistant.exposed_entities import async_should_expose
from homeassistant.core import SupportsResponse
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers import issue_registry as ir
from homeassistant.setup import async_setup_component
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.const import DOMAIN
from custom_components.mira.mail import _date, app_of, calendar_issue, show_missing_calendars

from .conftest import AGENT_ID, AGENT_URL, AGENT_USER


def test_the_entry_name_names_the_application():
    assert app_of("Pakete", ["pakete"]) == "pakete"
    assert app_of("pakete  stefan", ["pakete"]) == "pakete"
    assert app_of("me@example.com", ["pakete"]) == ""
    assert app_of("", ["pakete"]) == ""


def test_the_date_keeps_its_offset_or_is_left_out():
    assert _date("2026-10-20T08:00:00+02:00") == "2026-10-20T08:00:00+02:00"
    assert _date("2026-10-20T08:00:00") is None  # no offset: the agent takes now
    assert _date(None) is None
    assert _date("gestern") is None


def _mails(aioclient_mock):
    return [c for c in aioclient_mock.mock_calls if c[0] == "POST" and str(c[1]).endswith("/v1/mail")]


async def test_new_mails_of_a_mail_entry_reach_the_agent(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID, "ha_user_id": AGENT_USER})
    aioclient_mock.get(
        f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False, "mail_apps": ["pakete"]}
    )
    aioclient_mock.post(f"{AGENT_URL}/v1/mail", json={"action": "created"})
    # Renamed device: counts. Entry title "Pakete" with the device as set up:
    # does not.
    pakete = MockConfigEntry(domain="imap", title="me@example.com")
    other = MockConfigEntry(domain="imap", title="Pakete")
    devices = dr.async_get(hass)
    for imap in (pakete, other):
        imap.add_to_hass(hass)
        devices.async_get_or_create(
            config_entry_id=imap.entry_id, identifiers={("imap", imap.entry_id)}, name="IMAP (me@example.com)"
        )
    (device,) = dr.async_entries_for_config_entry(devices, pakete.entry_id)
    devices.async_update_device(device.id, name_by_user="Pakete  stefan")

    async def fetch(call):
        return {
            "text": "Ihr Paket kommt morgen.",
            "sender": "a@dhl.example",
            "subject": "Paket",
            "date": "2026-10-20T08:00:00+02:00",
            "uid": call.data["uid"],
        }

    hass.services.async_register("imap", "fetch", fetch, supports_response=SupportsResponse.ONLY)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    hass.bus.async_fire("imap_content", {"entry_id": pakete.entry_id, "uid": "7", "initial": True})
    hass.bus.async_fire("imap_content", {"entry_id": pakete.entry_id, "uid": "7", "initial": False})
    hass.bus.async_fire("imap_content", {"entry_id": other.entry_id, "uid": "8", "initial": True})
    await hass.async_block_till_done()

    sent = _mails(aioclient_mock)
    assert len(sent) == 1
    assert sent[0][2] == {
        "entry": "Pakete stefan",
        "sender": "a@dhl.example",
        "subject": "Paket",
        "date": "2026-10-20T08:00:00+02:00",
        "text": "Ihr Paket kommt morgen.",
    }


# --- A missing calendar is a repair (docs/spec/hacs-default.md, decision 3) ---


async def _polled(hass, aioclient_mock, entry, name="Pakete"):
    """Mira set up with the mail application "pakete", and an IMAP entry whose device is named for it."""
    imap = MockConfigEntry(domain="imap", title="me@example.com")
    imap.add_to_hass(hass)
    dr.async_get(hass).async_get_or_create(
        config_entry_id=imap.entry_id, identifiers={("imap", imap.entry_id)}, name=name
    )
    aioclient_mock.get(
        f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False, "mail_apps": ["pakete"]}
    )
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    return imap


async def _fix(hass, hass_client, name="Pakete"):
    assert await async_setup_component(hass, "repairs", {})
    client = await hass_client()
    resp = await client.post("/api/repairs/issues/fix", json={"handler": DOMAIN, "issue_id": calendar_issue(name)})
    flow = await resp.json()
    assert flow["step_id"] == "confirm" and flow["description_placeholders"] == {"name": name}, flow
    resp = await client.post(f"/api/repairs/issues/fix/{flow['flow_id']}", json={})
    return await resp.json()


async def test_HD10_a_missing_calendar_is_a_repair_nothing_is_created(hass, aioclient_mock, entry):
    await _polled(hass, aioclient_mock, entry)
    issue = ir.async_get(hass).async_get_issue(DOMAIN, "mail_calendar_pakete")
    assert issue is not None and issue.is_fixable and issue.translation_placeholders == {"name": "Pakete"}
    assert not hass.config_entries.async_entries("local_calendar")


async def test_HD11_confirming_the_repair_creates_the_calendar_and_exposes_it(hass, aioclient_mock, entry, hass_client):
    assert await async_setup_component(hass, "homeassistant", {})
    await _polled(hass, aioclient_mock, entry)
    made = MockConfigEntry(domain="local_calendar", title="Pakete")

    async def local_calendar(handler, context, data):
        # Home Assistant's own flow needs the ical library, which the tests do not have.
        assert handler == "local_calendar" and data == {"calendar_name": "Pakete"}
        made.add_to_hass(hass)
        er.async_get(hass).async_get_or_create("calendar", "local_calendar", "pakete", config_entry=made)
        return {"type": "create_entry", "result": made}

    with patch.object(hass.config_entries.flow, "async_init", local_calendar):
        result = await _fix(hass, hass_client)
    assert result["type"] == "create_entry", result
    await hass.async_block_till_done()
    (calendar,) = hass.config_entries.async_entries("local_calendar")
    assert calendar.title == "Pakete"
    (entity,) = er.async_entries_for_config_entry(er.async_get(hass), calendar.entry_id)
    assert async_should_expose(hass, "conversation", entity.entity_id)
    assert ir.async_get(hass).async_get_issue(DOMAIN, "mail_calendar_pakete") is None


async def test_HD12_a_refused_calendar_keeps_the_repair(hass, aioclient_mock, entry, hass_client, caplog):
    await _polled(hass, aioclient_mock, entry)
    refused = AsyncMock(return_value={"type": "abort", "reason": "already_configured"})
    with patch.object(hass.config_entries.flow, "async_init", refused):
        result = await _fix(hass, hass_client)
    assert result["step_id"] == "failed"
    assert result["description_placeholders"] == {"name": "Pakete", "reason": "already_configured"}
    assert "Could not create the calendar Pakete: already_configured" in caplog.text
    assert ir.async_get(hass).async_get_issue(DOMAIN, "mail_calendar_pakete") is not None


async def test_HD13_the_repair_goes_when_it_is_no_longer_needed(hass, aioclient_mock, entry):
    imap = await _polled(hass, aioclient_mock, entry)
    hass.states.async_set("calendar.pakete", "off", {"friendly_name": "Pakete"})
    show_missing_calendars(hass, ["pakete"])
    assert ir.async_get(hass).async_get_issue(DOMAIN, "mail_calendar_pakete") is None

    hass.states.async_remove("calendar.pakete")
    show_missing_calendars(hass, ["pakete"])
    assert ir.async_get(hass).async_get_issue(DOMAIN, "mail_calendar_pakete") is not None
    devices = dr.async_get(hass)
    (device,) = dr.async_entries_for_config_entry(devices, imap.entry_id)
    devices.async_update_device(device.id, name_by_user="IMAP privat")
    show_missing_calendars(hass, ["pakete"])
    assert ir.async_get(hass).async_get_issue(DOMAIN, "mail_calendar_pakete") is None
