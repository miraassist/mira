"""Presence (the Show's radar) and whether an alarm or timer rings."""

from __future__ import annotations

from homeassistant.components.binary_sensor import BinarySensorDeviceClass, BinarySensorEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .entity import FakeEntity, present

SENSORS = {
    "presence": ("Presence", BinarySensorDeviceClass.OCCUPANCY),
    "sounding": ("Sounding", BinarySensorDeviceClass.SOUND),
    "alarm_sounding": ("Alarm sounding", BinarySensorDeviceClass.SOUND),
    "timer_sounding": ("Timer sounding", BinarySensorDeviceClass.SOUND),
}


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeBinary(dev, suffix, *spec) for dev, suffix, spec in present(entry, "binary_sensor", SENSORS))


class FakeBinary(FakeEntity, BinarySensorEntity):
    """Presence is off until a test sets it (testha set binary_sensor.fake_show_presence on)."""

    def __init__(self, dev, suffix: str, name: str, device_class: BinarySensorDeviceClass) -> None:
        super().__init__(dev, "binary_sensor", suffix, name)
        self._suffix = suffix
        self._attr_device_class = device_class

    @property
    def is_on(self) -> bool:
        if self._suffix == "alarm_sounding":
            return self.dev.alarm_sounding
        if self._suffix == "timer_sounding":
            return self.dev.timer_sounding
        if self._suffix == "sounding":
            return self.dev.alarm_sounding or self.dev.timer_sounding
        return False
