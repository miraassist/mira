"""mira.agent, mira.agent_result, mira.agent_cancel and the event
mira_agent_done (docs/spec/agenten.md, Calling)."""

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
        f"{AGENT_URL}/v1/agent", json={"call_id": "c1", "agent": "rezept", "status": "done", "output": {"steps": ["Teig"]}}
    )
    result = await hass.services.async_call(
        DOMAIN, "agent", {"agent": "rezept", "input": {"url": "https://example.com"}}, blocking=True, return_response=True
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
        {"type": "mira/agent/done", "call_id": "c1", "agent": "rezept", "status": "done", "output": '{"steps": ["Teig"]}'}
    )
    assert (await agent.receive_json())["success"]
    await hass.async_block_till_done()
    assert events == [{"call_id": "c1", "agent": "rezept", "status": "done", "output": {"steps": ["Teig"]}}]


async def test_only_the_agent_fires_the_done_event(hass, aioclient_mock, entry, hass_ws_client, hass_read_only_access_token):
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
    result = await hass.services.async_call(DOMAIN, "agent_result", {"call_id": "c1"}, blocking=True, return_response=True)
    assert result == {"call_id": "c1", "status": "running"}
    result = await hass.services.async_call(DOMAIN, "agent_cancel", {"call_id": "c1"}, blocking=True, return_response=True)
    assert result == {"cancelled": True}
    assert _sent(aioclient_mock, "/v1/agent/cancel") == {"call_id": "c1"}


async def test_unknown_agent_says_why(hass, aioclient_mock, entry):
    """AG-66: the call fails with the agent's reason."""
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.post(f"{AGENT_URL}/v1/agent", status=404, json={"error": "no such agent: gibtsnicht"})
    with pytest.raises(HomeAssistantError, match="no such agent: gibtsnicht"):
        await hass.services.async_call(DOMAIN, "agent", {"agent": "gibtsnicht"}, blocking=True, return_response=True)
