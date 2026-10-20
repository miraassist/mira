"""The timers counting down, the next alarm, and the fake's own log (only on
the fakes: what the device was told, for tests)."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from homeassistant.components.sensor import SensorDeviceClass, SensorEntity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.util import dt as dt_util

from .entity import FakeEntity, present


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    entities: list[SensorEntity] = []
    for dev, suffix, _ in present(entry, "sensor", {"timers": None, "next_alarm": None}):
        entities.append(FakeTimers(dev) if suffix == "timers" else FakeNextAlarm(dev))
    entities += [FakeLog(dev) for dev in entry.runtime_data.values()]
    add(entities)


class FakeTimers(FakeEntity, SensorEntity):
    def __init__(self, dev) -> None:
        super().__init__(dev, "sensor", "timers", "Timers")

    @property
    def native_value(self) -> str | None:
        parts = []
        for t in self.dev.listing()["timers"]:
            label = t["label"] or "Timer"
            if t["running"]:
                ends = dt_util.now().timestamp() + t["left_seconds"]
                parts.append(f"{label} at {dt_util.as_local(dt_util.utc_from_timestamp(ends)).strftime('%H:%M')}")
            else:
                m, s = divmod(t["left_seconds"], 60)
                parts.append(f"{label} paused ({m}:{s:02d} left)")
        return ", ".join(parts)[:255] or None


class FakeNextAlarm(FakeEntity, SensorEntity):
    _attr_device_class = SensorDeviceClass.TIMESTAMP

    def __init__(self, dev) -> None:
        super().__init__(dev, "sensor", "next_alarm", "Next alarm")

    @property
    def native_value(self) -> datetime | None:
        return self.dev.next_alarm()


class FakeLog(FakeEntity, SensorEntity):
    """Not on a real device: the number of entries, and the last ones as
    attribute "log" (each with at, kind and what was given)."""

    def __init__(self, dev) -> None:
        super().__init__(dev, "sensor", "fake_log", "Fake log")

    @property
    def native_value(self) -> int:
        return len(self.dev.log)

    @property
    def extra_state_attributes(self) -> dict[str, Any]:
        return {"log": self.dev.log}
