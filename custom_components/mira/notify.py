"""notify.mira_*: send a message through Mira with Home Assistant's own
notify.send_message (docs/spec/nachrichten.md). One entity for the house,
one per person; the sender is the automation (or whoever calls it).
Mira then delivers it like any message: in the house by announcement or
the indicator light, to a person over their channels."""

from __future__ import annotations

from homeassistant.components.notify import NotifyEntity, NotifyEntityFeature
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import MiraConfigEntry
from .api import MiraError
from .entity import device_info
from .managed import all_persons, entity_name, person_key

HOUSE = "house"


async def async_setup_entry(
    hass: HomeAssistant, entry: MiraConfigEntry, async_add_entities: AddConfigEntryEntitiesCallback
) -> None:
    entities = [MiraNotify(entry, HOUSE, None)]
    for person in all_persons(hass):
        entities.append(MiraNotify(entry, person_key(hass, person), entity_name(hass, person)))
    async_add_entities(entities)


class MiraNotify(NotifyEntity):
    """A message to the house or to one person, through Mira."""

    _attr_has_entity_name = True
    _attr_supported_features = NotifyEntityFeature.TITLE

    def __init__(self, entry: MiraConfigEntry, receiver: str, name: str | None) -> None:
        self._entry = entry
        self._receiver = receiver
        self._attr_unique_id = f"{entry.entry_id}_notify_{receiver}"
        self._attr_device_info = device_info(entry.entry_id)
        if name is None:
            self._attr_translation_key = "notify_house"
        else:
            self._attr_name = name

    async def async_send_message(self, message: str, title: str | None = None) -> None:
        text = f"{title}: {message}" if title else message
        try:
            await self._entry.runtime_data.client.send_message({"text": text, "receiver": self._receiver})
        except MiraError as err:
            raise HomeAssistantError(f"Mira did not take the message: {err}") from err
        await self._entry.runtime_data.coordinator.async_request_refresh()
