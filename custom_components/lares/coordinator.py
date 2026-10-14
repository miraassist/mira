"""Polls the agent's status for the sensor and the switch."""

from __future__ import annotations

from datetime import timedelta
import logging
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import LaresClient, LaresError
from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)


class LaresCoordinator(DataUpdateCoordinator[dict[str, Any]]):
    """Reads /v1/status every minute."""

    def __init__(self, hass: HomeAssistant, entry: ConfigEntry, client: LaresClient) -> None:
        super().__init__(
            hass, _LOGGER, config_entry=entry, name=DOMAIN, update_interval=timedelta(seconds=60)
        )
        self.client = client

    async def _async_update_data(self) -> dict[str, Any]:
        try:
            return await self.client.status()
        except LaresError as err:
            raise UpdateFailed(str(err)) from err
