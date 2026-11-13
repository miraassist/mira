"""Mira's group watch in Home Assistant as docs/spec/group-watch.md says: the shared selection
(GW-01 to GW-03) and the seven blueprints (GW-08 to GW-16), loaded from internal/watch/blueprints as
the service saves them, over the fake house of testenv/watch_fake, with the time moved on."""

from __future__ import annotations

import asyncio
import shutil
from datetime import datetime, timedelta
from pathlib import Path

import pytest
from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers import label_registry as lr
from homeassistant.helpers.template import Template
from homeassistant.setup import async_setup_component
from homeassistant.util import dt as dt_util
from pytest_homeassistant_custom_component.common import async_fire_time_changed, async_mock_service

import custom_components

REPO = Path(__file__).resolve().parents[2]
BLUEPRINTS = REPO / "internal" / "watch" / "blueprints"
FAKE = REPO / "testenv" / "watch_fake"
SELECTION = (BLUEPRINTS / "selection.jinja").read_text().strip()
OPENINGS = ["window", "door", "opening", "garage_door"]
LABEL = "mira_dont_watch"

# The automations' template triggers keep a timer for the next minute.
pytestmark = pytest.mark.parametrize("expected_lingering_timers", [True])


@pytest.fixture
async def house(hass: HomeAssistant, tmp_path, monkeypatch, freezer):
    """The fake house with its floors (its components set up after the group helpers: a component is
    set up once), a group helper of two windows and a light group, Mira's label,
    Mira's blueprints where the service saves them, and mira.send_message recorded. Anna and Ben
    are at home."""
    freezer.move_to(dt_util.as_utc(datetime(2026, 11, 13, 20, 0, tzinfo=dt_util.get_default_time_zone())))
    hass.data["test_freezer"] = freezer
    config = tmp_path / "config"
    shutil.copytree(BLUEPRINTS, config / "blueprints" / "automation" / "mira")
    monkeypatch.setattr(hass.config, "config_dir", str(config))
    (tmp_path / "custom" / "watch_fake").parent.mkdir()
    (tmp_path / "custom" / "watch_fake").symlink_to(FAKE)
    monkeypatch.setattr(custom_components, "__path__", [*custom_components.__path__, str(tmp_path / "custom")])
    hass.data.pop("custom_components", None)
    assert await async_setup_component(
        hass,
        "binary_sensor",
        {
            "binary_sensor": [
                {
                    "platform": "group",
                    "name": "Fake Fenster Gast",
                    "device_class": "window",
                    "entities": ["binary_sensor.fake_fenster_schlafzimmer", "binary_sensor.fake_fenster_buro"],
                }
            ]
        },
    )
    assert await async_setup_component(
        hass,
        "light",
        {
            "light": [
                {
                    "platform": "group",
                    "name": "Fake Lichter Außen",
                    "entities": ["light.fake_licht_terrasse", "light.fake_licht_garten"],
                }
            ]
        },
    )
    assert await async_setup_component(hass, "watch_fake", {"watch_fake": {}})
    await hass.async_block_till_done()
    labels = lr.async_get(hass)
    labels.async_create("mira_dont_watch")
    hass.states.async_set("zone.home", "2")
    await hass.async_block_till_done()
    return async_mock_service(hass, "mira", "send_message")


def members(hass: HomeAssistant, domain: str, classes: list[str], **selection) -> list[str]:
    """The selection rendered as the check renders it: the members' ids."""
    variables = {"domain": domain, "classes": classes, "areas": [], "floors": [], "group": "", **selection}
    out = Template(SELECTION + "{{ ns.members | map(attribute='entity_id') | list }}", hass).async_render(variables)
    return sorted(out)


def label(hass: HomeAssistant, entity_id: str) -> None:
    er.async_get(hass).async_update_entity(entity_id, labels={LABEL})


async def test_GW01_a_selection_by_device_class_and_floor(hass, house):
    # Given fake windows on the ground floor and on the top floor
    # When the selection "windows on the top floor" is rendered
    got = members(hass, "binary_sensor", ["window"], floors=["dachgeschoss"])
    # Then only the top floor's windows are members
    assert got == ["binary_sensor.fake_fenster_buro", "binary_sensor.fake_fenster_schlafzimmer"]
    # An area and a floor together: everything in the area and on the floor.
    assert members(hass, "binary_sensor", OPENINGS, floors=["dachgeschoss"], areas=["keller"]) == [
        "binary_sensor.fake_fenster_buro",
        "binary_sensor.fake_fenster_schlafzimmer",
        "binary_sensor.fake_kellerfenster",
    ]
    # Without areas and floors: the whole house, doors and the garage door included, no motion sensor.
    everything = members(hass, "binary_sensor", OPENINGS)
    assert "binary_sensor.fake_garagentor" in everything and "binary_sensor.fake_haustur" in everything
    assert "binary_sensor.fake_bewegung_garten" not in everything
    assert len(everything) == 8


async def test_GW02_a_labelled_entity_is_no_member(hass, house):
    # Given the fake garden light carries the label "Mira: nicht überwachen"
    label(hass, "light.fake_licht_garten")
    # When the selection "all lights" is rendered
    got = members(hass, "light", [])
    # Then the garden light is not a member
    assert "light.fake_licht_garten" not in got
    # And a light group is not a member, its lights are
    assert "light.fake_lichter_aussen" not in got
    assert "light.fake_licht_terrasse" in got and "light.fake_licht_kuche" in got


async def test_GW03_a_group_helpers_members(hass, house):
    # Given a group helper with two fake windows, one of them labelled
    label(hass, "binary_sensor.fake_fenster_buro")
    # When the selection is that group helper
    got = members(hass, "binary_sensor", OPENINGS, group="binary_sensor.fake_fenster_gast")
    # Then exactly its two windows are members, labelled or not
    assert got == ["binary_sensor.fake_fenster_buro", "binary_sensor.fake_fenster_schlafzimmer"]


async def settle() -> None:
    """Let what the last change started run (a run waiting in a delay stays pending)."""
    for _ in range(20):
        await asyncio.sleep(0)


async def minutes(hass: HomeAssistant, freezer, n: int) -> None:
    """Move the time on minute by minute, as Home Assistant lives it."""
    for _ in range(n):
        freezer.tick(timedelta(minutes=1))
        async_fire_time_changed(hass)
        await settle()


GROUP_BLUEPRINTS = {
    "left_on": {},
    "open_while_away": {},
    "open_at_night": {"night_start": "23:00:00", "night_end": "06:00:00"},
    "open_too_long": {},
    "motion_while_away": {},
    "motion_at_night": {"night_start": "23:00:00", "night_end": "06:00:00"},
    "temperature_out_of_range": {"below": 5},
}


async def test_every_group_blueprint_loads(hass, house):
    """Each group blueprint makes an automation Home Assistant takes, with only its inputs without default."""
    automations = [
        {"id": f"mira_{name}", "alias": name, "use_blueprint": {"path": f"mira/{name}.yaml", "input": inputs}}
        for name, inputs in GROUP_BLUEPRINTS.items()
    ]
    assert await async_setup_component(hass, "automation", {"automation": automations})
    await hass.async_block_till_done()
    for name in GROUP_BLUEPRINTS:
        assert hass.states.get(f"automation.{name}").state == "on", name


async def watch(hass: HomeAssistant, blueprint: str, **inputs) -> None:
    """A group watch as Mira saves it: an automation using the blueprint, its entry the members' names."""
    inputs.setdefault("message", "{{ breaking | join(', ') }}")
    assert await async_setup_component(
        hass,
        "automation",
        {
            "automation": [
                {
                    "id": "mira_g1",
                    "alias": "Wächter",
                    "use_blueprint": {"path": f"mira/{blueprint}.yaml", "input": inputs},
                }
            ]
        },
    )
    await hass.async_block_till_done()
    assert hass.states.get("automation.wachter").state == "on"


async def put(hass: HomeAssistant, entity_id: str, state: str) -> None:
    await hass.services.async_call("watch_fake", "set", {"entity_id": entity_id, "state": state}, blocking=True)
    await rendered(hass)


async def rendered(hass: HomeAssistant) -> None:
    """Home Assistant renders a template over a whole domain at most once a second: two seconds on."""
    await settle()
    hass.data["test_freezer"].tick(timedelta(seconds=2))
    async_fire_time_changed(hass)
    await settle()


async def nobody_home(hass: HomeAssistant, away: bool = True) -> None:
    hass.states.async_set("zone.home", "0" if away else "2")
    await rendered(hass)


def entries(calls) -> list[dict]:
    out = []
    for call in calls:
        data = dict(call.data)
        assert data["dedupe_key"] == "watch:mira_g1" and data["task"] == "mira_g1" and data["expires_in"] == "12h"
        out.append(data)
    return out


async def at(hass: HomeAssistant, freezer, hour: int, minute: int = 0) -> None:
    """Move the time on to the next hour:minute, minute by minute."""
    now = dt_util.now()
    target = now.replace(hour=hour, minute=minute, second=0, microsecond=0)
    if target <= now:
        target += timedelta(days=1)
    await minutes(hass, freezer, int((target - now).total_seconds() // 60))


async def test_GW08_lights_left_on(hass, house, freezer):
    # Given a watch "left_on" with 3 hours
    await watch(hass, "left_on", hours=3)
    # When a fake light is on for 3 hours
    await hass.services.async_call("light", "turn_on", {"entity_id": "light.fake_licht_kuche"}, blocking=True)
    await minutes(hass, freezer, 179)
    assert house == []
    await minutes(hass, freezer, 2)
    # Then an entry names the light, for the house (somebody is home)
    [entry] = entries(house)
    assert entry["text"] == "Fake Licht Küche" and entry["receiver"] == "house" and entry["importance"] == 1
    # And once per streak: not again while it stays on
    await minutes(hass, freezer, 120)
    assert len(house) == 1
    # (The user's "mach's aus" is the next turn with Home Assistant's own tools: run at the test HA.)
    await hass.services.async_call("light", "turn_off", {"entity_id": "light.fake_licht_kuche"}, blocking=True)
    await minutes(hass, freezer, 2)
    assert hass.states.get("light.fake_licht_kuche").state == "off"


async def test_GW08_only_while_nobody_is_home(hass, house, freezer):
    await watch(hass, "left_on", hours=1, only_away=True)
    await hass.services.async_call("light", "turn_on", {"entity_id": "light.fake_licht_schlafzimmer"}, blocking=True)
    await minutes(hass, freezer, 90)
    assert house == []
    await nobody_home(hass)
    [entry] = entries(house)
    assert entry["receiver"] == "persons" and entry["text"] == "Fake Licht Schlafzimmer"


async def test_GW09_an_excluded_light_never_makes_or_joins_an_entry(hass, house, freezer):
    # Given a watch "left_on" and the garden light labelled
    label(hass, "light.fake_licht_garten")
    await watch(hass, "left_on", hours=3)
    # When the garden light and the kitchen light are on for the hours
    await hass.services.async_call("light", "turn_on", {"entity_id": "light.fake_licht_garten"}, blocking=True)
    await minutes(hass, freezer, 30)
    await hass.services.async_call("light", "turn_on", {"entity_id": "light.fake_licht_kuche"}, blocking=True)
    await minutes(hass, freezer, 200)
    # Then the entry names the kitchen light only (the garden light, on longer, made none)
    [entry] = entries(house)
    assert entry["text"] == "Fake Licht Küche"


async def test_GW10_open_while_nobody_is_home(hass, house, freezer):
    # Given a watch "open_while_away" and a fake window open
    await watch(hass, "open_while_away")
    await put(hass, "binary_sensor.fake_fenster_kuche", "on")
    await minutes(hass, freezer, 10)
    assert house == []
    # When every fake person leaves
    await nobody_home(hass)
    await minutes(hass, freezer, 4)
    assert house == []
    await minutes(hass, freezer, 2)
    # Then after the delay every person gets an entry privately naming the window
    [entry] = entries(house)
    assert entry["receiver"] == "persons" and entry["importance"] == 2 and entry["text"] == "Fake Fenster Küche"


async def test_GW11_open_at_night(hass, house, freezer):
    # Given a watch "open_at_night" from 23 to 6
    await watch(hass, "open_at_night", night_start="23:00:00", night_end="06:00:00")
    await at(hass, freezer, 22, 30)
    # When a fake window is open at 23:10
    await put(hass, "binary_sensor.fake_fenster_schlafzimmer", "on")
    await at(hass, freezer, 23, 9)
    assert house == []
    await minutes(hass, freezer, 2)
    # Then an entry names the window
    [entry] = entries(house)
    assert entry["text"] == "Fake Fenster Schlafzimmer" and entry["receiver"] == "house"
    # Open by day: nothing.
    await put(hass, "binary_sensor.fake_fenster_schlafzimmer", "off")
    await at(hass, freezer, 9)
    await put(hass, "binary_sensor.fake_fenster_schlafzimmer", "on")
    await minutes(hass, freezer, 60)
    assert len(house) == 1


async def test_GW12_open_too_long_once_per_streak(hass, house, freezer):
    # Given a watch "open_too_long" with 30 minutes
    await watch(hass, "open_too_long", minutes=30)
    # When a fake window is open for 30 minutes and a second one opens meanwhile
    await put(hass, "binary_sensor.fake_fenster_wohnzimmer", "on")
    await minutes(hass, freezer, 10)
    await put(hass, "binary_sensor.fake_haustur", "on")
    await minutes(hass, freezer, 21)
    # Then one entry names the window
    [entry] = entries(house)
    assert entry["text"] == "Fake Fenster Wohnzimmer"
    # And no second entry comes until both were closed
    await minutes(hass, freezer, 60)
    await put(hass, "binary_sensor.fake_fenster_wohnzimmer", "off")
    await minutes(hass, freezer, 60)
    assert len(house) == 1
    await put(hass, "binary_sensor.fake_haustur", "off")
    await minutes(hass, freezer, 2)
    await put(hass, "binary_sensor.fake_haustur", "on")
    await minutes(hass, freezer, 31)
    assert len(house) == 2 and house[1].data["text"] == "Fake Haustür"


async def test_GW13_motion_while_nobody_is_home(hass, house, freezer):
    # Given a watch "motion_while_away"
    await watch(hass, "motion_while_away")
    await put(hass, "binary_sensor.fake_bewegung_garten", "on")
    await put(hass, "binary_sensor.fake_bewegung_garten", "off")
    assert house == []
    # When fake motion is detected while nobody is home
    await nobody_home(hass)
    await put(hass, "binary_sensor.fake_prasenz_buro", "on")
    # Then every person gets an entry privately
    [entry] = entries(house)
    assert entry["receiver"] == "persons" and entry["importance"] == 2 and entry["text"] == "Fake Präsenz Büro"


async def test_GW14_motion_at_night(hass, house, freezer):
    # Given a watch "motion_at_night" on the garden from 23 to 6
    await watch(hass, "motion_at_night", areas=["garten"], night_start="23:00:00", night_end="06:00:00")
    await put(hass, "binary_sensor.fake_prasenz_buro", "on")  # not in the garden
    await at(hass, freezer, 2)
    assert house == []
    # When fake motion in the garden at 2:00
    await put(hass, "binary_sensor.fake_bewegung_garten", "on")
    # Then an entry names the garden's sensor
    [entry] = entries(house)
    assert entry["text"] == "Fake Bewegung Garten"


async def test_GW15_too_cold(hass, house, freezer):
    # Given a watch "temperature_out_of_range" on the cellar below 5 °C for 30 minutes
    await watch(
        hass,
        "temperature_out_of_range",
        areas=["keller"],
        below=5,
        minutes=30,
        message="{% for b in breaking %}{{ b.name }}: {{ b.value }} °C, {{ 'unter' if b.low else 'über' }} {{ below if b.low else above }} °C.{% endfor %}",
    )
    # An unavailable sensor makes no entry
    await put(hass, "sensor.fake_temperatur_keller", "unavailable")
    await minutes(hass, freezer, 40)
    assert house == []
    # When a fake cellar temperature is 4 °C for 30 minutes
    await put(hass, "sensor.fake_temperatur_keller", "4")
    await minutes(hass, freezer, 29)
    assert house == []
    await minutes(hass, freezer, 2)
    # Then an entry names the sensor and its value
    [entry] = entries(house)
    assert entry["text"] == "Fake Temperatur Keller: 4.0 °C, unter 5 °C."
    # The living room is no member: too warm there says nothing.
    await put(hass, "sensor.fake_temperatur_wohnzimmer", "40")
    await minutes(hass, freezer, 60)
    assert len(house) == 1


async def test_GW16_a_watch_switched_off_says_nothing(hass, house, freezer):
    # Given a watch "motion_while_away" switched off by the household's holiday script
    await watch(hass, "motion_while_away")
    await hass.services.async_call("automation", "turn_off", {"entity_id": "automation.wachter"}, blocking=True)
    # When fake motion is detected while nobody is home
    await nobody_home(hass)
    await put(hass, "binary_sensor.fake_bewegung_garten", "on")
    await minutes(hass, freezer, 5)
    # Then no entry comes
    assert house == []
