"""button.mira_compact_profile: rebuild the memory profile now."""

from __future__ import annotations

from homeassistant.components.button import ButtonEntity
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import MiraConfigEntry
from .api import MiraError
from .entity import MiraEntity


async def async_setup_entry(
    hass: HomeAssistant, entry: MiraConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    async_add_entities([CompactProfileButton(entry.runtime_data.coordinator, "compact_profile")])


class CompactProfileButton(MiraEntity, ButtonEntity):
    """Runs the nightly profile compaction now."""

    _attr_icon = "mdi:brain"
    _attr_entity_category = EntityCategory.CONFIG

    async def async_press(self) -> None:
        try:
            await self.coordinator.client.compact_profile()
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent could not compact the profile: {err}") from err
