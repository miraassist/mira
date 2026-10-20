"""Which assistant and wake word, the sleep timer and the sleep sounds. The
sounds play on the device itself, not on its speaker: a test sees whether
something turned them off."""

from __future__ import annotations

from homeassistant.components.select import SelectEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .entity import FakeEntity, present

NOISE = ["None", "White", "Pink", "Brown", "Rain", "Ocean", "Brook", "Wind", "Fire", "Crickets", "Fan", "Cabin"]
SELECTS = {
    "assistent": ("Assistent", ["preferred"], "preferred"),
    "aktivierungswort": ("Aktivierungswort", ["Okay Nabu", "Hey Jarvis", "Alexa"], "Okay Nabu"),
    "sleep_timer": ("Sleep timer", ["Off", "15 minutes", "30 minutes", "45 minutes", "1 hour", "1 hour 30 minutes", "2 hours"], "Off"),
    "playback_white_noise_layer_1": ("playback White noise layer 1", NOISE, "None"),
    "playback_white_noise_layer_2": ("playback White noise layer 2", NOISE, "None"),
}


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeSelect(dev, suffix, *spec) for dev, suffix, spec in present(entry, "select", SELECTS))


class FakeSelect(FakeEntity, SelectEntity):
    def __init__(self, dev, suffix: str, name: str, options: list[str], initial: str) -> None:
        super().__init__(dev, "select", suffix, name)
        self._suffix = suffix
        self._attr_options = options
        self._attr_current_option = initial

    async def async_select_option(self, option: str) -> None:
        self.dev.record("select", select=self._suffix, option=option)
        self._attr_current_option = option
        self.async_write_ha_state()
