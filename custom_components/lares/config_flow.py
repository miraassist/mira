"""Config flow for Lares: agent URL and token, checked against /v1/health."""

from __future__ import annotations

from typing import Any

import voluptuous as vol

from homeassistant.config_entries import ConfigEntry, ConfigFlow, ConfigFlowResult, OptionsFlow
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.core import callback
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .api import LaresAuthError, LaresClient, LaresError
from .const import CONF_TIMEOUT, DEFAULT_TIMEOUT, DOMAIN

USER_SCHEMA = vol.Schema(
    {
        vol.Required(CONF_URL, default="http://lares.local:8787"): str,
        vol.Required(CONF_TOKEN): str,
    }
)


class LaresConfigFlow(ConfigFlow, domain=DOMAIN):
    """Ask for the agent's address and token and test them."""

    VERSION = 1

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            url = user_input[CONF_URL].rstrip("/")
            self._async_abort_entries_match({CONF_URL: url})
            client = LaresClient(async_get_clientsession(self.hass), url, user_input[CONF_TOKEN])
            try:
                await client.health()
            except LaresAuthError:
                errors["base"] = "invalid_auth"
            except LaresError:
                errors["base"] = "cannot_connect"
            else:
                return self.async_create_entry(
                    title="Lares", data={CONF_URL: url, CONF_TOKEN: user_input[CONF_TOKEN]}
                )
        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(USER_SCHEMA, user_input),
            errors=errors,
        )

    @staticmethod
    @callback
    def async_get_options_flow(config_entry: ConfigEntry) -> OptionsFlow:
        return LaresOptionsFlow()


class LaresOptionsFlow(OptionsFlow):
    """Let the user tune how long a satellite waits for the agent."""

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            return self.async_create_entry(data=user_input)
        current = self.config_entry.options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)
        return self.async_show_form(
            step_id="init",
            data_schema=vol.Schema(
                {vol.Required(CONF_TIMEOUT, default=current): vol.All(int, vol.Range(min=3, max=120))}
            ),
        )
