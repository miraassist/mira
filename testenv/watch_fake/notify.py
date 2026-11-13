"""The fake phones of the persons (see house.py): a legacy notify service per phone (notify.fake_handy_anna),
as a companion app's, which keeps what it was sent in its phone's sensor (sensor.fake_handy_anna: the number
of messages, the last ones newest last in its attribute `messages`), for a test to read what reached a person
privately. Home Assistant's notify entities cannot be set up without a config entry; the legacy service can."""

from __future__ import annotations

from typing import Any

from homeassistant.components.notify import BaseNotificationService
from homeassistant.core import HomeAssistant

from . import DOMAIN


async def async_get_service(
    hass: HomeAssistant, config: dict, discovery_info: dict | None = None
) -> BaseNotificationService | None:
    if discovery_info is None:
        return None
    return FakePhoneService(hass, discovery_info["phone"])


class FakePhoneService(BaseNotificationService):
    def __init__(self, hass: HomeAssistant, phone: str) -> None:
        self.hass = hass
        self._phone = phone

    async def async_send_message(self, message: str = "", **kwargs: Any) -> None:
        self.hass.data[DOMAIN]["phones"][self._phone].receive(message)
