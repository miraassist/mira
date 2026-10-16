"""Config flow for Mira: agent URL and token, checked against /v1/health.

The agent announces itself by mDNS (_mira._tcp), so Home Assistant offers it
under "Discovered" and only asks for the token. Persons and rooms
are subentries with HA's own pickers; see managed.py.
"""

from __future__ import annotations

from typing import Any

import voluptuous as vol

from homeassistant.config_entries import (
    ConfigEntry,
    ConfigEntryState,
    ConfigFlow,
    ConfigFlowResult,
    ConfigSubentryFlow,
    OptionsFlow,
    SubentryFlowResult,
)
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.core import callback
from homeassistant.helpers import area_registry as ar, selector
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .api import MiraAuthError, MiraClient, MiraError, MiraInvalidConfig
from .const import CONF_SIDEBAR, CONF_TIMEOUT, DEFAULT_TIMEOUT, DOMAIN
from .managed import SUBENTRY_PERSON, SUBENTRY_ROOM, build_config, unique_id

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

    @classmethod
    @callback
    def async_get_supported_subentry_types(cls, config_entry: ConfigEntry) -> dict[str, type[ConfigSubentryFlow]]:
        return {
            SUBENTRY_PERSON: PersonSubentryFlow,
            SUBENTRY_ROOM: RoomSubentryFlow,
        }


class MiraOptionsFlow(OptionsFlow):
    """How long a satellite waits for the agent; Mira in the sidebar."""

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            return self.async_create_entry(data=user_input)
        options = self.config_entry.options
        return self.async_show_form(
            step_id="init",
            data_schema=vol.Schema(
                {
                    vol.Required(CONF_TIMEOUT, default=options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)): vol.All(
                        int, vol.Range(min=3, max=120)
                    ),
                    vol.Required(CONF_SIDEBAR, default=options.get(CONF_SIDEBAR, True)): bool,
                }
            ),
        )


class _ManagedSubentryFlow(ConfigSubentryFlow):
    """One form for adding and changing; the agent checks the result first.

    The whole config, with this form applied, goes to the agent before the
    subentry is saved: what the agent refuses (an unknown area, a person
    the Telegram allowlist does not know) is shown in the form instead.
    """

    subentry_type: str

    def _schema(self, data: dict[str, Any]) -> vol.Schema:
        raise NotImplementedError

    def _title(self, data: dict[str, Any]) -> str:
        raise NotImplementedError

    def _normalize(self, data: dict[str, Any]) -> dict[str, Any]:
        return data

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> SubentryFlowResult:
        return await self._form("user", None, user_input)

    async def async_step_reconfigure(self, user_input: dict[str, Any] | None = None) -> SubentryFlowResult:
        return await self._form("reconfigure", self._get_reconfigure_subentry().subentry_id, user_input)

    async def _form(self, step_id: str, subentry_id: str | None, user_input: dict[str, Any] | None) -> SubentryFlowResult:
        entry = self._get_entry()
        if entry.state is not ConfigEntryState.LOADED:
            return self.async_abort(reason="not_loaded")
        old = dict(entry.subentries[subentry_id].data) if subentry_id else {}
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            # Keep what the form does not show (imported extras); a field
            # the user cleared is missing from user_input and stays cleared.
            shown = {str(key) for key in self._schema(old).schema}
            data = {k: v for k, v in old.items() if k not in shown} | user_input
            uid = ""
            try:
                data = self._normalize(data)
                uid = unique_id(self.subentry_type, data)
                if any(s.unique_id == uid and sid != subentry_id for sid, s in entry.subentries.items()):
                    errors["base"] = "already_configured"
                else:
                    await entry.runtime_data.client.set_managed_config(
                        build_config(entry, (subentry_id, self.subentry_type, data))
                    )
            except (ValueError, MiraInvalidConfig) as err:
                errors["base"], placeholders["error"] = "invalid_config", str(err)
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                if subentry_id is None:
                    return self.async_create_entry(title=self._title(data), data=data, unique_id=uid)
                return self.async_update_and_abort(
                    entry, self._get_reconfigure_subentry(), title=self._title(data), data=data, unique_id=uid
                )
        schema = self.add_suggested_values_to_schema(self._schema(old), user_input or old)
        return self.async_show_form(
            step_id=step_id, data_schema=schema, errors=errors, description_placeholders=placeholders
        )


class PersonSubentryFlow(_ManagedSubentryFlow):
    """A person who gets personal and private notices."""

    subentry_type = SUBENTRY_PERSON

    def _schema(self, data: dict[str, Any]) -> vol.Schema:
        notify = sorted(f"notify.{name}" for name in self.hass.services.async_services_for_domain("notify"))
        return vol.Schema(
            {
                vol.Required("entity"): selector.EntitySelector(selector.EntitySelectorConfig(domain="person")),
                vol.Optional("telegram_user_id"): selector.TextSelector(),
                vol.Optional("notify"): selector.SelectSelector(
                    selector.SelectSelectorConfig(
                        options=notify, custom_value=True, mode=selector.SelectSelectorMode.DROPDOWN
                    )
                ),
                vol.Optional("alone_is_me", default=False): selector.BooleanSelector(),
            }
        )

    def _normalize(self, data: dict[str, Any]) -> dict[str, Any]:
        # The key is what audience refers to; it stays once given.
        if not data.get("key"):
            data["key"] = data["entity"].split(".", 1)[1]
        telegram = str(data.get("telegram_user_id") or "").strip()
        if telegram and not telegram.isdigit():
            raise ValueError(f"Telegram user id {telegram!r} is not a number")
        data["telegram_user_id"] = telegram
        return data

    def _title(self, data: dict[str, Any]) -> str:
        state = self.hass.states.get(data["entity"])
        return state.name if state else data["key"]


class RoomSubentryFlow(_ManagedSubentryFlow):
    """An area with a satellite that speaks there and a presence sensor."""

    subentry_type = SUBENTRY_ROOM

    def _schema(self, data: dict[str, Any]) -> vol.Schema:
        return vol.Schema(
            {
                vol.Required("area"): selector.AreaSelector(),
                vol.Optional("satellite"): selector.EntitySelector(
                    selector.EntitySelectorConfig(domain=["assist_satellite", "media_player"])
                ),
                vol.Optional("presence"): selector.EntitySelector(
                    selector.EntitySelectorConfig(domain="binary_sensor")
                ),
                vol.Required("morning", default=True): selector.BooleanSelector(),
                vol.Required("mode", default="announce"): selector.SelectSelector(
                    selector.SelectSelectorConfig(options=["announce", "indicate"], translation_key="room_mode")
                ),
                vol.Optional("indicator"): selector.EntitySelector(selector.EntitySelectorConfig(domain="light")),
            }
        )

    def _title(self, data: dict[str, Any]) -> str:
        area = ar.async_get(self.hass).async_get_area(data["area"])
        return area.name if area else data["area"]
