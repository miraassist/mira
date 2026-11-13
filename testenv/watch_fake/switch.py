"""The fake plug of the availability watch (devices.py)."""

from __future__ import annotations

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from . import DOMAIN
from .devices import entities_of


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    entities = entities_of("switch")
    hass.data[DOMAIN]["devices"].extend(entities)
    add(entities)
