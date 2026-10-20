"""The speaker: plays what it is given (logged), stops, keeps its volume."""

from __future__ import annotations

from typing import Any

from homeassistant.components.media_player import MediaPlayerEntity, MediaPlayerEntityFeature, MediaPlayerState
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback

from .entity import FakeEntity, present

# What the real speaker reports (TECHO5).
FEATURES = MediaPlayerEntityFeature(1201677)


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeSpeaker(dev) for dev, _, _ in present(entry, "media_player", {"speaker": None}))


class FakeSpeaker(FakeEntity, MediaPlayerEntity):
    _attr_supported_features = FEATURES

    def __init__(self, dev) -> None:
        super().__init__(dev, "media_player", "speaker", "Lautsprecher")
        self._attr_state = MediaPlayerState.IDLE
        self._attr_volume_level = 0.5
        self._attr_is_volume_muted = False
        self._attr_media_content_id = None

    async def async_play_media(self, media_type: str, media_id: str, **kwargs: Any) -> None:
        self.dev.record("play_media", media_type=str(media_type), media_id=media_id)
        self._attr_media_content_id = media_id
        self._attr_media_content_type = media_type
        self._attr_state = MediaPlayerState.PLAYING
        self.async_write_ha_state()

    async def async_media_stop(self) -> None:
        self.dev.record("media_stop")
        self._attr_state = MediaPlayerState.IDLE
        self._attr_media_content_id = None
        self.async_write_ha_state()

    async def async_media_pause(self) -> None:
        self.dev.record("media_pause")
        if self._attr_state is MediaPlayerState.PLAYING:
            self._attr_state = MediaPlayerState.PAUSED
        self.async_write_ha_state()

    async def async_media_play(self) -> None:
        self.dev.record("media_play")
        if self._attr_media_content_id:
            self._attr_state = MediaPlayerState.PLAYING
        self.async_write_ha_state()

    async def async_set_volume_level(self, volume: float) -> None:
        self.dev.record("volume_set", volume=volume)
        self._attr_volume_level = volume
        self.async_write_ha_state()

    async def async_mute_volume(self, mute: bool) -> None:
        self.dev.record("mute", mute=mute)
        self._attr_is_volume_muted = mute
        self.async_write_ha_state()
