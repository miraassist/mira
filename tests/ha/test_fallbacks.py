"""Error handlers of the integration leave a trace (docs/spec/fallbacks.md, decision 4, row 36)."""

import logging

import aiohttp

from .conftest import AGENT_URL
from .test_backup import _fix, _restored

STATUS = {"pending_messages": 0, "quiet_mode": False}


async def test_FB29_an_answer_of_mira_the_integration_cannot_read_is_in_the_log(
    hass, aioclient_mock, entry, tmp_path, hass_client, caplog
):
    # Mira answers a restore with a body that is no JSON: a warning with the status.
    aioclient_mock.post(f"{AGENT_URL}/v1/restore", status=200, text="<html>proxy</html>")
    await _restored(hass, aioclient_mock, entry, tmp_path)
    with caplog.at_level(logging.WARNING, logger="custom_components.mira.api"):
        await _fix(hass_client, "restore")
    assert "Mira answered the restore with HTTP 200 and a body that is no JSON" in caplog.text


async def test_FB29_mira_not_reachable_at_setup_is_in_the_log(hass, aioclient_mock, entry, caplog):
    # Mira not reachable at setup: her id and settings wait for the next start, and the log says so.
    aioclient_mock.clear_requests()  # the fixture's Mira answers; this one does not
    aioclient_mock.get(f"{AGENT_URL}/v1/health", exc=aiohttp.ClientConnectionError("refused"))
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    with caplog.at_level(logging.INFO, logger="custom_components.mira"):
        await hass.config_entries.async_setup(entry.entry_id)
        await hass.async_block_till_done()
    assert "her id and settings are taken over at the next start" in caplog.text
