"""The conversation entity: hands every turn to the agent, nothing else.

No LLM API, no tools, no prompt here. The agent owns all of that; this entity
forwards text, conversation id, satellite device id and language, and speaks
what comes back.
"""

from __future__ import annotations

import logging
from typing import Literal

from homeassistant.components import conversation
from homeassistant.const import MATCH_ALL
from homeassistant.core import HomeAssistant
from homeassistant.helpers import intent
from homeassistant.helpers.device_registry import DeviceEntryType, DeviceInfo
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import MiraConfigEntry
from .api import MiraError
from .const import CONF_TIMEOUT, DEFAULT_TIMEOUT, DOMAIN, ERROR_TEXT

_LOGGER = logging.getLogger(__name__)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: MiraConfigEntry,
    async_add_entities: AddConfigEntryEntitiesCallback,
) -> None:
    """Set up the conversation entity."""
    async_add_entities([MiraConversationEntity(entry)])


class MiraConversationEntity(conversation.ConversationEntity, conversation.AbstractConversationAgent):
    """Forwards Assist turns to the Mira agent."""

    _attr_has_entity_name = True
    _attr_name = None
    # The agent controls the house itself, through its own MCP connection.
    _attr_supported_features = conversation.ConversationEntityFeature.CONTROL

    def __init__(self, entry: MiraConfigEntry) -> None:
        self.entry = entry
        self._attr_unique_id = entry.entry_id
        self._attr_device_info = DeviceInfo(
            identifiers={(DOMAIN, entry.entry_id)},
            name="Mira",
            manufacturer="Mira",
            entry_type=DeviceEntryType.SERVICE,
        )

    @property
    def supported_languages(self) -> list[str] | Literal["*"]:
        return MATCH_ALL

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        conversation.async_set_agent(self.hass, self.entry, self)

    async def async_will_remove_from_hass(self) -> None:
        conversation.async_unset_agent(self.hass, self.entry)
        await super().async_will_remove_from_hass()

    async def _async_handle_message(
        self,
        user_input: conversation.ConversationInput,
        chat_log: conversation.ChatLog,
    ) -> conversation.ConversationResult:
        # The chat session's id is stable across a follow-up, which is what
        # keeps the agent's session (and its prompt cache) together.
        conversation_id = chat_log.conversation_id
        timeout = self.entry.options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)
        continue_conversation = False
        try:
            reply = await self.entry.runtime_data.client.converse(
                text=user_input.text,
                conversation_id=conversation_id,
                device_id=user_input.device_id,
                language=user_input.language,
                agent_id=user_input.agent_id,
                extra_system_prompt=user_input.extra_system_prompt,
                timeout=timeout,
            )
            text = reply.text
            continue_conversation = reply.continue_conversation
        except MiraError as err:
            _LOGGER.warning("Mira agent did not answer: %s", err)
            lang = (user_input.language or "en").split("-")[0]
            text = ERROR_TEXT.get(lang, ERROR_TEXT["en"])

        chat_log.async_add_assistant_content_without_tools(
            conversation.AssistantContent(agent_id=user_input.agent_id, content=text)
        )
        response = intent.IntentResponse(language=user_input.language)
        response.async_set_speech(text)
        return conversation.ConversationResult(
            response=response,
            conversation_id=conversation_id,
            continue_conversation=continue_conversation,
        )
