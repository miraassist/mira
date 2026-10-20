"""The Echo's LED ring, the indicator Mira lets pulse while messages wait."""

from __future__ import annotations

from typing import Any

from homeassistant.components.light import ATTR_BRIGHTNESS, ATTR_EFFECT, ATTR_RGB_COLOR, ColorMode, LightEntity, LightEntityFeature
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .entity import FakeEntity, present


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeRing(dev) for dev, _, _ in present(entry, "light", {"ring_led_ring": None}))


class FakeRing(FakeEntity, LightEntity):
    _attr_color_mode = ColorMode.RGB
    _attr_supported_color_modes = {ColorMode.RGB}
    _attr_supported_features = LightEntityFeature.EFFECT
    _attr_effect_list = ["None", "Pulse", "Spin", "Breathe"]

    def __init__(self, dev) -> None:
        super().__init__(dev, "light", "ring_led_ring", "ring LED ring")
        self._attr_is_on = False
        self._attr_brightness = 255
        self._attr_rgb_color = (0, 120, 255)
        self._attr_effect = "None"

    async def async_turn_on(self, **kwargs: Any) -> None:
        self.dev.record("ring_on", **{k: (list(v) if isinstance(v, tuple) else v) for k, v in kwargs.items()})
        self._attr_is_on = True
        self._attr_brightness = kwargs.get(ATTR_BRIGHTNESS, self._attr_brightness)
        self._attr_rgb_color = kwargs.get(ATTR_RGB_COLOR, self._attr_rgb_color)
        self._attr_effect = kwargs.get(ATTR_EFFECT, self._attr_effect)
        self.async_write_ha_state()

    async def async_turn_off(self, **kwargs: Any) -> None:
        self.dev.record("ring_off")
        self._attr_is_on = False
        self.async_write_ha_state()
