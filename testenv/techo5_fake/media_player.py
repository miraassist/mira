"""The speaker, as a TECHO5 device's (docs/spec/satellit.md, How a TECHO5
satellite behaves): it plays nothing, but reads the media's length and
reports the end of a playback when that length is over.

- Without announce a playback is background music: the wake word lowers it.
- With announce it plays in the foreground: the satellite is "responding"
  meanwhile, and the wake word does not lower it.
- Stopping or pausing leaves it "paused"; a play_media that replaces one
  still playing reports no change of state.
- Media without a length (a stream) plays until it is stopped."""

from __future__ import annotations

import asyncio
from datetime import datetime, timedelta
import logging
from typing import Any

from homeassistant.components.assist_satellite.entity import AssistSatelliteState
from homeassistant.components.media_player import MediaPlayerEntity, MediaPlayerEntityFeature, MediaPlayerState
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import CALLBACK_TYPE, HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.helpers.event import async_call_later
from homeassistant.util import dt as dt_util

from .entity import FakeEntity, present

_LOGGER = logging.getLogger(__name__)

# What the real speaker reports (TECHO5).
FEATURES = MediaPlayerEntityFeature(1201677)
# The device lowers background sound by 15 dB while it listens.
DUCK = 10 ** (-15 / 20)
# How long ffprobe may take to find a length; a stream gives none.
PROBE_TIMEOUT = 10


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeSpeaker(dev) for dev, _, _ in present(entry, "media_player", {"speaker": None}))


async def media_length(url: str) -> float | None:
    """Seconds of the media at url (a URL or a file), None when it has no
    length or cannot be read. Runs ffprobe as a process, off the event loop."""
    try:
        proc = await asyncio.create_subprocess_exec(
            "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", url,
            stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL,
        )
    except OSError as err:
        _LOGGER.warning("ffprobe not found: %s", err)
        return None
    try:
        out, _ = await asyncio.wait_for(proc.communicate(), PROBE_TIMEOUT)
    except TimeoutError:
        proc.kill()
        return None
    try:
        length = float(out.decode().strip())
    except ValueError:
        return None
    return length if length > 0 else None


class FakeSpeaker(FakeEntity, MediaPlayerEntity):
    _attr_supported_features = FEATURES

    def __init__(self, dev) -> None:
        super().__init__(dev, "media_player", "speaker", "Lautsprecher")
        self._attr_state = MediaPlayerState.IDLE
        self._volume = 0.5
        self._attr_is_volume_muted = False
        self._attr_media_content_id = None
        self._attr_media_title = None
        self.announcing = False
        self.ducked = False
        self._playing = 0  # which playback is current; a replaced one ends nothing
        self._remaining: float | None = None
        self._ends_at: datetime | None = None
        self._end: CALLBACK_TYPE | None = None

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        self.dev.speaker = self

    async def async_will_remove_from_hass(self) -> None:
        self._cancel_end()
        self.dev.speaker = None

    @property
    def volume_level(self) -> float:
        return self._volume * DUCK if self.ducked else self._volume

    async def async_play_media(self, media_type: str, media_id: str, **kwargs: Any) -> None:
        announce = bool(kwargs.get("announce"))
        self.dev.record("play_media", media_type=str(media_type), media_id=media_id, announce=announce)
        self._cancel_end()
        self._playing += 1
        playing = self._playing
        title, self.dev.media_title = self.dev.media_title, None
        self._attr_media_content_id = media_id
        self._attr_media_content_type = media_type
        self._attr_media_title = title["title"] if title else None
        self._attr_state = MediaPlayerState.PLAYING
        self._set_announcing(announce)
        self.async_write_ha_state()

        length = await media_length(await self._url(media_id))
        if playing == self._playing and self._attr_state is MediaPlayerState.PLAYING and length:
            self._run(length)

    async def async_media_stop(self) -> None:
        self.dev.record("media_stop")
        self._pause()

    async def async_media_pause(self) -> None:
        self.dev.record("media_pause")
        self._pause()

    async def async_media_play(self) -> None:
        self.dev.record("media_play")
        if self._attr_state is MediaPlayerState.PAUSED and self._attr_media_content_id:
            self._attr_state = MediaPlayerState.PLAYING
            self.async_write_ha_state()
            if self._remaining:
                self._run(self._remaining)

    async def async_set_volume_level(self, volume: float) -> None:
        self.dev.record("volume_set", volume=volume)
        self._volume = volume
        self.async_write_ha_state()

    async def async_mute_volume(self, mute: bool) -> None:
        self.dev.record("mute", mute=mute)
        self._attr_is_volume_muted = mute
        self.async_write_ha_state()

    @callback
    def duck(self, on: bool) -> None:
        """The satellite listens (on) or stopped listening: background music
        is lowered meanwhile, a foreground playback is not."""
        on = on and self._attr_state is MediaPlayerState.PLAYING and not self.announcing
        if on != self.ducked:
            self.ducked = on
            self.async_write_ha_state()

    async def _url(self, media_id: str) -> str:
        """Where the media is, as the device would fetch it."""
        from homeassistant.components import media_source
        from homeassistant.components.media_player import async_process_play_media_url

        if media_source.is_media_source_id(media_id):
            media_id = (await media_source.async_resolve_media(self.hass, media_id, self.entity_id)).url
        if media_id.startswith("/"):
            try:
                return async_process_play_media_url(self.hass, media_id)
            except Exception:  # noqa: BLE001 - a file path in a test
                return media_id
        return media_id

    @callback
    def _run(self, seconds: float) -> None:
        self._remaining = seconds
        self._ends_at = dt_util.utcnow() + timedelta(seconds=seconds)
        self._end = async_call_later(self.hass, seconds, self._ended)

    @callback
    def _cancel_end(self) -> None:
        if self._end:
            self._end()
            self._end = None

    @callback
    def _ended(self, _now: datetime) -> None:
        self._end = None
        self._remaining = None
        self._attr_state = MediaPlayerState.IDLE
        self._attr_media_content_id = None
        self._attr_media_title = None
        self.ducked = False
        self._set_announcing(False)
        self.async_write_ha_state()

    @callback
    def _pause(self) -> None:
        if self._end and self._ends_at:
            self._remaining = max((self._ends_at - dt_util.utcnow()).total_seconds(), 0)
        self._cancel_end()
        if self._attr_state is MediaPlayerState.PLAYING:
            self._attr_state = MediaPlayerState.PAUSED
        self.ducked = False
        self._set_announcing(False)
        self.async_write_ha_state()

    @callback
    def _set_announcing(self, on: bool) -> None:
        """A foreground playback keeps the satellite "responding"."""
        if on == self.announcing:
            return
        self.announcing = on
        if self.dev.satellite:
            self.dev.satellite.set_state(AssistSatelliteState.RESPONDING if on else AssistSatelliteState.IDLE)
