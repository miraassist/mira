"""Shared base for the entities of one agent."""

from __future__ import annotations

from homeassistant.helpers.device_registry import DeviceEntryType, DeviceInfo
from homeassistant.helpers.update_coordinator import CoordinatorEntity

from .const import DOMAIN
from .coordinator import MiraCoordinator


def device_info(entry_id: str) -> DeviceInfo:
    """One service device per agent, so all entities group together."""
    return DeviceInfo(
        identifiers={(DOMAIN, entry_id)}, name="Mira", manufacturer="Mira", entry_type=DeviceEntryType.SERVICE
    )


class MiraEntity(CoordinatorEntity[MiraCoordinator]):
    """An entity fed by the status coordinator."""

    _attr_has_entity_name = True

    def __init__(self, coordinator: MiraCoordinator, key: str) -> None:
        super().__init__(coordinator)
        entry_id = coordinator.config_entry.entry_id
        self._attr_unique_id = f"{entry_id}_{key}"
        self._attr_translation_key = key
        self._attr_device_info = device_info(entry_id)
