"""The fake answering machine's two sensors, as Mira FRITZ!Box's (see __init__.py)."""

from __future__ import annotations

from collections.abc import Callable

from homeassistant.components.sensor import SensorEntity
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback

from . import DOMAIN, Box


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    box: Box = hass.data[DOMAIN]
    add(
        [
            Count(box, "Anrufbeantworter Neue Nachrichten", "fake_fritz_new_messages", box.new_messages),
            Count(box, "Anrufbeantworter Verpasste Anrufe", "fake_fritz_missed_calls", box.missed),
        ]
    )


class Count(SensorEntity):
    """A count of the box: new messages or missed calls."""

    _attr_should_poll = False

    def __init__(self, box: Box, name: str, unique_id: str, count: Callable[[], int]) -> None:
        self._box, self._count = box, count
        self._attr_name, self._attr_unique_id = name, unique_id

    @property
    def native_value(self) -> int:
        return self._count()

    async def async_added_to_hass(self) -> None:
        self._box.listeners.append(self._changed)

    async def async_will_remove_from_hass(self) -> None:
        self._box.listeners.remove(self._changed)

    @callback
    def _changed(self) -> None:
        self.async_write_ha_state()
