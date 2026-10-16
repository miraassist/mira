"""Config flow for Mira: agent URL and token, checked against /v1/health.

The agent announces itself by mDNS (_mira._tcp), so Home Assistant offers it
under "Discovered" and only asks for the token. Mira's settings for a person
or a satellite are set under "Configure" and stored on that person or
satellite; see managed.py.
"""

from __future__ import annotations

from typing import Any

import voluptuous as vol

from homeassistant.config_entries import (
    ConfigEntry,
    ConfigEntryState,
    ConfigFlow,
    ConfigFlowResult,
    OptionsFlow,
)
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.core import callback
from homeassistant.data_entry_flow import section
from homeassistant.helpers import selector
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .api import MiraAuthError, MiraClient, MiraError, MiraInvalidConfig
from .const import CONF_SIDEBAR, CONF_TIMEOUT, DEFAULT_TIMEOUT, DOMAIN
from .managed import (
    ACCEPT,
    all_persons,
    all_satellites,
    entity_name,
    notify_targets,
    person_settings,
    satellite_settings,
    save_settings,
)

USER_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_URL, default="http://mira.local:8787"): str,
        vol.Required(CONF_TOKEN): str,
    }
)
TOKEN_SCHEMA = vol.Schema({vol.Required(CONF_TOKEN): str})


class MiraConfigFlow(ConfigFlow, domain=DOMAIN):
    """Ask for the agent's address and token and test them."""

    VERSION = 1

    _url: str = ""

    async def _check(self, url: str, token: str) -> tuple[dict[str, str], dict[str, Any]]:
        """Test address and token; returns (errors, health)."""
        client = MiraClient(async_get_clientsession(self.hass), url, token)
        try:
            return {}, await client.health()
        except MiraAuthError:
            return {"base": "invalid_auth"}, {}
        except MiraError:
            return {"base": "cannot_connect"}, {}

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            url = user_input[CONF_URL].rstrip("/")
            self._async_abort_entries_match({CONF_URL: url})
            errors, health = await self._check(url, user_input[CONF_TOKEN])
            if not errors:
                if health.get("id"):
                    await self.async_set_unique_id(health["id"])
                    self._abort_if_unique_id_configured(updates={CONF_URL: url})
                return self.async_create_entry(
                    title="Mira", data={CONF_URL: url, CONF_TOKEN: user_input[CONF_TOKEN]}
                )
        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(USER_SCHEMA, user_input),
            errors=errors,
        )

    async def async_step_zeroconf(self, discovery_info: ZeroconfServiceInfo) -> ConfigFlowResult:
        """An agent announced itself on the network."""
        agent_id = discovery_info.properties.get("id")
        if not agent_id:
            return self.async_abort(reason="no_id")
        host = discovery_info.host
        if ":" in host:
            host = f"[{host}]"
        url = f"http://{host}:{discovery_info.port}"
        await self.async_set_unique_id(agent_id)
        self._abort_if_unique_id_configured(updates={CONF_URL: url})
        # An entry from before agents had ids: same address, same agent.
        for entry in self._async_current_entries(include_ignore=False):
            if entry.unique_id is None and entry.data.get(CONF_URL) == url:
                self.hass.config_entries.async_update_entry(entry, unique_id=agent_id)
                return self.async_abort(reason="already_configured")
        self._url = url
        self.context["title_placeholders"] = {"name": discovery_info.name.split(".")[0], "url": url}
        return await self.async_step_zeroconf_confirm()

    async def async_step_zeroconf_confirm(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Only the token is missing."""
        errors: dict[str, str] = {}
        if user_input is not None:
            errors, _ = await self._check(self._url, user_input[CONF_TOKEN])
            if not errors:
                return self.async_create_entry(
                    title="Mira", data={CONF_URL: self._url, CONF_TOKEN: user_input[CONF_TOKEN]}
                )
        return self.async_show_form(
            step_id="zeroconf_confirm",
            data_schema=TOKEN_SCHEMA,
            errors=errors,
            description_placeholders={"url": self._url},
        )

    @staticmethod
    @callback
    def async_get_options_flow(config_entry: ConfigEntry) -> OptionsFlow:
        return MiraOptionsFlow()



# Field names inside the per-person and per-satellite sections. Home
# Assistant translates a section's fields by the section's key, and those
# keys are names of persons and satellites, so the fields carry readable
# names themselves, in Home Assistant's language.
FIELD_NAMES = {
    "de": {
        "channels": "Kanäle (notify-Ziele, z. B. Telegram-Chat, Companion-App)",
        "accept_from": "Nachrichten annehmen von",
        "alone_is_me": "Allein zu Hause sind die Satelliten ich",
        "presence": "Bewegungsmelder",
        "morning": "Morgenansage",
        "mode": "Zustellart",
        "indicator": "Anzeige-Lampe",
    },
    "en": {
        "channels": "Channels (notify targets, e.g. Telegram chat, companion app)",
        "accept_from": "Take messages from",
        "alone_is_me": "Alone at home, the satellites are me",
        "presence": "Motion sensors",
        "morning": "Morning announcement",
        "mode": "Delivery",
        "indicator": "Indicator light",
    },
}


class MiraOptionsFlow(OptionsFlow):
    """General options, and Mira's settings on Home Assistant's persons and
    satellites: one form each, with a collapsible section per person or
    satellite, filled with what is set now. The settings are stored on the
    persons and satellites themselves (managed.py)."""

    _keys: dict[str, str] = {}

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        return self.async_show_menu(step_id="init", menu_options=["general", "persons", "satellites"])

    async def async_step_general(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            return self.async_create_entry(data=dict(self.config_entry.options) | user_input)
        options = self.config_entry.options
        return self.async_show_form(
            step_id="general",
            data_schema=vol.Schema(
                {
                    vol.Required(CONF_TIMEOUT, default=options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)): vol.All(
                        int, vol.Range(min=3, max=120)
                    ),
                    vol.Required(CONF_SIDEBAR, default=options.get(CONF_SIDEBAR, True)): bool,
                }
            ),
        )

    async def async_step_persons(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        fields = self._named(
            {
                vol.Optional("channels"): selector.SelectSelector(
                    selector.SelectSelectorConfig(
                        options=notify_targets(self.hass), custom_value=True, multiple=True,
                        mode=selector.SelectSelectorMode.DROPDOWN,
                    )
                ),
                vol.Optional("accept_from", default="all"): selector.SelectSelector(
                    selector.SelectSelectorConfig(options=list(ACCEPT), translation_key="accept_from")
                ),
                vol.Optional("alone_is_me", default=False): selector.BooleanSelector(),
            }
        )
        return await self._sections("persons", all_persons(self.hass), person_settings(self.hass), fields, user_input)

    async def async_step_satellites(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        fields = self._named(
            {
                vol.Optional("presence"): selector.EntitySelector(
                    selector.EntitySelectorConfig(domain="binary_sensor", multiple=True)
                ),
                vol.Optional("morning", default=False): selector.BooleanSelector(),
                vol.Optional("mode", default="announce"): selector.SelectSelector(
                    selector.SelectSelectorConfig(options=["announce", "indicate"], translation_key="room_mode")
                ),
                vol.Optional("indicator"): selector.EntitySelector(selector.EntitySelectorConfig(domain="light")),
            }
        )
        return await self._sections(
            "satellites", all_satellites(self.hass), satellite_settings(self.hass), fields, user_input
        )

    def _names(self) -> dict[str, str]:
        return FIELD_NAMES.get((self.hass.config.language or "en")[:2], FIELD_NAMES["en"])

    def _named(self, fields: dict[Any, Any]) -> vol.Schema:
        """The fields under their readable names."""
        names = self._names()
        return vol.Schema({type(k)(names[str(k)], **_marker_args(k)): v for k, v in fields.items()})

    async def _sections(
        self,
        step_id: str,
        entities: list[str],
        current: dict[str, dict[str, Any]],
        fields: vol.Schema,
        user_input: dict[str, Any] | None,
    ) -> ConfigFlowResult:
        entry = self.config_entry
        if entry.state is not ConfigEntryState.LOADED:
            return self.async_abort(reason="not_loaded")
        if not entities:
            return self.async_abort(reason=f"no_{step_id}")
        # A section is named by what it is for; its key is the name, which
        # the form shows as the section's title.
        keys: dict[str, str] = {}
        for entity in entities:
            name = entity_name(self.hass, entity)
            keys[name if name not in keys else f"{name} ({entity})"] = entity
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            back = {v: k for k, v in self._names().items()}
            changes = {keys[k]: {back.get(f, f): x for f, x in v.items()} for k, v in user_input.items() if k in keys}
            try:
                if step_id == "persons":
                    await save_settings(self.hass, entry, persons=changes)
                else:
                    await save_settings(self.hass, entry, satellites=changes)
            except (ValueError, MiraInvalidConfig) as err:
                errors["base"], placeholders["error"] = "invalid_config", str(err)
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                return self.async_create_entry(data=dict(entry.options))
        schema = vol.Schema(
            {
                vol.Required(key): section(
                    self.add_suggested_values_to_schema(
                        fields,
                        (user_input or {}).get(key)
                        or {self._names().get(f, f): x for f, x in current.get(entity, {}).items()},
                    ),
                    {"collapsed": not current.get(entity)},
                )
                for key, entity in keys.items()
            }
        )
        return self.async_show_form(
            step_id=step_id,
            data_schema=schema,
            errors=errors,
            description_placeholders=placeholders,
        )


def _marker_args(marker: vol.Marker) -> dict[str, Any]:
    """A marker's default and description, to rename it."""
    out: dict[str, Any] = {}
    if marker.default is not vol.UNDEFINED:
        out["default"] = marker.default
    if marker.description is not None:
        out["description"] = marker.description
    return out
