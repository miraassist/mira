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
from homeassistant.helpers import selector
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .api import MiraAuthError, MiraClient, MiraError, MiraInvalidConfig
from .const import CONF_SIDEBAR, CONF_TIMEOUT, DEFAULT_TIMEOUT, DOMAIN
from .managed import (
    PERSON_FIELDS,
    SATELLITE_DOMAINS,
    SATELLITE_FIELDS,
    build_config,
    person_settings,
    satellite_area,
    satellite_settings,
    set_person_settings,
    set_satellite_settings,
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



class MiraOptionsFlow(OptionsFlow):
    """General options, and Mira's settings for one of Home Assistant's
    persons. Every HA person is a person for Mira anyway; the settings only
    add what Mira needs on top, stored on the person (managed.py)."""

    _person: str = ""

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        return self.async_show_menu(step_id="init", menu_options=["general", "person_pick", "satellite_pick"])

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

    async def async_step_person_pick(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            self._person = user_input["entity"]
            return await self.async_step_person()
        return self.async_show_form(
            step_id="person_pick",
            data_schema=vol.Schema(
                {vol.Required("entity"): selector.EntitySelector(selector.EntitySelectorConfig(domain="person"))}
            ),
        )

    async def async_step_person(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        entry = self.config_entry
        if entry.state is not ConfigEntryState.LOADED:
            return self.async_abort(reason="not_loaded")
        old = person_settings(self.hass).get(self._person, {})
        errors: dict[str, str] = {}
        placeholders = {"error": "", "person": self._name()}
        if user_input is not None:
            data = {k: user_input.get(k) for k in PERSON_FIELDS}
            telegram = str(data.get("telegram_user_id") or "").strip()
            try:
                if telegram and not telegram.isdigit():
                    raise ValueError(f"Telegram user id {telegram!r} is not a number")
                data["telegram_user_id"] = telegram
                await entry.runtime_data.client.set_managed_config(
                    build_config(self.hass, entry, person=(self._person, data))
                )
            except (ValueError, MiraInvalidConfig) as err:
                errors["base"], placeholders["error"] = "invalid_config", str(err)
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                set_person_settings(self.hass, self._person, data)
                return self.async_create_entry(data=dict(entry.options))
        notify = sorted(f"notify.{name}" for name in self.hass.services.async_services_for_domain("notify"))
        schema = vol.Schema(
            {
                vol.Optional("telegram_user_id"): selector.TextSelector(),
                vol.Optional("notify"): selector.SelectSelector(
                    selector.SelectSelectorConfig(
                        options=notify, custom_value=True, mode=selector.SelectSelectorMode.DROPDOWN
                    )
                ),
                vol.Optional("alone_is_me", default=False): selector.BooleanSelector(),
            }
        )
        return self.async_show_form(
            step_id="person",
            data_schema=self.add_suggested_values_to_schema(schema, user_input or old),
            errors=errors,
            description_placeholders=placeholders,
        )

    def _name(self, entity_id: str = "") -> str:
        entity_id = entity_id or self._person
        state = self.hass.states.get(entity_id)
        return state.name if state else entity_id

    _satellite: str = ""

    async def async_step_satellite_pick(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            self._satellite = user_input["entity"]
            if satellite_area(self.hass, self._satellite):
                return await self.async_step_satellite()
            errors["base"] = "no_area"
        return self.async_show_form(
            step_id="satellite_pick",
            data_schema=vol.Schema(
                {
                    vol.Required("entity"): selector.EntitySelector(
                        selector.EntitySelectorConfig(domain=list(SATELLITE_DOMAINS))
                    )
                }
            ),
            errors=errors,
        )

    async def async_step_satellite(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Mira's settings on a satellite: its motion sensors (several may
        say someone came in), the morning announcement, announce or
        indicate, the indicator light. Its room is its area."""
        entry = self.config_entry
        if entry.state is not ConfigEntryState.LOADED:
            return self.async_abort(reason="not_loaded")
        old = satellite_settings(self.hass).get(self._satellite, {})
        errors: dict[str, str] = {}
        placeholders = {"error": "", "satellite": self._name(self._satellite)}
        if user_input is not None:
            data = {k: user_input.get(k) for k in SATELLITE_FIELDS}
            try:
                await entry.runtime_data.client.set_managed_config(
                    build_config(self.hass, entry, satellite=(self._satellite, data))
                )
            except (ValueError, MiraInvalidConfig) as err:
                errors["base"], placeholders["error"] = "invalid_config", str(err)
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                set_satellite_settings(self.hass, self._satellite, data)
                return self.async_create_entry(data=dict(entry.options))
        schema = vol.Schema(
            {
                vol.Optional("presence"): selector.EntitySelector(
                    selector.EntitySelectorConfig(domain="binary_sensor", multiple=True)
                ),
                vol.Required("morning", default=True): selector.BooleanSelector(),
                vol.Required("mode", default="announce"): selector.SelectSelector(
                    selector.SelectSelectorConfig(options=["announce", "indicate"], translation_key="room_mode")
                ),
                vol.Optional("indicator"): selector.EntitySelector(selector.EntitySelectorConfig(domain="light")),
            }
        )
        return self.async_show_form(
            step_id="satellite",
            data_schema=self.add_suggested_values_to_schema(schema, user_input or old),
            errors=errors,
            description_placeholders=placeholders,
        )
