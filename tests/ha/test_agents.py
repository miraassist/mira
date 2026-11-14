"""mira.agent, mira.agent_result, mira.agent_cancel and the event
mira_agent_done (docs/spec/agents.md, Calling)."""

import pytest
from homeassistant.exceptions import HomeAssistantError

from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}


async def _setup(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


def _sent(aioclient_mock, path):
    return [c for c in aioclient_mock.mock_calls if str(c[1]).endswith(path)][-1][2]


async def test_agent_answers_synchronously(hass, aioclient_mock, entry):
    """AG-62: the automation gets the agent's answer in its response_variable."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(
        f"{AGENT_URL}/v1/agent",
        json={"call_id": "c1", "agent": "rezept", "status": "done", "output": {"steps": ["Teig"]}},
    )
    result = await hass.services.async_call(
        DOMAIN,
        "agent",
        {"agent": "rezept", "input": {"url": "https://example.com"}},
        blocking=True,
        return_response=True,
    )
    assert result["output"] == {"steps": ["Teig"]}
    # AG-98: only agent and input given, the defaults go along.
    assert _sent(aioclient_mock, "/v1/agent") == {
        "agent": "rezept",
        "input": {"url": "https://example.com"},
        "mode": "sync",
        "timeout": 60,
        "on_timeout": "cancel",
        "delivery": "event",
    }


async def test_agent_asynchronously_with_the_done_event(hass, aioclient_mock, entry, hass_ws_client):
    """AG-63: the call id comes at once, the result as mira_agent_done."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(f"{AGENT_URL}/v1/agent", json={"call_id": "c1", "agent": "rezept", "status": "running"})
    result = await hass.services.async_call(
        DOMAIN, "agent", {"agent": "rezept", "input": {}, "mode": "async"}, blocking=True, return_response=True
    )
    assert result["call_id"] == "c1"

    events = []
    hass.bus.async_listen("mira_agent_done", lambda e: events.append(e.data))
    agent = await hass_ws_client(hass)
    await agent.send_json_auto_id(
        {
            "type": "mira/agent/done",
            "call_id": "c1",
            "agent": "rezept",
            "status": "done",
            "output": '{"steps": ["Teig"]}',
        }
    )
    assert (await agent.receive_json())["success"]
    await hass.async_block_till_done()
    assert events == [{"call_id": "c1", "agent": "rezept", "status": "done", "output": {"steps": ["Teig"]}}]


async def test_only_the_agent_fires_the_done_event(
    hass, aioclient_mock, entry, hass_ws_client, hass_read_only_access_token
):
    await _setup(hass, aioclient_mock, entry)
    events = []
    hass.bus.async_listen("mira_agent_done", lambda e: events.append(e.data))
    other = await hass_ws_client(hass, hass_read_only_access_token)
    await other.send_json_auto_id({"type": "mira/agent/done", "call_id": "c1", "agent": "x", "status": "done"})
    msg = await other.receive_json()
    await hass.async_block_till_done()
    assert not msg["success"] and msg["error"]["code"] == "unauthorized"
    assert events == []


async def test_agent_result_and_cancel(hass, aioclient_mock, entry):
    """AG-64 and AG-68: a run read and cancelled by its call id."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(f"{AGENT_URL}/v1/agent/result", json={"call_id": "c1", "status": "running"})
    aioclient_mock.post(f"{AGENT_URL}/v1/agent/cancel", json={"cancelled": True})
    result = await hass.services.async_call(
        DOMAIN, "agent_result", {"call_id": "c1"}, blocking=True, return_response=True
    )
    assert result == {"call_id": "c1", "status": "running"}
    result = await hass.services.async_call(
        DOMAIN, "agent_cancel", {"call_id": "c1"}, blocking=True, return_response=True
    )
    assert result == {"cancelled": True}
    assert _sent(aioclient_mock, "/v1/agent/cancel") == {"call_id": "c1"}


async def test_unknown_agent_says_why(hass, aioclient_mock, entry):
    """AG-66: the call fails with the agent's reason."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(f"{AGENT_URL}/v1/agent", status=404, json={"error": "no such agent: gibtsnicht"})
    with pytest.raises(HomeAssistantError, match="no such agent: gibtsnicht"):
        await hass.services.async_call(DOMAIN, "agent", {"agent": "gibtsnicht"}, blocking=True, return_response=True)


async def test_RM15_agent_with_skill_starts_its_thread(hass, aioclient_mock, entry):
    """RM-15: mira.agent with skill starts the skill's thread for the receiver
    with the input as the request; the response holds the thread's id."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(f"{AGENT_URL}/v1/agent/skill", json={"thread_id": "t1"})
    result = await hass.services.async_call(
        DOMAIN,
        "agent",
        {"skill": "recherche", "input": "Welche Wärmepumpe passt?", "receiver": "anna"},
        blocking=True,
        return_response=True,
    )
    assert result == {"thread_id": "t1"}
    # Only what was given goes along: no defaults of a call of an agent.
    assert _sent(aioclient_mock, "/v1/agent/skill") == {
        "skill": "recherche",
        "input": "Welche Wärmepumpe passt?",
        "receiver": "anna",
    }


async def test_agent_with_skill_refuses_the_fields_of_an_agent(hass, aioclient_mock, entry):
    """With skill, agent, mode, timeout, on_timeout and delivery are not allowed."""
    await _setup(hass, aioclient_mock, entry)
    for extra in ({"agent": "delegate"}, {"mode": "async"}, {"delivery": "message"}):
        with pytest.raises(Exception, match="not allowed"):
            await hass.services.async_call(
                DOMAIN, "agent", {"skill": "recherche", "input": "x", **extra}, blocking=True, return_response=True
            )
    with pytest.raises(Exception, match="name an agent or a skill"):
        await hass.services.async_call(DOMAIN, "agent", {"input": {}}, blocking=True, return_response=True)


async def test_RM16_agent_with_a_skill_without_a_target(hass, aioclient_mock, entry):
    """RM-16: the action fails naming the service's reason."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(
        f"{AGENT_URL}/v1/agent/skill",
        status=400,
        json={"error": "the skill wetter has no target: it has no thread to start"},
    )
    with pytest.raises(HomeAssistantError) as err:
        await hass.services.async_call(
            DOMAIN, "agent", {"skill": "wetter", "input": "x", "receiver": "anna"}, blocking=True, return_response=True
        )
    assert err.value.translation_key == "skill_failed"
    assert "has no target" in err.value.translation_placeholders["error"]


async def test_RM17_agent_with_skill_at_an_older_service(hass, aioclient_mock, entry):
    """RM-17: a service without the route says Mira needs an update."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(f"{AGENT_URL}/v1/agent/skill", status=404, json={"error": "404 page not found"})
    with pytest.raises(HomeAssistantError) as err:
        await hass.services.async_call(
            DOMAIN,
            "agent",
            {"skill": "recherche", "input": "x", "receiver": "anna"},
            blocking=True,
            return_response=True,
        )
    assert err.value.translation_key == "skill_needs_update"
