"""The fake TECHO5 devices behave like the real ones (docs/spec/fake-geraete.md,
scenarios FG-nn): the states of satellite and speaker, at the same
transitions as docs/spec/satellit.md describes."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime
from pathlib import Path
import time

import pytest

import custom_components
from homeassistant.core import Event, HomeAssistant, callback
from homeassistant.helpers.event import async_track_state_change_event
from homeassistant.setup import async_setup_component
from pytest_homeassistant_custom_component.common import MockConfigEntry, async_fire_time_changed

REPO = Path(__file__).resolve().parents[2]
FAKE = REPO / "testenv" / "techo5_fake"
# As a file: URL, so Home Assistant leaves it as it is (a path would become its own URL).
TWO_SECONDS = (REPO / "testenv" / "ha" / "www" / "fake" / "two-seconds.wav").as_uri()

SAT = "assist_satellite.fake_show_assist_satellit"
SPK = "media_player.fake_show_speaker"


@pytest.fixture
async def fakes(hass: HomeAssistant, tmp_path, monkeypatch):
    """The fake integration loaded from testenv, as the test HA mounts it."""
    (tmp_path / "techo5_fake").symlink_to(FAKE)
    monkeypatch.setattr(custom_components, "__path__", [*custom_components.__path__, str(tmp_path)])
    hass.data.pop("custom_components", None)
    assert await async_setup_component(hass, "conversation", {})
    entry = MockConfigEntry(domain="techo5_fake", data={})
    entry.add_to_hass(hass)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    return entry


def states(hass: HomeAssistant, *entities: str) -> list[tuple[str, str]]:
    """Every reported change of state of the entities, in order."""
    seen: list[tuple[str, str]] = []

    @callback
    def changed(event: Event) -> None:
        old, new = event.data["old_state"], event.data["new_state"]
        if new and (old is None or old.state != new.state):
            seen.append((new.entity_id, new.state))

    async_track_state_change_event(hass, list(entities), changed)
    return seen


async def later(hass: HomeAssistant, seconds: float) -> None:
    async_fire_time_changed(hass, datetime.fromtimestamp(time.time() + seconds, UTC))
    for _ in range(10):
        await asyncio.sleep(0)


def call_in_background(hass: HomeAssistant, domain: str, action: str, data: dict) -> asyncio.Task:
    """A call that returns only when the device is done, started so the test can move the time on."""
    return asyncio.ensure_future(hass.services.async_call(domain, action, data, blocking=True))


async def play(hass: HomeAssistant, media: str, announce: bool = False) -> None:
    data = {"entity_id": SPK, "media_content_type": "music", "media_content_id": media}
    if announce:
        data["announce"] = True
    await hass.services.async_call("media_player", "play_media", data, blocking=True)
    await hass.async_block_till_done()


async def test_FG01_music_plays_in_the_background(hass, fakes):
    await play(hass, TWO_SECONDS)
    assert hass.states.get(SPK).state == "playing"
    assert hass.states.get(SAT).state == "idle"


async def test_FG02_a_playback_ends_after_the_medias_length(hass, fakes):
    await play(hass, TWO_SECONDS)
    assert hass.states.get(SPK).state == "playing"
    await later(hass, 1)
    assert hass.states.get(SPK).state == "playing"
    await later(hass, 2.5)
    assert hass.states.get(SPK).state == "idle"


async def test_FG07_a_stream_plays_until_it_is_stopped(hass, fakes):
    await play(hass, (REPO / "testenv" / "no-such-stream").as_uri())
    await later(hass, 3600)
    assert hass.states.get(SPK).state == "playing"
    await hass.services.async_call("media_player", "media_stop", {"entity_id": SPK}, blocking=True)
    assert hass.states.get(SPK).state == "paused"


async def test_FG03_stopping_pauses(hass, fakes):
    await play(hass, TWO_SECONDS)
    await hass.services.async_call("media_player", "media_stop", {"entity_id": SPK}, blocking=True)
    assert hass.states.get(SPK).state == "paused"
    await later(hass, 5)
    assert hass.states.get(SPK).state == "paused"


async def test_FG04_a_replacing_playback_reports_no_change(hass, fakes):
    await play(hass, TWO_SECONDS)
    seen = states(hass, SPK)
    await later(hass, 1)
    await play(hass, TWO_SECONDS)
    assert seen == []
    # The replaced one ends nothing: the new one runs its own two seconds.
    await later(hass, 1)
    assert hass.states.get(SPK).state == "playing"
    await later(hass, 2.5)
    assert seen == [(SPK, "idle")]


async def test_FG05_an_announced_playback_plays_in_the_foreground(hass, fakes):
    await play(hass, TWO_SECONDS, announce=True)
    assert hass.states.get(SPK).state == "playing"
    assert hass.states.get(SAT).state == "responding"
    await later(hass, 2.5)
    assert hass.states.get(SPK).state == "idle"
    assert hass.states.get(SAT).state == "idle"


async def test_FG10_a_plain_announcement_ends_idle(hass, fakes):
    seen = states(hass, SAT)
    call = call_in_background(hass, "assist_satellite", "announce",
                              {"entity_id": SAT, "message": "Hallo", "preannounce": False})
    await later(hass, 0)
    assert not call.done()
    await later(hass, 2)
    await call
    assert seen == [(SAT, "responding"), (SAT, "idle")]


async def test_FG11_starting_a_conversation_has_a_short_idle_before_listening(hass, fakes):
    seen = states(hass, SAT)
    call = call_in_background(hass, "assist_satellite", "start_conversation",
                              {"entity_id": SAT, "start_message": "Noch etwas?", "preannounce": False})
    await later(hass, 0)
    await later(hass, 2)
    await call
    assert seen == [(SAT, "responding"), (SAT, "idle"), (SAT, "listening")]
    await later(hass, 10)
    assert seen[-1] == (SAT, "idle")


async def test_FG12_the_wake_word_lowers_music(hass, fakes):
    await hass.services.async_call("media_player", "volume_set", {"entity_id": SPK, "volume_level": 0.6}, blocking=True)
    await play(hass, (REPO / "testenv" / "no-such-stream").as_uri())
    await hass.services.async_call("techo5_fake", "wake_word", {"device": "fake_show"}, blocking=True)
    assert hass.states.get(SAT).state == "listening"
    assert hass.states.get(SPK).attributes["volume_level"] < 0.6 / 4
    await later(hass, 10)
    assert hass.states.get(SAT).state == "idle"
    assert hass.states.get(SPK).attributes["volume_level"] == 0.6
    assert hass.states.get(SPK).state == "playing"


async def test_FG13_the_wake_word_does_not_lower_an_announced_playback(hass, fakes):
    await hass.services.async_call("media_player", "volume_set", {"entity_id": SPK, "volume_level": 0.6}, blocking=True)
    await play(hass, TWO_SECONDS, announce=True)
    await hass.services.async_call("techo5_fake", "wake_word", {"device": "fake_show"}, blocking=True)
    assert hass.states.get(SAT).state == "listening"
    assert hass.states.get(SPK).attributes["volume_level"] == 0.6


async def test_FG14_a_device_without_a_screen_shows_nothing(hass, fakes):
    before = {s.entity_id: s.state for s in hass.states.async_all() if "fake_echo" in s.entity_id
              and not s.entity_id.endswith("fake_log")}
    await hass.services.async_call("esphome", "fake_echo_notify", {"message": "Hallo"}, blocking=True)
    await hass.async_block_till_done()
    after = {s.entity_id: s.state for s in hass.states.async_all() if "fake_echo" in s.entity_id
             and not s.entity_id.endswith("fake_log")}
    assert after == before
    log = hass.states.get("sensor.fake_echo_fake_log").attributes["log"]
    assert log[-1]["kind"] == "action" and log[-1]["action"] == "notify"


async def say(hass: HomeAssistant, text: str) -> asyncio.Task:
    return asyncio.ensure_future(hass.services.async_call(
        "techo5_fake", "say", {"device": "fake_show", "text": text}, blocking=True, return_response=True))


async def test_FG15_a_voice_turn_reports_the_states_of_a_voice_turn(hass, fakes):
    seen = states(hass, SAT)
    turn = await say(hass, "Wie spät ist es?")
    for _ in range(20):
        await later(hass, 1)
        if turn.done():
            break
    answer = await turn
    assert seen == [(SAT, "listening"), (SAT, "processing"), (SAT, "responding"), (SAT, "idle")]
    assert answer["speech"]
    assert answer["continue_conversation"] is False


async def test_FG16_background_music_is_lowered_for_the_whole_voice_turn(hass, fakes):
    await hass.services.async_call("media_player", "volume_set", {"entity_id": SPK, "volume_level": 0.6}, blocking=True)
    await play(hass, (REPO / "testenv" / "no-such-stream").as_uri())
    volumes: list[tuple[str, float]] = []

    @callback
    def changed(event: Event) -> None:
        volumes.append((hass.states.get(SAT).state, hass.states.get(SPK).attributes["volume_level"]))

    async_track_state_change_event(hass, [SAT], changed)
    turn = await say(hass, "Wie spät ist es?")
    for _ in range(20):
        await later(hass, 1)
        if turn.done():
            break
    await turn
    assert [s for s, _ in volumes] == ["listening", "processing", "responding", "idle"]
    assert all(v < 0.6 / 4 for s, v in volumes if s != "idle")
    assert hass.states.get(SPK).attributes["volume_level"] == 0.6


async def test_FG17_a_longer_text_takes_longer_to_speak(hass, fakes):
    async def responding_for(text: str) -> int:
        call = call_in_background(hass, "assist_satellite", "announce",
                                  {"entity_id": SAT, "message": text, "preannounce": False})
        await later(hass, 0)
        for second in range(1, 20):
            await later(hass, second)
            if call.done():
                return second
        raise AssertionError("never done")

    short = await responding_for("x" * 15)
    long = await responding_for("x" * 90)
    assert short < long
