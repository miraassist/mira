"""The discovered flows of the fake (docs/spec/found-devices.md, Testing)."""

from __future__ import annotations

from typing import Any

import voluptuous as vol

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult

DOMAIN = "found_fake"


class FoundFakeFlow(ConfigFlow, domain=DOMAIN):
    VERSION = 1

    def __init__(self) -> None:
        self._info: dict[str, Any] = {}

    async def async_step_import(self, data: dict[str, Any]) -> ConfigFlowResult:
        await self.async_set_unique_id("app")
        self._abort_if_unique_id_configured()
        return self.async_create_entry(title="Fake app", data={"app": True})

    async def async_step_integration_discovery(self, info: dict[str, Any]) -> ConfigFlowResult:
        await self.async_set_unique_id(info["uid"])
        self._abort_if_unique_id_configured()
        self._info = dict(info)
        self.context["title_placeholders"] = {"name": info["name"]}
        return await {"confirm": self.async_step_confirm, "button": self.async_step_link,
                      "fields": self.async_step_credentials, "wifi": self.async_step_provision}[info["kind"]]()

    def _create(self, extra: dict[str, Any] | None = None) -> ConfigFlowResult:
        return self.async_create_entry(title=self._info["name"], data={**self._info, **(extra or {})})

    async def async_step_confirm(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            return self._create()
        return self.async_show_form(step_id="confirm", description_placeholders={"name": self._info["name"]})

    async def async_step_link(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        errors = {}
        if user_input is not None:
            if self._info["uid"] in self.hass.data.get(DOMAIN, {}).get("pressed", set()):
                return self._create()
            errors["base"] = "not_pressed"
        return self.async_show_form(step_id="link", errors=errors, description_placeholders={"name": self._info["name"]})

    async def async_step_credentials(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            return self._create({"api_key": user_input["api_key"]})
        return self.async_show_form(step_id="credentials", data_schema=vol.Schema({vol.Required("api_key"): str}))

    async def async_step_provision(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        if user_input is not None:
            self.hass.data.setdefault(DOMAIN, {}).setdefault("provisioned", {})[self._info["uid"]] = user_input["ssid"]
            return self._create({"ssid": user_input["ssid"]})
        return self.async_show_form(step_id="provision", data_schema=vol.Schema(
            {vol.Required("ssid"): str, vol.Required("password"): str}))
