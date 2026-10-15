"""The Mira integration: forwards Assist conversations to the Mira agent."""

from __future__ import annotations

from dataclasses import dataclass
import json
import logging
from pathlib import Path
from typing import Any

import voluptuous as vol

from homeassistant.config_entries import ConfigEntry, ConfigEntryState
from homeassistant.const import CONF_TOKEN, CONF_URL, Platform
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse
from homeassistant.exceptions import HomeAssistantError, ServiceValidationError
from homeassistant.helpers import config_validation as cv, device_registry as dr
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.typing import ConfigType

from .api import MiraClient, MiraError
from .automations import async_register_commands
from .const import CONF_AGENT_USER, CONF_IMPORTED, DOMAIN
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


SERVICE_ASSESS = "assess"
SERVICE_DECIDE = "decide"
# What Mira looks up and condenses before judging (tasks/views.go).
DATA_SCHEMA = vol.All(
    cv.ensure_list,
    [
        vol.Schema(
            {
                vol.Required("entity_id"): cv.entity_id,
                vol.Optional("view", default="state"): vol.In(["state", "same_hour", "day"]),
            }
        )
    ],
)
JUDGEMENT_FIELDS = {
    vol.Optional("observation"): cv.string,
    vol.Optional("data"): DATA_SCHEMA,
    vol.Optional("memory"): vol.In(["relevant", "profile", "none"]),
    vol.Optional("task"): cv.string,
    vol.Optional("owner"): cv.string,
}
ASSESS_SCHEMA = vol.Schema(
    {
        vol.Required("goal"): cv.string,
        **JUDGEMENT_FIELDS,
        vol.Optional("cooldown"): cv.string,
        vol.Optional("notify"): cv.boolean,
        vol.Optional("audience"): cv.string,
        vol.Optional("confidentiality"): vol.In(["normal", "confidential"]),
        vol.Optional("dedupe_key"): cv.string,
        vol.Optional("expires_in"): cv.string,
    }
)
DECIDE_SCHEMA = vol.Schema({vol.Required("question"): cv.string, **JUDGEMENT_FIELDS})


@dataclass
class MiraData:
    """What a loaded entry holds."""

    client: MiraClient
    coordinator: MiraCoordinator


type MiraConfigEntry = ConfigEntry[MiraData]


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """Register Mira's actions for automations (mira.add_notice,
    mira.assess, mira.decide) and the commands the agent manages its
    automations with."""

    def loaded() -> MiraConfigEntry:
        entries = [
            e for e in hass.config_entries.async_entries(DOMAIN) if e.state is ConfigEntryState.LOADED
        ]
        if not entries:
            raise ServiceValidationError("No Mira agent is set up")
        return entries[0]

    async def add_notice(call: ServiceCall) -> ServiceResponse:
        entry = loaded()
        try:
            result = await entry.runtime_data.client.add_notice(dict(call.data))
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent did not accept the notice: {err}") from err
        await entry.runtime_data.coordinator.async_request_refresh()
        return result

    async def assess(call: ServiceCall) -> ServiceResponse:
        """Mira judges what happened against the goal and, if it is worth
        it, says it: importance, wording and delivery are the agent's."""
        entry = loaded()
        try:
            result = await entry.runtime_data.client.assess(dict(call.data))
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent could not assess: {err}") from err
        await entry.runtime_data.coordinator.async_request_refresh()
        return result

    async def decide(call: ServiceCall) -> ServiceResponse:
        """A yes/no answer from Mira, for the automation to act on
        (response_variable plus a template condition). No notice."""
        try:
            return await loaded().runtime_data.client.decide(dict(call.data))
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent could not decide: {err}") from err

    hass.services.async_register(
        DOMAIN, SERVICE_ADD_NOTICE, add_notice, schema=ADD_NOTICE_SCHEMA, supports_response=SupportsResponse.OPTIONAL
    )
    hass.services.async_register(
        DOMAIN, SERVICE_ASSESS, assess, schema=ASSESS_SCHEMA, supports_response=SupportsResponse.OPTIONAL
    )
    hass.services.async_register(
        DOMAIN, SERVICE_DECIDE, decide, schema=DECIDE_SCHEMA, supports_response=SupportsResponse.ONLY
    )
    async_register_commands(hass)
    return True


async def async_setup_entry(hass: HomeAssistant, entry: MiraConfigEntry) -> bool:
    """Set up Mira from a config entry."""
    client = MiraClient(async_get_clientsession(hass), entry.data[CONF_URL], entry.data[CONF_TOKEN])
    health = await _adopt(hass, entry, client)
    # Home Assistant reads an integration's manifest once at start, so its
    # version there goes stale when the files are replaced and the entry
    # reloaded. The device shows the versions read now: the integration's
    # from its manifest file, the agent's from /v1/health.
    integration_version = await hass.async_add_executor_job(_manifest_version)
    dr.async_get(hass).async_get_or_create(
        config_entry_id=entry.entry_id,
        identifiers={(DOMAIN, entry.entry_id)},
        name="Mira",
        manufacturer="Mira",
        entry_type=dr.DeviceEntryType.SERVICE,
        sw_version=f"Integration {integration_version} · Agent {health.get('version', '?')}",
    )
    coordinator = MiraCoordinator(hass, entry, client)
    # Not first_refresh: an agent that is down at boot must not take the
    # conversation entity with it. That entity speaks an error instead of
    # leaving the satellite silent; the sensor and switch go unavailable.
    await coordinator.async_refresh()
    entry.runtime_data = MiraData(client=client, coordinator=coordinator)
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    entry.async_on_unload(entry.add_update_listener(_async_reload))
    return True


def _manifest_version() -> str:
    with open(Path(__file__).parent / "manifest.json", encoding="utf-8") as f:
        return json.load(f).get("version", "?")


async def _adopt(hass: HomeAssistant, entry: MiraConfigEntry, client: MiraClient) -> dict[str, Any]:
    """Give an entry from before agents had ids its id, and import the
    agent's persons and rooms as subentries once. Both wait for
    the next start if the agent is down now."""
    try:
        health = await client.health()
    except MiraError:
        health = {}
    agent_id = health.get("id")
    if entry.unique_id is None and agent_id and not hass.config_entries.async_entry_for_domain_unique_id(DOMAIN, agent_id):
        hass.config_entries.async_update_entry(entry, unique_id=agent_id)
    # The agent's own Home Assistant user may manage Mira's automations.
    if (user := health.get("ha_user_id")) and entry.data.get(CONF_AGENT_USER) != user:
        hass.config_entries.async_update_entry(entry, data={**entry.data, CONF_AGENT_USER: user})
    if entry.data.get(CONF_IMPORTED):
        return health
    try:
        agent = await client.managed_config()
    except MiraError as err:
        _LOGGER.info("Importing persons and rooms waits for the agent: %s", err)
        return health
    have = {s.unique_id for s in entry.subentries.values()}
    for subentry in import_subentries(agent):
        if subentry.unique_id not in have:
            hass.config_entries.async_add_subentry(entry, subentry)
    hass.config_entries.async_update_entry(entry, data={**entry.data, CONF_IMPORTED: True})
    return health


async def async_remove_entry(hass: HomeAssistant, entry: MiraConfigEntry) -> None:
    """Hand persons and rooms back to the agent's web UI."""
    client = MiraClient(async_get_clientsession(hass), entry.data[CONF_URL], entry.data[CONF_TOKEN])
    try:
        await client.release_managed_config()
    except MiraError as err:
        _LOGGER.warning("The agent still thinks Home Assistant manages its persons and rooms: %s", err)


async def async_unload_entry(hass: HomeAssistant, entry: MiraConfigEntry) -> bool:
    """Unload a config entry."""
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)


async def _async_reload(hass: HomeAssistant, entry: MiraConfigEntry) -> None:
    """Apply changed options."""
    await hass.config_entries.async_reload(entry.entry_id)
