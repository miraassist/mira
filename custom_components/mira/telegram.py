"""Telegram as a conversation channel, through Home Assistant's own Telegram
integration (telegram_bot), see docs/spec/kanaele.md.

Mira runs no bot of her own. This adapter is the only place that knows
HA's Telegram events and actions:
- in: a `telegram_text` event from a chat that belongs to a person (one of
  their channels is that chat's notify entity) becomes a turn for Mira,
  channel "telegram", the chat as conversation;
- out: Mira's answer goes back to that chat with `telegram_bot.send_message`
  (Telegram HTML), "typing …" shows while she thinks.
Chats that belong to no person are ignored: HA's list of allowed chats plus
the person's channels decide who talks to Mira.
"""

from __future__ import annotations

import logging
from typing import Any

from homeassistant.core import CALLBACK_TYPE, Event, HomeAssistant, callback
from homeassistant.helpers import entity_registry as er

from .api import MiraError
from .const import ERROR_TEXT
from .managed import person_key, person_settings

_LOGGER = logging.getLogger(__name__)

TELEGRAM = "telegram_bot"
EVENT_TEXT = "telegram_text"
EVENT_COMMAND = "telegram_command"
# Commands that start a new conversation.
NEW = ("/neu", "/new", "/start")
NEW_TEXT = {"de": "Okay, neues Gespräch.", "en": "Okay, new conversation."}
# Answers over Telegram may take longer than at a satellite: nobody waits
# with a microphone open, and web searches are common here.
TIMEOUT = 120


def chats_of_persons(hass: HomeAssistant) -> dict[int, tuple[str, str]]:
    """Telegram chat id -> (person key, the chat's notify entity), for the
    chats among the persons' channels."""
    registry = er.async_get(hass)
    out: dict[int, tuple[str, str]] = {}
    for person, settings in person_settings(hass).items():
        for channel in settings.get("channels") or []:
            entry = registry.async_get(channel)
            if entry is None or entry.platform != TELEGRAM or not entry.config_subentry_id:
                continue
            bot = hass.config_entries.async_get_entry(entry.config_entry_id or "")
            chat = bot.subentries.get(entry.config_subentry_id) if bot else None
            if chat is not None and "chat_id" in chat.data:
                out[int(chat.data["chat_id"])] = (person_key(hass, person), channel)
    return out


@callback
def async_setup_telegram(hass: HomeAssistant, entry: Any) -> CALLBACK_TYPE:
    """Listen to Telegram for the loaded Mira entry; returns the unsubscribe."""

    async def on_text(event: Event) -> None:
        text = (event.data.get("text") or "").strip()
        chat = event.data.get("chat_id")
        if not text or chat is None:
            return
        target = chats_of_persons(hass).get(int(chat))
        if target is None:
            _LOGGER.debug("Telegram chat %s belongs to no person; ignored", chat)
            return
        person, notify_entity = target
        await _call(hass, "send_chat_action", {"entity_id": notify_entity, "chat_action": "typing"})
        try:
            reply = await entry.runtime_data.client.converse(
                text=text,
                conversation_id=str(chat),
                device_id=None,
                language=hass.config.language,
                agent_id="",
                extra_system_prompt=None,
                timeout=TIMEOUT,
                channel="telegram",
                person=person,
            )
            parts, mode = reply.html or [reply.text], "html"
        except MiraError as err:
            _LOGGER.warning("Mira agent did not answer on Telegram: %s", err)
            lang = (hass.config.language or "en").split("-")[0]
            parts, mode = [ERROR_TEXT.get(lang, ERROR_TEXT["en"])], None
        for part in parts:
            data = {"entity_id": notify_entity, "message": part}
            if mode:
                data["parse_mode"] = mode
            await _call(hass, "send_message", data)

    async def on_command(event: Event) -> None:
        command = str(event.data.get("command") or "").split("@", 1)[0].lower()
        chat = event.data.get("chat_id")
        if command not in NEW or chat is None:
            return
        target = chats_of_persons(hass).get(int(chat))
        if target is None:
            return
        try:
            await entry.runtime_data.client.reset_conversation("telegram", str(chat))
        except MiraError as err:
            _LOGGER.warning("Mira agent could not start a new conversation: %s", err)
            return
        lang = (hass.config.language or "en").split("-")[0]
        await _call(hass, "send_message", {"entity_id": target[1], "message": NEW_TEXT.get(lang, NEW_TEXT["en"])})

    unsubscribe = [hass.bus.async_listen(EVENT_TEXT, on_text), hass.bus.async_listen(EVENT_COMMAND, on_command)]

    @callback
    def stop() -> None:
        for unsub in unsubscribe:
            unsub()

    return stop


async def _call(hass: HomeAssistant, service: str, data: dict[str, Any]) -> None:
    try:
        await hass.services.async_call(TELEGRAM, service, data, blocking=True)
    except Exception as err:  # noqa: BLE001 - a failed send must not end the listener
        _LOGGER.warning("telegram_bot.%s failed: %s", service, err)
