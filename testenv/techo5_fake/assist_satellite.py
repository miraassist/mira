"""The voice satellite. It speaks nothing: an announcement or the start of a
conversation is logged with its text, and the satellite looks busy for a
moment, like a device that speaks."""

from __future__ import annotations

import asyncio
from typing import Any

from homeassistant.components.assist_satellite import (
    AssistSatelliteConfiguration,
    AssistSatelliteEntity,
    AssistSatelliteEntityFeature,
)
from homeassistant.components.assist_satellite.entity import AssistSatelliteState
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .entity import FakeEntity, present

SPEAKING = 1.5  # seconds a fake "speaks"


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeSatellite(dev) for dev, _, _ in present(entry, "assist_satellite", {"assist_satellit": None}))


class FakeSatellite(FakeEntity, AssistSatelliteEntity):
    _attr_supported_features = AssistSatelliteEntityFeature.ANNOUNCE | AssistSatelliteEntityFeature.START_CONVERSATION

    def __init__(self, dev) -> None:
        super().__init__(dev, "assist_satellite", "assist_satellit", "Assist-Satellit")

    def async_get_configuration(self) -> AssistSatelliteConfiguration:
        return AssistSatelliteConfiguration(available_wake_words=[], active_wake_words=[], max_active_wake_words=1)

    async def async_set_configuration(self, config: AssistSatelliteConfiguration) -> None:
        return None

    def on_pipeline_event(self, event: Any) -> None:
        return None

    async def _speak(self, kind: str, message: str | None, media_id: str | None, **extra: Any) -> None:
        self.dev.record(kind, message=message or "", media_id=media_id or "", **extra)
        self._set_state(AssistSatelliteState.RESPONDING)
        try:
            await asyncio.sleep(SPEAKING)
        finally:
            self._set_state(AssistSatelliteState.IDLE)

    # No speech synthesis: the text is what a test checks, and a fake must
    # not make the test house pay for a cloud voice.
    async def async_internal_announce(self, message: str | None = None, media_id: str | None = None,
                                      preannounce: bool = True, **_: Any) -> None:
        await self._speak("announce", message, media_id, preannounce=preannounce)

    async def async_internal_start_conversation(self, start_message: str | None = None,
                                                start_media_id: str | None = None,
                                                extra_system_prompt: str | None = None,
                                                preannounce: bool = True, **_: Any) -> None:
        await self._speak("start_conversation", start_message, start_media_id,
                          extra_system_prompt=extra_system_prompt or "", preannounce=preannounce)
