"""What Assist sees is sent again when the agent's fingerprint differs from
ours, and never while Home Assistant is still starting."""

from homeassistant.core import CoreState

from custom_components.mira.managed import exposed_entities, exposed_hash

from .conftest import AGENT_ID, AGENT_URL, AGENT_USER


def _puts(aioclient_mock):
    return [c for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/ha")]


def _agent(aioclient_mock, fingerprint):
    """The agent answers the status with this fingerprint of its list."""
    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID, "ha_user_id": AGENT_USER})
    status = {"pending_messages": 0, "quiet_mode": False}
    if fingerprint is not None:
        status["exposed_hash"] = fingerprint
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=status)
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})


async def _poll(entry):
    await entry.runtime_data.coordinator.async_refresh()


def test_the_fingerprint_is_the_sha256_of_the_sorted_ids():
    assert exposed_hash(["b", "a"]) == exposed_hash(["a", "b"])
    assert exposed_hash(["a"]) != exposed_hash(["a", "b"])
    # The agent computes the same (cmd/mira/exposed_test.go has this value).
    assert exposed_hash(["a", "b"]) == "7e18f737311b2dc3b2f269dd78396b0351f14fb66efa879f768cb23181883c78"


async def test_a_differing_fingerprint_sends_the_list_again(hass, aioclient_mock, entry):
    _agent(aioclient_mock, "")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()

    # The agent has our list: nothing is sent.
    _agent(aioclient_mock, exposed_hash(exposed_entities(hass)))
    await _poll(entry)
    assert not _puts(aioclient_mock)

    # A device that appears later changes our list; the agent does not have it.
    hass.states.async_set("light.found_later", "off")
    await _poll(entry)
    assert len(_puts(aioclient_mock)) == 1

    # An agent that started with an empty database reports no list.
    _agent(aioclient_mock, "")
    await _poll(entry)
    assert len(_puts(aioclient_mock)) == 1


async def test_an_older_agent_without_a_fingerprint_is_left_alone(hass, aioclient_mock, entry):
    _agent(aioclient_mock, None)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    _agent(aioclient_mock, None)
    await _poll(entry)
    await _poll(entry)
    assert not _puts(aioclient_mock)


async def test_nothing_is_sent_while_home_assistant_is_starting(hass, aioclient_mock, entry):
    """A short list from the first seconds would replace a complete one."""
    hass.set_state(CoreState.starting)
    _agent(aioclient_mock, "old")
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    await _poll(entry)
    assert not _puts(aioclient_mock)

    hass.set_state(CoreState.running)
    await _poll(entry)
    assert len(_puts(aioclient_mock)) == 1
