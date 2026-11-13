"""The fake water meter and the fake plug's power (see __init__.py)."""

from __future__ import annotations

from datetime import datetime, timedelta

from homeassistant.components.sensor import RestoreSensor, SensorDeviceClass, SensorStateClass
from homeassistant.const import UnitOfPower, UnitOfVolume
from homeassistant.core import CALLBACK_TYPE, HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddEntitiesCallback
from homeassistant.helpers.event import async_track_time_change, async_track_time_interval
from homeassistant.util import dt as dt_util

from . import DOMAIN


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    meter, plug = FakeMeter(), FakePlug()
    hass.data[DOMAIN].update(meter=meter, plug=plug)
    add([meter, plug])


class FakeMeter(RestoreSensor):
    """A water meter counting litres; it changes only while water runs."""

    _attr_name = "Fake Wasserzähler"
    _attr_unique_id = "watch_fake_water_meter"
    _attr_device_class = SensorDeviceClass.WATER
    _attr_state_class = SensorStateClass.TOTAL_INCREASING
    _attr_native_unit_of_measurement = UnitOfVolume.LITERS
    _attr_should_poll = False
    _attr_suggested_display_precision = 0

    def __init__(self) -> None:
        self._attr_native_value = 81240.0
        self._unsub: CALLBACK_TYPE | None = None
        self._until: datetime | None = None
        self._litres = 0.0

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if (last := await self.async_get_last_sensor_data()) and last.native_value is not None:
            self._attr_native_value = float(last.native_value)

    async def async_will_remove_from_hass(self) -> None:
        self.stop()

    @callback
    def run(self, litres: float, every: float, seconds: float | None) -> None:
        """Count up litres now and every `every` seconds, for `seconds` or until stopped."""
        self.stop()
        self._litres = litres
        self._until = dt_util.utcnow() + timedelta(seconds=seconds) if seconds else None
        self._unsub = async_track_time_interval(self.hass, self._step, timedelta(seconds=every))
        self._step()

    @callback
    def stop(self) -> None:
        if self._unsub:
            self._unsub()
            self._unsub = None

    @callback
    def _step(self, _now: datetime | None = None) -> None:
        if self._until and dt_util.utcnow() > self._until:
            self.stop()
            return
        self._attr_native_value = float(self._attr_native_value or 0) + self._litres
        self.async_write_ha_state()


class FakePlug(RestoreSensor):
    """A plug's power: set by hand, or following a daily routine."""

    _attr_name = "Fake Aquarium Leistung"
    _attr_unique_id = "watch_fake_plug_power"
    _attr_device_class = SensorDeviceClass.POWER
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_should_poll = False

    def __init__(self) -> None:
        self._attr_native_value = 0.0
        self._unsub: CALLBACK_TYPE | None = None
        self._windows: list[tuple[int, int]] = []
        self._watts = 0.0

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if (last := await self.async_get_last_sensor_data()) and last.native_value is not None:
            self._attr_native_value = float(last.native_value)

    async def async_will_remove_from_hass(self) -> None:
        self._end_routine()

    @callback
    def set_power(self, watts: float) -> None:
        """Set the power now; a routine ends."""
        self._end_routine()
        self._set(watts)

    @callback
    def follow(self, windows: str, watts: float) -> None:
        """Follow a daily routine: watts within the windows, else 0, now and at every full minute."""
        self._end_routine()
        self._windows = [_window(w) for w in windows.split(",")]
        self._watts = watts
        self._unsub = async_track_time_change(self.hass, self._tick, second=0)
        self._tick()

    @callback
    def _end_routine(self) -> None:
        if self._unsub:
            self._unsub()
            self._unsub = None

    @callback
    def _tick(self, _now: datetime | None = None) -> None:
        now = dt_util.now()
        minute = now.hour * 60 + now.minute
        on = any(s <= minute < e if s <= e else (minute >= s or minute < e) for s, e in self._windows)
        self._set(self._watts if on else 0.0)

    @callback
    def _set(self, watts: float) -> None:
        if self._attr_native_value != watts:
            self._attr_native_value = watts
            self.async_write_ha_state()


def _window(text: str) -> tuple[int, int]:
    start, end = (part.strip().split(":") for part in text.split("-"))
    return int(start[0]) * 60 + int(start[1]), int(end[0]) * 60 + int(end[1])
