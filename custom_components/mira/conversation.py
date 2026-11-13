"""The conversation entity: hands every turn to the service, nothing else.

No LLM API, no tools, no prompt here. The service owns all of that; this entity
forwards text, conversation id, satellite device id and language, and speaks
what comes back, while it is written (docs/spec/streaming.md).
"""

from __future__ import annotations

import json
import logging
from typing import Any, Literal

from homeassistant.components import conversation
from homeassistant.const import MATCH_ALL
from homeassistant.core import HomeAssistant
from homeassistant.helpers import intent, llm
from homeassistant.helpers.device_registry import DeviceEntryType, DeviceInfo
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import MiraConfigEntry
from .api import AgentReply, MiraError, ToolCallPart
from .const import BROKEN_TEXT, CONF_TIMEOUT, DEFAULT_TIMEOUT, DOMAIN, ERROR_TEXT

_LOGGER = logging.getLogger(__name__)


async def async_setup_entry(
    hass: HomeAssistant,
    entry: MiraConfigEntry,
    async_add_entities: AddConfigEntryEntitiesCallback,
) -> None:
    """Set up the conversation entity."""
    async_add_entities([MiraConversationEntity(entry)])


class MiraConversationEntity(conversation.ConversationEntity, conversation.AbstractConversationAgent):
    """Forwards Assist turns to the Mira service."""

    _attr_has_entity_name = True
    _attr_name = None
    # The answer goes into the chat log while the service writes it, so a
    # satellite can speak before it is complete.
    _attr_supports_streaming = True
    # The service controls the house itself, through its own MCP connection.
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
        # keeps the service's session (and its prompt cache) together.
        conversation_id = chat_log.conversation_id
        timeout = self.entry.options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)
        lang = (user_input.language or "en").split("-")[0]
        said: list[str] = []
        reply: AgentReply | None = None

        async def deltas():
            nonlocal reply
            # A tool's result closes the assistant message; what follows
            # opens the next one.
            open_message = True
            yield {"role": "assistant"}
            try:
                async for part in self.entry.runtime_data.client.converse_stream(
                    text=user_input.text,
                    conversation_id=conversation_id,
                    device_id=user_input.device_id,
                    language=user_input.language,
                    agent_id=user_input.agent_id,
                    extra_system_prompt=user_input.extra_system_prompt,
                    timeout=timeout,
                    # Who is signed in (the app, its chat): Mira maps it to a
                    # person. Satellites have no user.
                    user_id=user_input.context.user_id,
                ):
                    if isinstance(part, AgentReply):
                        reply = part
                        continue
                    if isinstance(part, ToolCallPart) and part.result is not None:
                        # The service ran the tool itself: an external call's
                        # result, for the debug view.
                        open_message = False
                        yield {
                            "role": "tool_result",
                            "tool_call_id": part.id,
                            "tool_name": part.name,
                            "result": llm.ToolResult(data={"result": part.result}),
                        }
                        continue
                    if not open_message:
                        open_message = True
                        yield {"role": "assistant"}
                    if isinstance(part, ToolCallPart):
                        # Text and then a tool call: Home Assistant starts
                        # speaking the text at once (docs/spec/streaming.md).
                        yield {
                            "tool_calls": [
                                llm.ToolInput(
                                    id=part.id, tool_name=part.name, tool_args=_args(part.arguments), external=True
                                )
                            ]
                        }
                    else:
                        said.append(part)
                        yield {"content": part}
            except MiraError as err:
                _LOGGER.warning("Mira service did not answer: %s", err)
                # Part of the answer was said: a short sentence ends it.
                if said:
                    text = BROKEN_TEXT.get(lang, BROKEN_TEXT["en"])
                    text = text if said[-1].endswith(" ") else f" {text}"
                else:
                    text = ERROR_TEXT.get(lang, ERROR_TEXT["en"])
                said.append(text)
                if not open_message:
                    yield {"role": "assistant"}
                yield {"content": text}
                return
            if reply is not None and not said and reply.text:
                # An older service answers whole.
                said.append(reply.text)
                if not open_message:
                    yield {"role": "assistant"}
                yield {"content": reply.text}

        async for _content in chat_log.async_add_delta_content_stream(user_input.agent_id, deltas()):
            pass
        response = intent.IntentResponse(language=user_input.language)
        response.async_set_speech("".join(said).strip())
        return conversation.ConversationResult(
            response=response,
            conversation_id=conversation_id,
            continue_conversation=reply is not None and reply.continue_conversation,
        )


def _args(arguments: str) -> dict[str, Any]:
    """A tool call's arguments as the chat log takes them."""
    try:
        args = json.loads(arguments) if arguments else {}
    except ValueError:
        return {"arguments": arguments}
    return args if isinstance(args, dict) else {"arguments": args}
