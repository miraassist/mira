"""Mira's blueprints run in Home Assistant as docs/spec/device-watch.md says
(scenarios CW-11 to CW-15): loaded from internal/watch/blueprints, as the
service saves them, driven by the fake water meter and the fake plug of
testenv/watch_fake, with the time moved on."""

from __future__ import annotations

import asyncio
import re
import shutil
from datetime import datetime, timedelta
from pathlib import Path

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.setup import async_setup_component
from homeassistant.util import dt as dt_util
from pytest_homeassistant_custom_component.common import async_fire_time_changed, async_mock_service

import custom_components

REPO = Path(__file__).resolve().parents[2]
BLUEPRINTS = REPO / "internal" / "watch" / "blueprints"
FAKE = REPO / "testenv" / "watch_fake"

METER = "sensor.fake_wasserzahler"
# The automations' template triggers keep a timer for the next minute.
pytestmark = pytest.mark.parametrize("expected_lingering_timers", [True])
PLUG = "sensor.fake_aquarium_leistung"


@pytest.fixture
async def house(hass: HomeAssistant, tmp_path, monkeypatch, freezer):
    """Mira's blueprints where the service saves them, the fake devices, and
    mira.send_message recorded."""
    freezer.move_to(dt_util.as_utc(datetime(2026, 11, 13, 8, 0, tzinfo=dt_util.get_default_time_zone())))
    config = tmp_path / "config"
    shutil.copytree(BLUEPRINTS, config / "blueprints" / "automation" / "mira")
    monkeypatch.setattr(hass.config, "config_dir", str(config))
    (tmp_path / "custom" / "watch_fake").parent.mkdir()
    (tmp_path / "custom" / "watch_fake").symlink_to(FAKE)
    monkeypatch.setattr(custom_components, "__path__", [*custom_components.__path__, str(tmp_path / "custom")])
    hass.data.pop("custom_components", None)
    assert await async_setup_component(hass, "watch_fake", {"watch_fake": {}})
    await hass.async_block_till_done()
    return async_mock_service(hass, "mira", "send_message")


async def settle() -> None:
    """Let what the last change started run. Not async_block_till_done: a
    run waiting in a delay is a task that stays pending for hours."""
    for _ in range(20):
        await asyncio.sleep(0)


async def watch(hass: HomeAssistant, blueprint: str, **inputs) -> None:
    """A watch as Mira saves it: an automation using the blueprint, loaded
    (Home Assistant says so with automation_reloaded). The fake devices were
    set up just before: their last change is now."""
    assert await async_setup_component(
        hass,
        "automation",
        {
            "automation": [
                {
                    "id": "mira_w1",
                    "alias": "Wächter",
                    "use_blueprint": {"path": f"mira/{blueprint}.yaml", "input": inputs},
                }
            ]
        },
    )
    await hass.async_block_till_done()
    assert hass.states.get("automation.wachter").state == "on"
    hass.bus.async_fire("automation_reloaded")
    await settle()


async def minutes(hass: HomeAssistant, freezer, n: int) -> None:
    """Move the time on minute by minute, as Home Assistant lives it."""
    for _ in range(n):
        freezer.tick(timedelta(minutes=1))
        async_fire_time_changed(hass)
        await settle()


async def act(hass: HomeAssistant, action: str, **data) -> None:
    await hass.services.async_call("watch_fake", action, data, blocking=True)
    await settle()


def entry(call) -> dict:
    data = dict(call.data)
    assert data["receiver"] == "house"
    assert data["dedupe_key"] == "watch:mira_w1"
    assert data["expires_in"] == "12h"
    assert data["task"] == "mira_w1"
    return data


async def test_CW11_water_runs_on(hass, house, freezer):
    calls = house
    # Given a watch "water_runs_on" on the fake water meter with 30 minutes
    await watch(hass, "water_runs_on", meter=METER, duration=30, message="{{ duration }} min, {{ states(meter) }} L")
    # (A template trigger fires when its template turns true: the meter
    # rests longer than the gap first.)
    await minutes(hass, freezer, 12)
    # When the meter changes every 5 minutes for 30 minutes
    await act(hass, "water_run", litres=10, every=300)
    await minutes(hass, freezer, 29)
    assert calls == []
    await minutes(hass, freezer, 2)
    # Then a simple message says that water has run for 30 minutes, with the meter's value
    assert len(calls) == 1
    data = entry(calls[0])
    assert data["importance"] == 2
    assert re.fullmatch(r"30 min, 81\d\d\d\.0 L", data["text"])
    # And it is said only once while the water keeps running
    await minutes(hass, freezer, 60)
    assert len(calls) == 1
    # Once it stopped for longer than the gap, a new streak is said again.
    await act(hass, "water_stop")
    await minutes(hass, freezer, 15)
    await act(hass, "water_run", litres=10, every=300)
    await minutes(hass, freezer, 31)
    assert len(calls) == 2


async def test_CW11_the_default_entry_names_the_facts(hass, house, freezer):
    await watch(hass, "water_runs_on", meter=METER, duration=10)
    await minutes(hass, freezer, 12)
    await act(hass, "water_run", litres=5, every=120)
    await minutes(hass, freezer, 11)
    assert len(house) == 1
    assert "water had run for 10 minutes without a pause. Meter: 81" in house[0].data["text"]


async def hours(hass: HomeAssistant, freezer, n: float) -> None:
    """Move the time on in steps of 10 minutes."""
    for _ in range(int(n * 6)):
        freezer.tick(timedelta(minutes=10))
        async_fire_time_changed(hass)
        await settle()


async def test_CW12_water_never_rests(hass, house, freezer):
    calls = house
    # Given a watch "water_never_rests" on the fake water meter
    await watch(hass, "water_never_rests", meter=METER, rest=1, window=24)
    # When the meter has no rest of an hour within the window
    await act(hass, "water_run", litres=0.5, every=1800)
    await hours(hass, freezer, 23.5)
    assert calls == []
    await hours(hass, freezer, 1)
    # Then a simple message says the meter did not rest, with its value
    assert len(calls) == 1
    data = entry(calls[0])
    assert data["importance"] == 1
    assert re.search(r"within the last 24 hours\..* Meter: 812\d\d(\.\d)? L\.$", data["text"])
    # And it comes again after another window without a rest
    await hours(hass, freezer, 24)
    assert len(calls) == 2


async def test_CW13_a_rest_starts_the_window_anew(hass, house, freezer):
    calls = house
    # Given a watch "water_never_rests" on the fake water meter
    await watch(hass, "water_never_rests", meter=METER, rest=1, window=24)
    await act(hass, "water_run", litres=0.5, every=1800)
    await hours(hass, freezer, 10)
    # When the meter rests for an hour before the window ends
    await act(hass, "water_stop")
    await hours(hass, freezer, 1.5)
    await act(hass, "water_run", litres=0.5, every=1800)
    # Then no message comes at the end of the first window
    await hours(hass, freezer, 14)
    assert calls == []
    # but a window after the rest ended, while it does not rest again.
    await hours(hass, freezer, 11)
    assert len(calls) == 1


async def test_CW13_a_rest_that_lasts_holds_no_entry(hass, house, freezer):
    calls = house
    await watch(hass, "water_never_rests", meter=METER, rest=1, window=24)
    # The meter stands still from before the watch was loaded and stays so.
    await hours(hass, freezer, 50)
    assert calls == []


async def test_CW15_a_device_leaves_its_daily_routine(hass, house, freezer):
    calls = house
    # Given a watch "daily_routine" on a fake plug with the windows "07:40-11:40, 14:20-21:00"
    await act(hass, "routine", windows="07:40-11:40, 14:20-21:00", watts=50)
    await watch(hass, "daily_routine", power=PLUG, windows="07:40-11:40, 14:20-21:00", tolerance=15)
    # The plug follows its routine through the day: no entry.
    await minutes(hass, freezer, 18 * 60 - 8 * 60 + 36)  # 08:00 to 18:36
    assert hass.states.get(PLUG).state == "50.0"
    assert calls == []
    # When the plug's power drops to 0 W at 18:36
    await act(hass, "power", watts=0)
    await minutes(hass, freezer, 14)
    assert calls == []
    await minutes(hass, freezer, 1)
    # Then 15 minutes later a simple message says the device left its daily routine
    assert len(calls) == 1
    data = entry(calls[0])
    assert data["importance"] == 1
    assert "18:51" in data["text"]
    assert "0.0 W, expected on" in data["text"]


async def test_CW15_an_unavailable_plug_is_no_deviation(hass, house, freezer):
    calls = house
    await act(hass, "routine", windows="07:40-11:40", watts=50)
    await watch(hass, "daily_routine", power=PLUG, windows="07:40-11:40", tolerance=15)
    await act(hass, "power", watts=0)
    hass.states.async_set(PLUG, "unavailable")
    await minutes(hass, freezer, 30)
    assert calls == []
