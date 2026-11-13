"""The fake devices of the battery and the availability watch (docs/spec/fake-devices.md, "The watched group").

Unlike the rest of the fake house these entities belong to devices (Home Assistant's device registry), as a
battery or a lost device is told by its device: they come with the config entry "Fake Geräte", which watch_fake
makes for itself at its setup. Each device is in an area. `watch_fake.set` sets one of their entities (a battery's
percentage, low or not, the plug on or off, unavailable); `watch_fake.reachable` loses a whole device (every entity
unavailable) or brings it back. A device that was lost stays lost over a restart of Home Assistant, as a device
whose battery is gone would.
"""

from __future__ import annotations

from homeassistant.components.binary_sensor import BinarySensorDeviceClass, BinarySensorEntity
from homeassistant.components.sensor import SensorDeviceClass, SensorEntity, SensorStateClass
from homeassistant.components.switch import SwitchEntity
from homeassistant.const import PERCENTAGE, SIGNAL_STRENGTH_DECIBELS_MILLIWATT
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.entity import Entity
from homeassistant.helpers.restore_state import ExtraStoredData, RestoredExtraData, RestoreEntity
from homeassistant.util import slugify

DOMAIN = "watch_fake"

# (device name, key, area, [(domain, kind, initial value)]); kind is the device class and the entity's name.
DEVICES = [
    ("Fake Rauchmelder Flur", "smoke_hall", "Flur", [("sensor", "battery", 90)]),
    ("Fake Wassermelder Keller", "leak_cellar", "Keller", [("binary_sensor", "battery", False), ("binary_sensor", "moisture", False)]),
    ("Fake Klima Garten", "climate_garden", "Garten", [("sensor", "battery", 70), ("sensor", "humidity", 60)]),
    ("Fake Steckdose Garage", "plug_garage", "Garage", [("switch", "outlet", False), ("sensor", "signal_strength", -60)]),
]
# The entities' names in the house's language (the test HA speaks German).
NAMES = {"battery": "Batterie", "moisture": "Wasser", "humidity": "Luftfeuchtigkeit", "signal_strength": "Signal", "outlet": None}


def entities_of(domain: str) -> list[Entity]:
    """The fake devices' entities of one domain."""
    out: list[Entity] = []
    for device, key, area, entities in DEVICES:
        for d, kind, value in entities:
            if d != domain:
                continue
            cls = {"sensor": FakeDeviceSensor, "binary_sensor": FakeDeviceBinary, "switch": FakeDevicePlug}[d]
            out.append(cls(device, key, area, kind, value))
    return out


def place_device(hass: HomeAssistant, device_id: str | None, area_name: str) -> None:
    """Put a device into its area unless it has one (the user may move it in the test HA)."""
    if device_id is None:
        return
    devices = dr.async_get(hass)
    device = devices.async_get(device_id)
    area = ar.async_get(hass).async_get_area_by_name(area_name)
    if device is not None and device.area_id is None and area is not None:
        devices.async_update_device(device_id, area_id=area.id)


class FakeDeviceEntity(RestoreEntity):
    """An entity of a fake device: settable, and lost with its device."""

    _attr_has_entity_name = True
    _attr_should_poll = False
    platform_domain = ""

    def __init__(self, device: str, key: str, area: str, kind: str, value) -> None:
        self._attr_unique_id = f"watch_fake_{key}_{kind}"
        self._attr_name = NAMES[kind]
        # Fixed ids, whichever area the device is in when its entities are registered.
        slug = slugify(f"{device} {NAMES[kind] or ''}")
        self.entity_id = f"{self.platform_domain}.{slug}"
        self._attr_device_info = DeviceInfo(identifiers={(DOMAIN, key)}, name=device, manufacturer="Fake Maker")
        self.key = key
        self._area = area
        self._value = value

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        if self.registry_entry is not None:
            place_device(self.hass, self.registry_entry.device_id, self._area)
        if (last := await self.async_get_last_state()) is not None and last.state not in ("unavailable", "unknown"):
            self._value = self._restore(last.state)
        # Home Assistant keeps no "unavailable" to restore: whether the device was lost is kept apart.
        if (extra := await self.async_get_last_extra_data()) is not None:
            self._attr_available = extra.as_dict().get("available", True)

    @property
    def extra_restore_state_data(self) -> ExtraStoredData:
        return RestoredExtraData({"available": self.available})

    def _restore(self, state: str):
        return state

    @callback
    def set_state(self, state: str) -> None:
        self._attr_available = state != "unavailable"
        if state != "unavailable":
            self._value = None if state == "unknown" else self._parse(state)
        self.async_write_ha_state()

    @callback
    def set_reachable(self, reachable: bool) -> None:
        self._attr_available = reachable
        self.async_write_ha_state()

    def _parse(self, state: str):
        return state


class FakeDeviceSensor(FakeDeviceEntity, SensorEntity):
    """A battery's percentage, a humidity or a signal."""

    platform_domain = "sensor"

    def __init__(self, device: str, key: str, area: str, kind: str, value) -> None:
        super().__init__(device, key, area, kind, value)
        self._attr_device_class = SensorDeviceClass(kind)
        self._attr_state_class = SensorStateClass.MEASUREMENT
        self._attr_native_unit_of_measurement = SIGNAL_STRENGTH_DECIBELS_MILLIWATT if kind == "signal_strength" else PERCENTAGE

    @property
    def native_value(self):
        return self._value

    def _parse(self, state: str):
        return float(state)

    def _restore(self, state: str):
        try:
            return float(state)
        except ValueError:
            return self._value


class FakeDeviceBinary(FakeDeviceEntity, BinarySensorEntity):
    """A battery reporting low (on), or a leak sensor."""

    platform_domain = "binary_sensor"

    def __init__(self, device: str, key: str, area: str, kind: str, value) -> None:
        super().__init__(device, key, area, kind, value)
        self._attr_device_class = BinarySensorDeviceClass(kind)

    @property
    def is_on(self):
        return self._value

    def _parse(self, state: str):
        return state == "on"

    def _restore(self, state: str):
        return state == "on"


class FakeDevicePlug(FakeDeviceEntity, SwitchEntity):
    """A plug, switched by the test or by Home Assistant."""

    platform_domain = "switch"

    @property
    def is_on(self):
        return self._value

    def _parse(self, state: str):
        return state == "on"

    def _restore(self, state: str):
        return state == "on"

    async def async_turn_on(self, **kwargs) -> None:
        self._value = True
        self.async_write_ha_state()

    async def async_turn_off(self, **kwargs) -> None:
        self._value = False
        self.async_write_ha_state()
