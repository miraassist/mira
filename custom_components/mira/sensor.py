"""sensor.mira_pending_messages: how many messages wait for delivery."""

from __future__ import annotations

from homeassistant.components.sensor import SensorEntity, SensorStateClass
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import MiraConfigEntry
from .entity import MiraEntity


async def async_setup_entry(
    hass: HomeAssistant, entry: MiraConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    async_add_entities([PendingMessagesSensor(entry.runtime_data.coordinator, "pending_messages")])


class PendingMessagesSensor(MiraEntity, SensorEntity):
    """Number of pending messages."""

    _attr_state_class = SensorStateClass.MEASUREMENT
    _attr_icon = "mdi:bell-badge-outline"

    @property
    def native_value(self) -> int | None:
        return (self.coordinator.data or {}).get("pending_messages")
