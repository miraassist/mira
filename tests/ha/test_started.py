"""The agent says it started: the integration sends its settings at once
(docs/spec/editing.md, decision 10)."""

from custom_components.mira.const import EVENT_STARTED

from .conftest import AGENT_ID, AGENT_URL


def _puts(aioclient_mock):
    return [c for c in aioclient_mock.mock_calls if c[0] == "PUT" and str(c[1]).endswith("/v1/ui/ha")]


async def _set_up(hass, aioclient_mock, entry):
    # A restarted agent: an empty fingerprint, so every look sends.
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"status": "ok", "exposed_hash": ""})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    return len(_puts(aioclient_mock))


async def test_BE24_the_integration_sends_its_settings_on_mira_started(hass, aioclient_mock, entry):
    before = await _set_up(hass, aioclient_mock, entry)
    hass.bus.async_fire(EVENT_STARTED, {"id": AGENT_ID, "version": "0.22.0"})
    await hass.async_block_till_done()
    assert len(_puts(aioclient_mock)) == before + 1


async def test_BE25_another_miras_start_is_left_alone(hass, aioclient_mock, entry):
    before = await _set_up(hass, aioclient_mock, entry)
    hass.bus.async_fire(EVENT_STARTED, {"id": "another", "version": "0.22.0"})
    await hass.async_block_till_done()
    assert len(_puts(aioclient_mock)) == before
