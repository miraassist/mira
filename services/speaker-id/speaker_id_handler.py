"""Wyoming event handler: passes the audio through to the real STT and recognizes the speaker beside it."""

import asyncio
import logging
import time

import numpy as np
from wyoming.asr import Transcribe, Transcript
from wyoming.audio import AudioChunk, AudioStart, AudioStop
from wyoming.client import AsyncClient
from wyoming.event import Event
from wyoming.info import Describe, Info
from wyoming.server import AsyncEventHandler

from speaker_store import SpeakerStore

_LOGGER = logging.getLogger(__name__)

# How long a transcript may wait for its recognition before it goes to Home Assistant without it.
RECOGNITION_WAIT = 1.0


class SpeakerIdEventHandler(AsyncEventHandler):
    """One Home Assistant connection. The transcript is the upstream's, unchanged and not delayed."""

    def __init__(self, reader, writer, wyoming_info: Info, upstream: str, store: SpeakerStore) -> None:
        super().__init__(reader, writer)
        self.wyoming_info = wyoming_info
        self.upstream_uri = upstream
        self.store = store
        self.client: AsyncClient | None = None
        self.audio = bytearray()
        self.rate, self.width, self.channels = 16000, 2, 1

    async def _upstream(self) -> AsyncClient:
        if self.client is None:
            self.client = AsyncClient.from_uri(self.upstream_uri)
            await self.client.connect()
        return self.client

    async def _close_upstream(self) -> None:
        if self.client is not None:
            try:
                await self.client.disconnect()
            except Exception:  # noqa: BLE001 - the connection is done with either way
                pass
            self.client = None

    async def disconnect(self) -> None:
        await self._close_upstream()

    async def handle_event(self, event: Event) -> bool:
        if Describe.is_type(event.type):
            await self.write_event(self.wyoming_info.event())
            return True

        if Transcribe.is_type(event.type):
            self.audio = bytearray()
            await (await self._upstream()).write_event(event)
            return True

        if AudioStart.is_type(event.type):
            start = AudioStart.from_event(event)
            self.rate, self.width, self.channels = start.rate, start.width, start.channels
            await (await self._upstream()).write_event(event)
            return True

        if AudioChunk.is_type(event.type):
            chunk = AudioChunk.from_event(event)
            if not self.audio:
                self.rate, self.width, self.channels = chunk.rate, chunk.width, chunk.channels
            self.audio.extend(chunk.audio)
            await (await self._upstream()).write_event(event)
            return True

        if AudioStop.is_type(event.type):
            client = await self._upstream()
            await client.write_event(event)
            started = time.perf_counter()

            loop = asyncio.get_running_loop()
            recognition = loop.run_in_executor(None, self._recognize, bytes(self.audio))

            # Everything the upstream says goes back as it comes, up to the transcript. The transcript
            # waits for the recognition, which is nearly always done by then (it is ten times faster
            # than the STT): the conversation agent looks the speaker up by this text the moment Home
            # Assistant has it, so the utterance has to carry it first (docs/spec/sprecher.md).
            transcript_event, transcript = Transcript(text="").event(), ""
            while True:
                reply = await client.read_event()
                if reply is None:
                    _LOGGER.warning("Upstream closed before a transcript")
                    break
                if Transcript.is_type(reply.type):
                    transcript_event, transcript = reply, Transcript.from_event(reply).text
                    break
                await self.write_event(reply)
            stt_ms = (time.perf_counter() - started) * 1000
            await self._close_upstream()

            u = None
            try:
                u = await asyncio.wait_for(recognition, timeout=RECOGNITION_WAIT)
                u.transcript = transcript
            except Exception as err:  # noqa: BLE001 - recognition never breaks the transcript
                _LOGGER.error("Recognition failed or late: %r", err)
            await self.write_event(transcript_event)

            if u is not None:
                _LOGGER.info(
                    "speaker=%s decision=%s score=%.3f second=%s/%.3f seconds=%.1f embed_ms=%.0f stt_ms=%.0f text=%r",
                    u.speaker, u.decision, u.score, u.second, u.second_score, u.seconds,
                    u.elapsed_ms, stt_ms, transcript,
                )
            return True

        # Anything else (a client asking for streaming, say) is the upstream's business.
        if self.client is not None:
            await self.client.write_event(event)
        return True

    def _recognize(self, raw: bytes):
        if self.width != 2:
            raise ValueError(f"unsupported sample width {self.width}")
        audio = np.frombuffer(raw, dtype=np.int16).astype(np.float32) / 32768.0
        if self.channels > 1:
            audio = audio.reshape(-1, self.channels).mean(axis=1)
        return self.store.recognize(audio, self.rate)
