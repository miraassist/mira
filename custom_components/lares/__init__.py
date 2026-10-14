"""The Lares integration: forwards Assist conversations to the Lares agent."""

from __future__ import annotations

from dataclasses import dataclass

import voluptuous as vol

from homeassistant.config_entries import ConfigEntry, ConfigEntryState
from homeassistant.const import CONF_TOKEN, CONF_URL, Platform
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse
from homeassistant.exceptions import HomeAssistantError, ServiceValidationError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.typing import ConfigType

from .api import LaresClient, LaresError
from .const import DOMAIN
from .coordinator import LaresCoordinator

PLATFORMS = [Platform.CONVERSATION, Platform.SENSOR, Platform.SWITCH]

CONFIG_SCHEMA = cv.config_entry_only_config_schema(DOMAIN)

SERVICE_ADD_NOTICE = "add_notice"
ADD_NOTICE_SCHEMA = vol.Schema(
    {
        vol.Required("text"): cv.string,
        vol.Optional("details"): cv.string,
        vol.Optional("importance", default=1): vol.All(vol.Coerce(int), vol.Range(min=0, max=3)),
        vol.Optional("audience"): cv.string,
        vol.Optional("confidentiality"): vol.In(["normal", "confidential"]),
        vol.Optional("dedupe_key"): cv.string,
        vol.Optional("expires_in"): cv.string,
    }
)


@dataclass
class LaresData:
    """What a loaded entry holds."""

    client: LaresClient
    coordinator: LaresCoordinator


type LaresConfigEntry = ConfigEntry[LaresData]


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """Register lares.add_notice, so automations can queue notices."""

    async def add_notice(call: ServiceCall) -> ServiceResponse:
        entries = [
            e for e in hass.config_entries.async_entries(DOMAIN) if e.state is ConfigEntryState.LOADED
        ]
        if not entries:
            raise ServiceValidationError("No Lares agent is set up")
        try:
            result = await entries[0].runtime_data.client.add_notice(dict(call.data))
        except LaresError as err:
            raise HomeAssistantError(f"Lares agent did not accept the notice: {err}") from err
        await entries[0].runtime_data.coordinator.async_request_refresh()
        return result

    hass.services.async_register(
        DOMAIN, SERVICE_ADD_NOTICE, add_notice, schema=ADD_NOTICE_SCHEMA, supports_response=SupportsResponse.OPTIONAL
    )
    return True


async def async_setup_entry(hass: HomeAssistant, entry: LaresConfigEntry) -> bool:
    """Set up Lares from a config entry."""
    client = LaresClient(async_get_clientsession(hass), entry.data[CONF_URL], entry.data[CONF_TOKEN])
    coordinator = LaresCoordinator(hass, entry, client)
    # Not first_refresh: an agent that is down at boot must not take the
    # conversation entity with it. That entity speaks an error instead of
    # leaving the satellite silent; the sensor and switch go unavailable.
    await coordinator.async_refresh()
    entry.runtime_data = LaresData(client=client, coordinator=coordinator)
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    entry.async_on_unload(entry.add_update_listener(_async_reload))
    return True


async def async_unload_entry(hass: HomeAssistant, entry: LaresConfigEntry) -> bool:
    """Unload a config entry."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def _async_reload(hass: HomeAssistant, entry: LaresConfigEntry) -> None:
    """Apply changed options."""
    await hass.config_entries.async_reload(entry.entry_id)
