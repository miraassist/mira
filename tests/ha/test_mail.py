"""The mails of IMAP entries named like a mail application go to the agent;
other entries and repeated last mails do not."""

from homeassistant.core import SupportsResponse
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.mail import _date, app_of

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
    pakete = MockConfigEntry(domain="imap", title="Pakete stefan")
    other = MockConfigEntry(domain="imap", title="me@example.com")
    pakete.add_to_hass(hass)
    other.add_to_hass(hass)

    async def fetch(call):
        return {"text": "Ihr Paket kommt morgen.", "sender": "a@dhl.example", "subject": "Paket",
                "date": "2026-10-20T08:00:00+02:00", "uid": call.data["uid"]}

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
