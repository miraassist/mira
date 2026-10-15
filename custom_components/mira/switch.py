"""switch.mira_quiet_mode: no announcements while on."""

from __future__ import annotations

from typing import Any

from homeassistant.components.switch import SwitchEntity
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import MiraConfigEntry
from .api import MiraError
from .entity import MiraEntity


async def async_setup_entry(
    hass: HomeAssistant, entry: MiraConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    async_add_entities([QuietModeSwitch(entry.runtime_data.coordinator, "quiet_mode")])


class QuietModeSwitch(MiraEntity, SwitchEntity):
    """The agent's quiet mode."""

    _attr_icon = "mdi:volume-off"

    @property
    def is_on(self) -> bool | None:
        return (self.coordinator.data or {}).get("quiet_mode")

    async def async_turn_on(self, **kwargs: Any) -> None:
        await self._set(True)

    async def async_turn_off(self, **kwargs: Any) -> None:
        await self._set(False)

    async def _set(self, enabled: bool) -> None:
        try:
            await self.coordinator.client.set_quiet_mode(enabled)
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent did not accept quiet mode: {err}") from err
        self.coordinator.async_set_updated_data({**(self.coordinator.data or {}), "quiet_mode": enabled})
