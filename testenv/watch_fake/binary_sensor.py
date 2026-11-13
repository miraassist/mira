"""The fake openings and motion sensors of the group watch (see house.py), set with watch_fake.set."""

from __future__ import annotations

from homeassistant.components.binary_sensor import BinarySensorEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback, AddEntitiesCallback
from homeassistant.helpers.restore_state import RestoreEntity

from . import DOMAIN
from .devices import entities_of
from .house import MOTION, OPENINGS, place


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    sensors = [FakeBinary(*row) for row in OPENINGS + MOTION]
    hass.data[DOMAIN]["settable"].extend(sensors)
    add(sensors)



async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    """The fake devices' entities of the battery and the availability watch (devices.py)."""
    entities = entities_of("binary_sensor")
    hass.data[DOMAIN]["devices"].extend(entities)
    add(entities)

class FakeBinary(BinarySensorEntity, RestoreEntity):
    """A window, a door or a motion sensor: on, off, or not readable."""

    _attr_should_poll = False

    def __init__(self, name: str, unique_id: str, device_class: str, area: str) -> None:
        self._attr_name = name
        self._attr_unique_id = unique_id
        self._attr_device_class = device_class
        self._attr_is_on = False
        self._area = area

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        place(self.hass, self.entity_id, self._area)
        if (last := await self.async_get_last_state()) is not None and last.state in ("on", "off"):
            self._attr_is_on = last.state == "on"

    @callback
    def set_state(self, state: str) -> None:
        self._attr_available = state not in ("unavailable", "unknown")
        self._attr_is_on = None if state == "unknown" else state == "on"
        self.async_write_ha_state()
