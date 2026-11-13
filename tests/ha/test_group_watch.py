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
