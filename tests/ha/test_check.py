"""mira.check, the integration's side (docs/spec/group-watch.md, C2): the action's fields go to the service's
/v1/check as they are, its answer comes back as the response; an older service without the route is said
(GW-19, GW-21)."""

from __future__ import annotations

import pytest
from homeassistant.exceptions import HomeAssistantError

from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL
from .test_entities import _setup

ANSWER = {
    "scope": "openings",
    "open": [{"id": "binary_sensor.fenster_bad", "name": "Fenster Bad", "state": "on", "area": "Bad"}],
    "unreadable": [],
    "checked": 9,
    "message_id": "m1",
}


async def test_GW19_mira_check_from_a_script(hass, aioclient_mock, entry):
    # Given a script calls mira.check with a response variable, and the fake bathroom window is open
    await _setup(hass, entry, aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/check", json=ANSWER)
    result = await hass.services.async_call(
        DOMAIN, "check", {"floors": "erdgeschoss", "receiver": "anna"}, blocking=True, return_response=True
    )
    # Then the response lists the bathroom window (the entry is the service's: message_id)
    assert result == ANSWER
    sent = next(c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/check"))[2]
    assert sent == {"floors": ["erdgeschoss"], "receiver": "anna"}


async def test_GW21_mira_check_at_an_older_service(hass, aioclient_mock, entry):
    # Given the service has no route /v1/check
    await _setup(hass, entry, aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/check", status=404)
    # When a script calls mira.check
    with pytest.raises(HomeAssistantError) as err:
        await hass.services.async_call(DOMAIN, "check", {}, blocking=True, return_response=True)
    # Then the action fails with "Mira kann das erst nach einem Update"
    assert err.value.translation_key == "check_needs_update"
    assert "only after an update" in str(err.value)


async def test_mira_check_refused_names_the_reason(hass, aioclient_mock, entry):
    await _setup(hass, entry, aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/check", status=400, json={"error": 'eine Etage "turm" gibt es nicht'})
    with pytest.raises(HomeAssistantError) as err:
        await hass.services.async_call(DOMAIN, "check", {"floors": ["turm"]}, blocking=True, return_response=True)
    assert err.value.translation_key == "check_failed" and "turm" in err.value.translation_placeholders["error"]
