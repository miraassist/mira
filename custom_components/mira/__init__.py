"""The Mira integration: forwards Assist conversations to the Mira agent."""

from __future__ import annotations

from dataclasses import dataclass
import logging

import voluptuous as vol

from homeassistant.config_entries import ConfigEntry, ConfigEntryState
from homeassistant.const import CONF_TOKEN, CONF_URL, Platform
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse
from homeassistant.exceptions import HomeAssistantError, ServiceValidationError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.typing import ConfigType

from .api import MiraClient, MiraError
from .const import CONF_IMPORTED, DOMAIN
from .coordinator import MiraCoordinator
from .managed import import_subentries

_LOGGER = logging.getLogger(__name__)

PLATFORMS = [Platform.BUTTON, Platform.CONVERSATION, Platform.SENSOR, Platform.SWITCH]

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
class MiraData:
    """What a loaded entry holds."""

    client: MiraClient
    coordinator: MiraCoordinator


type MiraConfigEntry = ConfigEntry[MiraData]


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """Register mira.add_notice, so automations can queue notices."""

    async def add_notice(call: ServiceCall) -> ServiceResponse:
        entries = [
            e for e in hass.config_entries.async_entries(DOMAIN) if e.state is ConfigEntryState.LOADED
        ]
        if not entries:
            raise ServiceValidationError("No Mira agent is set up")
        try:
            result = await entries[0].runtime_data.client.add_notice(dict(call.data))
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent did not accept the notice: {err}") from err
        await entries[0].runtime_data.coordinator.async_request_refresh()
        return result

    hass.services.async_register(
        DOMAIN, SERVICE_ADD_NOTICE, add_notice, schema=ADD_NOTICE_SCHEMA, supports_response=SupportsResponse.OPTIONAL
    )
    return True


async def async_setup_entry(hass: HomeAssistant, entry: MiraConfigEntry) -> bool:
    """Set up Mira from a config entry."""
    client = MiraClient(async_get_clientsession(hass), entry.data[CONF_URL], entry.data[CONF_TOKEN])
    await _adopt(hass, entry, client)
    coordinator = MiraCoordinator(hass, entry, client)
    # Not first_refresh: an agent that is down at boot must not take the
    # conversation entity with it. That entity speaks an error instead of
    # leaving the satellite silent; the sensor and switch go unavailable.
    await coordinator.async_refresh()
    entry.runtime_data = MiraData(client=client, coordinator=coordinator)
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    entry.async_on_unload(entry.add_update_listener(_async_reload))
    return True


async def _adopt(hass: HomeAssistant, entry: MiraConfigEntry, client: MiraClient) -> None:
    """Give an entry from before agents had ids its id, and import the
    agent's watchers, persons and rooms as subentries once. Both wait for
    the next start if the agent is down now."""
    if entry.unique_id is None:
        try:
            agent_id = (await client.health()).get("id")
        except MiraError:
            agent_id = None
        if agent_id and not hass.config_entries.async_entry_for_domain_unique_id(DOMAIN, agent_id):
            hass.config_entries.async_update_entry(entry, unique_id=agent_id)
    if entry.data.get(CONF_IMPORTED):
        return
    try:
        agent = await client.managed_config()
    except MiraError as err:
        _LOGGER.info("Importing watchers, persons and rooms waits for the agent: %s", err)
        return
    have = {s.unique_id for s in entry.subentries.values()}
    for subentry in import_subentries(agent):
        if subentry.unique_id not in have:
            hass.config_entries.async_add_subentry(entry, subentry)
    hass.config_entries.async_update_entry(entry, data={**entry.data, CONF_IMPORTED: True})


async def async_remove_entry(hass: HomeAssistant, entry: MiraConfigEntry) -> None:
    """Hand watchers, persons and rooms back to the agent's web UI."""
    client = MiraClient(async_get_clientsession(hass), entry.data[CONF_URL], entry.data[CONF_TOKEN])
    try:
        await client.release_managed_config()
    except MiraError as err:
        _LOGGER.warning("The agent still thinks Home Assistant manages its watchers: %s", err)


async def async_unload_entry(hass: HomeAssistant, entry: MiraConfigEntry) -> bool:
    """Unload a config entry."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def _async_reload(hass: HomeAssistant, entry: MiraConfigEntry) -> None:
    """Apply changed options."""
    await hass.config_entries.async_reload(entry.entry_id)
