"""What every fake entity shares: the id of the real device's entity, the
device, and redrawing when the device changes."""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.helpers.device_registry import DeviceInfo
from homeassistant.helpers.entity import Entity

from .const import DOMAIN
from .device import FakeDevice


class FakeEntity(Entity):
    _attr_should_poll = False

    def __init__(self, dev: FakeDevice, domain: str, suffix: str, name: str) -> None:
        self.dev = dev
        self.entity_id = f"{domain}.{dev.node}_{suffix}"
        self._attr_unique_id = f"{dev.node}_{domain}_{suffix}"
        self._attr_name = f"{dev.name} {name}"
        self._attr_device_info = DeviceInfo(identifiers={(DOMAIN, dev.node)})

    async def async_added_to_hass(self) -> None:
        self.async_on_remove(self.dev.add_listener(self.async_write_ha_state))


def present(entry: ConfigEntry, domain: str, suffixes: dict[str, Any]) -> list[tuple[FakeDevice, str, Any]]:
    """(device, suffix, spec) for each entity the real device of that kind
    has: a fake never offers what the device does not."""
    out = []
    for dev in entry.runtime_data.values():
        for suffix, spec in suffixes.items():
            if dev.has_entity(f"{domain}.{suffix}"):
                out.append((dev, suffix, spec))
    return out
