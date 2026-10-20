"""One entry, created from configuration.yaml or by hand."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult

from .const import DOMAIN


class FakeFlow(ConfigFlow, domain=DOMAIN):
    VERSION = 1

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        await self.async_set_unique_id(DOMAIN)
        self._abort_if_unique_id_configured()
        return self.async_create_entry(title="TECHO5 Fake", data={})

    async def async_step_import(self, import_data: dict[str, Any]) -> ConfigFlowResult:
        return await self.async_step_user(import_data)
