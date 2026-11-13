"""The fake water meter and the fake plug's power (see __init__.py)."""

from __future__ import annotations

from datetime import datetime, timedelta

from homeassistant.components.sensor import RestoreSensor, SensorEntity, SensorDeviceClass, SensorStateClass
from homeassistant.config_entries import ConfigEntry
from homeassistant.const import UnitOfPower, UnitOfTemperature, UnitOfVolume
from homeassistant.core import CALLBACK_TYPE, HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback, AddEntitiesCallback
from homeassistant.helpers.event import async_call_later, async_track_time_change, async_track_time_interval
from homeassistant.util import dt as dt_util

from . import DOMAIN
from .devices import entities_of
from .house import PHONES, TEMPERATURES, place


async def async_setup_platform(
    hass: HomeAssistant, config: dict, add: AddEntitiesCallback, discovery_info: dict | None = None
) -> None:
    if discovery_info is None:
        return
    meter = FakeMeter()
    plugs = {
        "plug": FakePlug("Fake Aquarium Leistung", "watch_fake_plug_power"),
        "fridge": FakePlug("Fake Kühlschrank Leistung", "watch_fake_fridge_power"),
        "dishwasher": FakePlug("Fake Spülmaschine Leistung", "watch_fake_dishwasher_power"),
    }
    hass.data[DOMAIN].update(meter=meter, **plugs)
    temperatures = [FakeTemperature(*row) for row in TEMPERATURES]
    hass.data[DOMAIN]["settable"].extend(temperatures)
    phones = {unique_id: FakePhone(name, unique_id) for name, unique_id in PHONES}
    hass.data[DOMAIN]["phones"] = phones
    add([meter, *plugs.values(), *temperatures, *phones.values()])



async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    """The fake devices' entities of the battery and the availability watch (devices.py)."""
    entities = entities_of("sensor")
    hass.data[DOMAIN]["devices"].extend(entities)
    add(entities)

class FakePhone(SensorEntity):
    """What reached a person's phone (notify.py): the number of messages, the last ten in `messages`."""

    _attr_should_poll = False
    _attr_native_value = 0

    def __init__(self, name: str, unique_id: str) -> None:
        self._attr_name = name
        self._attr_unique_id = unique_id
        self._messages: list[str] = []

    @property
    def extra_state_attributes(self) -> dict:
        return {"messages": list(self._messages)}

    @callback
    def receive(self, message: str) -> None:
        self._messages = [*self._messages, message][-10:]
        self._attr_native_value = int(self._attr_native_value or 0) + 1
        self.async_write_ha_state()


class FakeTemperature(RestoreSensor):
    """A room's temperature in °C, set with watch_fake.set (a number, unavailable or unknown)."""

    _attr_device_class = SensorDeviceClass.TEMPERATURE
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfTemperature.CELSIUS
    _attr_should_poll = False

    def __init__(self, name: str, unique_id: str, area: str, celsius: float) -> None:
        self._attr_name = name
        self._attr_unique_id = unique_id
        self._attr_native_value = celsius
        self._area = area

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        place(self.hass, self.entity_id, self._area)
        if (last := await self.async_get_last_sensor_data()) and last.native_value is not None:
            self._attr_native_value = float(last.native_value)

    @callback
    def set_state(self, state: str) -> None:
        self._attr_available = state != "unavailable"
        self._attr_native_value = None if state in ("unavailable", "unknown") else float(state)
        self.async_write_ha_state()


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
    """A plug's power: set by hand, following a daily routine, switching in a cycle, or running a programme."""

    _attr_device_class = SensorDeviceClass.POWER
    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_native_unit_of_measurement = UnitOfPower.WATT
    _attr_should_poll = False

    def __init__(self, name: str, unique_id: str) -> None:
        self._attr_name = name
        self._attr_unique_id = unique_id
        self._attr_native_value = 0.0
        self._unsub: CALLBACK_TYPE | None = None
        self._windows: list[tuple[int, int]] = []
        self._watts = 0.0
        self._steps: list[tuple[float, float]] = []

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
    def cycle(self, watts: float, on: float, off: float) -> None:
        """Switch on (watts) for `on` seconds and off (0) for `off` seconds, again and again, starting on."""
        self.program([(on, watts), (off, 0.0)], repeat=True)

    @callback
    def program(self, steps: list[tuple[float, float]], repeat: bool = False) -> None:
        """Run a programme: each step's watts for its seconds, then 0 W (or again from the start)."""
        self._end_routine()
        self._steps = list(steps)
        self._step(0, repeat)

    @callback
    def _step(self, index: int, repeat: bool) -> None:
        if index >= len(self._steps):
            if not repeat:
                self._unsub = None
                self._set(0.0)
                return
            index = 0
        seconds, watts = self._steps[index]
        self._set(watts)

        @callback
        def _next(_now: datetime) -> None:
            self._step(index + 1, repeat)

        self._unsub = async_call_later(self.hass, seconds, _next)

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
