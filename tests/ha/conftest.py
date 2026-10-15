"""Fixtures for the Mira integration tests."""

import pytest

from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.setup import async_setup_component
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.const import CONF_IMPORTED, DOMAIN

AGENT_URL = "http://mira.test:8787"
AGENT_ID = "01agent"


@pytest.fixture(autouse=True)
async def setup_core(hass, enable_custom_integrations):
    """Load custom_components/ and the core component conversation needs."""
    assert await async_setup_component(hass, "homeassistant", {})
    yield


@pytest.fixture
def entry(hass, aioclient_mock) -> MockConfigEntry:
    """A config entry pointing at the mocked agent, past the first import.
    The agent accepts the subentries it gets after every setup."""
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    e = MockConfigEntry(
        domain=DOMAIN,
        title="Mira",
        unique_id=AGENT_ID,
        data={CONF_URL: AGENT_URL, CONF_TOKEN: "secret", CONF_IMPORTED: True},
    )
    e.add_to_hass(hass)
    return e
