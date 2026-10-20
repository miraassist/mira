"""Stop what rings, snooze an alarm."""

from __future__ import annotations

from homeassistant.components.button import ButtonEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .entity import FakeEntity, present

BUTTONS = {"stop_alarm_or_timer": "Stop alarm or timer", "snooze_alarm": "Snooze alarm"}


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeButton(dev, suffix, name) for dev, suffix, name in present(entry, "button", BUTTONS))


class FakeButton(FakeEntity, ButtonEntity):
    def __init__(self, dev, suffix: str, name: str) -> None:
        super().__init__(dev, "button", suffix, name)
        self._suffix = suffix

    async def async_press(self) -> None:
        self.dev.record("press", button=self._suffix)
        self.dev.stop_sounding()
