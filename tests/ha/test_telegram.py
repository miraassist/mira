"""Telegram through Home Assistant's own integration: the adapter."""

from homeassistant.config_entries import ConfigSubentryData
from homeassistant.core import ServiceCall
from homeassistant.helpers import entity_registry as er
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}


async def _setup(hass, aioclient_mock, entry, chat_owner=True):
    """A Telegram bot entry with Anna's chat 4711, Anna's channels pointing to
    it, and Mira loaded."""
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    bot = MockConfigEntry(
        domain="telegram_bot",
        data={},
        subentries_data=[
            ConfigSubentryData(data={"chat_id": 4711}, subentry_type="allowed_chat_ids", title="Anna", unique_id="4711")
        ],
    )
    bot.add_to_hass(hass)
    sub = next(iter(bot.subentries))
    registry = er.async_get(hass)
    chat = registry.async_get_or_create(
        "notify", "telegram_bot", "bot_4711", config_entry=bot, config_subentry_id=sub, suggested_object_id="telegram_anna"
    )
    person = registry.async_get_or_create("person", "person", "anna", suggested_object_id="anna")
    if chat_owner:
        registry.async_update_entity_options(person.entity_id, DOMAIN, {"channels": [chat.entity_id]})
    calls: list[ServiceCall] = []

    async def record(call: ServiceCall) -> None:
        calls.append(call)

    hass.services.async_register("telegram_bot", "send_message", record)
    hass.services.async_register("telegram_bot", "send_chat_action", record)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    return chat.entity_id, calls


async def test_a_persons_chat_talks_to_mira(hass, aioclient_mock, entry):
    aioclient_mock.post(
        f"{AGENT_URL}/v1/conversation",
        json={"text": "Gern.", "conversation_id": "4711", "continue_conversation": False, "html": ["<b>Gern.</b>"]},
    )
    chat, calls = await _setup(hass, aioclient_mock, entry)
    hass.bus.async_fire("telegram_text", {"chat_id": 4711, "text": "Mach das Licht an", "user_id": 1})
    await hass.async_block_till_done()

    _, _, sent, _ = next(c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/conversation"))
    assert sent["channel"] == "telegram" and sent["person"] == "anna" and sent["conversation_id"] == "4711"
    assert [c.service for c in calls] == ["send_chat_action", "send_message"]
    assert calls[1].data == {"entity_id": chat, "message": "<b>Gern.</b>", "parse_mode": "html"}


async def test_a_chat_of_nobody_is_ignored(hass, aioclient_mock, entry):
    _, calls = await _setup(hass, aioclient_mock, entry, chat_owner=False)
    hass.bus.async_fire("telegram_text", {"chat_id": 4711, "text": "Hallo"})
    await hass.async_block_till_done()
    assert not calls
    assert not any(str(c[1]).endswith("/v1/conversation") for c in aioclient_mock.mock_calls)


async def test_new_conversation_command(hass, aioclient_mock, entry):
    aioclient_mock.post(f"{AGENT_URL}/v1/conversation/reset", json={"reset": True})
    chat, calls = await _setup(hass, aioclient_mock, entry)
    hass.bus.async_fire("telegram_command", {"chat_id": 4711, "command": "/neu", "args": []})
    await hass.async_block_till_done()
    _, _, sent, _ = next(c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/conversation/reset"))
    assert sent == {"channel": "telegram", "conversation_id": "4711"}
    assert [c.service for c in calls] == ["send_message"]
