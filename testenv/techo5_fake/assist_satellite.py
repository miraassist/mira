"""The voice satellite, as a TECHO5 device's (docs/spec/satellit.md, How a
TECHO5 satellite behaves). It speaks nothing: an announcement or the start
of a conversation is logged with its text, and the satellite reports the
states a device reports.

- A plain announcement: "responding", then "idle"; the call returns when
  it is over.
- Starting a conversation: "responding", "idle" for a moment, "listening",
  and "idle" when the listening time is over.
- The wake word (the action techo5_fake.wake_word): "listening", music on
  the speaker lowered meanwhile; "idle" again when the listening time is
  over.
- A voice turn (the action techo5_fake.say): "listening", "processing", the
  text to the preferred pipeline's agent, "responding" while the answer is
  spoken, then "listening" if the answer asks for it, else "idle".

Speaking takes as long as the text, about 15 characters a second."""

from __future__ import annotations

import asyncio
from datetime import datetime
from typing import Any

from homeassistant.components.assist_satellite import (
    AssistSatelliteConfiguration,
    AssistSatelliteEntity,
    AssistSatelliteEntityFeature,
)
from homeassistant.components.assist_satellite.entity import AssistSatelliteState
from homeassistant.config_entries import ConfigEntry
from homeassistant.components import conversation
from homeassistant.components.assist_pipeline import async_get_pipeline
from homeassistant.core import CALLBACK_TYPE, Context, HomeAssistant, callback
from homeassistant.helpers.entity_platform import AddConfigEntryEntitiesCallback
from homeassistant.helpers.event import async_call_later

from .entity import FakeEntity, present

CHARS_PER_SECOND = 15  # how fast a fake "speaks", like a voice
LISTENING = 8  # seconds a fake listens without hearing anything


def speaking_time(text: str) -> float:
    """Seconds a fake takes to say text: longer texts take longer."""
    return max(1.0, len(text) / CHARS_PER_SECOND)


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry, add: AddConfigEntryEntitiesCallback) -> None:
    add(FakeSatellite(dev) for dev, _, _ in present(entry, "assist_satellite", {"assist_satellit": None}))


class FakeSatellite(FakeEntity, AssistSatelliteEntity):
    _attr_supported_features = AssistSatelliteEntityFeature.ANNOUNCE | AssistSatelliteEntityFeature.START_CONVERSATION

    def __init__(self, dev) -> None:
        super().__init__(dev, "assist_satellite", "assist_satellit", "Assist-Satellit")
        self._listen_end: CALLBACK_TYPE | None = None
        # The conversation a follow-up continues: set by an answer that
        # listens on, until the listening is over.
        self._follow_up: str | None = None

    async def async_added_to_hass(self) -> None:
        await super().async_added_to_hass()
        self.dev.satellite = self

    async def async_will_remove_from_hass(self) -> None:
        self._stop_listening()
        self.dev.satellite = None

    def async_get_configuration(self) -> AssistSatelliteConfiguration:
        return AssistSatelliteConfiguration(available_wake_words=[], active_wake_words=[], max_active_wake_words=1)

    async def async_set_configuration(self, config: AssistSatelliteConfiguration) -> None:
        return None

    def on_pipeline_event(self, event: Any) -> None:
        return None

    @callback
    def set_state(self, state: AssistSatelliteState) -> None:
        self._set_state(state)

    @callback
    def wake_word(self) -> None:
        """The wake word was heard: listen, music lowered meanwhile."""
        self.dev.record("wake_word")
        self._listen()

    async def say(self, text: str) -> dict[str, Any]:
        """A voice turn: someone said text after the wake word."""
        self.dev.record("say", text=text)
        conversation_id = self._follow_up if self._listen_end else None
        self._stop_listening()
        if self.dev.speaker:
            self.dev.speaker.duck(True)
        self._set_state(AssistSatelliteState.LISTENING)
        self._set_state(AssistSatelliteState.PROCESSING)
        pipeline = async_get_pipeline(self.hass, self._resolve_pipeline())
        result = await conversation.async_converse(
            self.hass, text, conversation_id, Context(), language=pipeline.conversation_language,
            agent_id=pipeline.conversation_engine, device_id=self.dev.device_id, satellite_id=self.entity_id,
        )
        speech = result.response.speech.get("plain", {}).get("speech", "")
        await self._responding(speech)
        if result.continue_conversation:
            self._follow_up = result.conversation_id
            self._listen()
        else:
            self._quiet()
        return {"speech": speech, "continue_conversation": result.continue_conversation}

    async def _speak(self, kind: str, message: str | None, media_id: str | None, **extra: Any) -> None:
        self.dev.record(kind, message=message or "", media_id=media_id or "", **extra)
        self._stop_listening()
        await self._responding(message or "")
        self._set_state(AssistSatelliteState.IDLE)

    async def _responding(self, text: str) -> None:
        """Responding for as long as text takes to say."""
        self._set_state(AssistSatelliteState.RESPONDING)
        done = asyncio.Event()

        @callback
        def spoken(_now: datetime) -> None:
            done.set()

        cancel = async_call_later(self.hass, speaking_time(text), spoken)
        try:
            await done.wait()
        finally:
            cancel()

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
        # Idle for a moment, then listening: that idle is no end of the
        # conversation.
        self._listen()

    @callback
    def _listen(self) -> None:
        self._stop_listening()
        if self.dev.speaker:
            self.dev.speaker.duck(True)
        self._set_state(AssistSatelliteState.LISTENING)
        self._listen_end = async_call_later(self.hass, LISTENING, self._heard_nothing)

    @callback
    def _heard_nothing(self, _now: datetime) -> None:
        self._listen_end = None
        self._quiet()

    @callback
    def _quiet(self) -> None:
        self._follow_up = None
        self._set_state(AssistSatelliteState.IDLE)
        if self.dev.speaker:
            self.dev.speaker.duck(False)

    @callback
    def _stop_listening(self) -> None:
        if self._listen_end:
            self._listen_end()
            self._listen_end = None
