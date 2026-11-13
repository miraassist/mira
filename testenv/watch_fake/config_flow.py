"""The config entry "Fake Geräte" of watch_fake (devices.py): made by watch_fake itself at its setup."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigFlow, ConfigFlowResult

DOMAIN = "watch_fake"


class WatchFakeFlow(ConfigFlow, domain=DOMAIN):
    VERSION = 1

    async def async_step_import(self, data: dict[str, Any]) -> ConfigFlowResult:
        await self.async_set_unique_id("devices")
        self._abort_if_unique_id_configured()
        return self.async_create_entry(title="Fake Geräte", data={})
