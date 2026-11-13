"""The fake lights of the group watch (see house.py): switched as every light, or with watch_fake.set."""

from __future__ import annotations

from typing import Any

from homeassistant.components.light import ColorMode, LightEntity
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.restore_state import RestoreEntity

from . import DOMAIN
from .house import LIGHTS, place


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    lights = [FakeLight(*row) for row in LIGHTS]
    hass.data[DOMAIN]["settable"].extend(lights)
    add(lights)


class FakeLight(LightEntity, RestoreEntity):
    """A light that is only on or off."""

    _attr_should_poll = False
    _attr_color_mode = ColorMode.ONOFF
    _attr_supported_color_modes = {ColorMode.ONOFF}

    def __init__(self, name: str, unique_id: str, area: str) -> None:
        self._attr_name = name
        self._attr_unique_id = unique_id
        self._attr_is_on = False
        self._area = area

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        place(self.hass, self.entity_id, self._area)
        if (last := await self.async_get_last_state()) is not None and last.state in ("on", "off"):
            self._attr_is_on = last.state == "on"

    async def async_turn_on(self, **kwargs: Any) -> None:
        self.set_state("on")

    async def async_turn_off(self, **kwargs: Any) -> None:
        self.set_state("off")

    @callback
    def set_state(self, state: str) -> None:
        self._attr_available = state not in ("unavailable", "unknown")
        self._attr_is_on = None if state == "unknown" else state == "on"
        self.async_write_ha_state()
