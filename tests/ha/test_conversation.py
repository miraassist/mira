"""Conversation entity: a pure forwarder to the agent."""

from homeassistant.components import conversation
from homeassistant.core import Context
from homeassistant.helpers import intent

from .conftest import AGENT_URL

ENTITY_ID = "conversation.lares"


async def _setup(hass, entry):
    assert await hass.config_entries.async_setup(entry.entry_id)
    # The status poll is not mocked here: an unreachable status must not
    # keep the conversation entity from loading.
    await hass.async_block_till_done()


async def _say(hass, text, conversation_id=None, device_id=None, extra=None):
    return await conversation.async_converse(
        hass, text, conversation_id, Context(), language="de", agent_id=ENTITY_ID,
        device_id=device_id, extra_system_prompt=extra,
    )


async def test_turn_is_forwarded_and_spoken(hass, aioclient_mock, entry):
    aioclient_mock.post(
        f"{AGENT_URL}/v1/conversation",
        json={"text": "Das Küchenlicht ist an.", "conversation_id": "x", "continue_conversation": False},
    )
    await _setup(hass, entry)
    assert hass.states.get(ENTITY_ID) is not None

    result = await _say(hass, "Licht an", device_id="sat-kueche", extra="Notice: Wechselrichter")

    assert result.response.speech["plain"]["speech"] == "Das Küchenlicht ist an."
    assert result.continue_conversation is False
    _, url, body, headers = aioclient_mock.mock_calls[-1]
    assert str(url) == f"{AGENT_URL}/v1/conversation"
    assert headers["Authorization"] == "Bearer secret"
    assert body["text"] == "Licht an"
    assert body["device_id"] == "sat-kueche"
    assert body["language"] == "de"
    assert body["agent_id"] == ENTITY_ID
    assert body["extra_system_prompt"] == "Notice: Wechselrichter"
    # HA's chat session id is passed on, so the agent can key its session on it.
    assert body["conversation_id"] == result.conversation_id
    assert result.conversation_id


async def test_question_keeps_the_satellite_listening(hass, aioclient_mock, entry):
    aioclient_mock.post(
        f"{AGENT_URL}/v1/conversation",
        json={"text": "Welches Licht?", "conversation_id": "x", "continue_conversation": True},
    )
    await _setup(hass, entry)

    first = await _say(hass, "Licht an")
    assert first.continue_conversation is True

    # The follow-up reuses the conversation id, so the agent sees one session.
    await _say(hass, "Das in der Küche", conversation_id=first.conversation_id)
    assert aioclient_mock.mock_calls[-1][2]["conversation_id"] == first.conversation_id


async def test_agent_failure_is_spoken_not_silent(hass, aioclient_mock, entry):
    aioclient_mock.post(f"{AGENT_URL}/v1/conversation", exc=TimeoutError())
    await _setup(hass, entry)

    result = await _say(hass, "Licht an")

    assert result.response.response_type is intent.IntentResponseType.ACTION_DONE
    assert "nicht antworten" in result.response.speech["plain"]["speech"]
    assert result.continue_conversation is False


async def test_unload(hass, aioclient_mock, entry):
    await _setup(hass, entry)
    assert await hass.config_entries.async_unload(entry.entry_id)
    await hass.async_block_till_done()
