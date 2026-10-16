"""Polls the agent's status, and keeps the agent's managed config in sync."""

from __future__ import annotations

from datetime import timedelta
import logging
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import MiraClient, MiraError, MiraInvalidConfig
from .const import CONF_IMPORTED, DOMAIN
from .managed import build_config

_LOGGER = logging.getLogger(__name__)


class MiraCoordinator(DataUpdateCoordinator[dict[str, Any]]):
    """Reads /v1/status every minute.

    After every setup (a subentry change reloads the entry) it also sends the
    subentries to the agent, until that worked once: an agent that was down
    or restarted with an empty database gets them on the next poll.
    """

    def __init__(self, hass: HomeAssistant, entry: ConfigEntry, client: MiraClient) -> None:
        super().__init__(
            hass, _LOGGER, config_entry=entry, name=DOMAIN, update_interval=timedelta(seconds=60)
        )
        self.client = client
        self._synced = False

    async def _async_update_data(self) -> dict[str, Any]:
        try:
            status = await self.client.status()
        except MiraError as err:
            raise UpdateFailed(str(err)) from err
        await self._sync()
        return status

    async def _sync(self) -> None:
        entry = self.config_entry
        if self._synced or not entry.data.get(CONF_IMPORTED):
            return
        try:
            await self.client.set_managed_config(build_config(self.hass, entry))
        except MiraInvalidConfig as err:
            # Retrying cannot fix it; the agent keeps its last valid config.
            _LOGGER.error("The agent refused watchers, persons and rooms from Home Assistant: %s", err)
        except MiraError as err:
            _LOGGER.debug("Sending watchers, persons and rooms waits for the agent: %s", err)
            return
        self._synced = True
