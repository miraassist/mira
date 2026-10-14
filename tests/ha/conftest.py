"""Fixtures for the Lares integration tests."""

import pytest

from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.setup import async_setup_component
from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.lares.const import DOMAIN

AGENT_URL = "http://lares.test:8787"


@pytest.fixture(autouse=True)
async def setup_core(hass, enable_custom_integrations):
    """Load custom_components/ and the core component conversation needs."""
    assert await async_setup_component(hass, "homeassistant", {})
    yield


@pytest.fixture
def entry(hass) -> MockConfigEntry:
    """A config entry pointing at the mocked agent."""
    e = MockConfigEntry(domain=DOMAIN, title="Lares", data={CONF_URL: AGENT_URL, CONF_TOKEN: "secret"})
    e.add_to_hass(hass)
    return e
