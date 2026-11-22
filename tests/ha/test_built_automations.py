"""The automations the service builds (the sleep timer, the alarm) pass Home
Assistant's own check and the rule what a task may call
(mira/automation/validate). The service writes them in its tests
(internal/timer/testdata, internal/alarm/testdata); here Home Assistant checks
them (docs/spec/scheduled-actions.md, docs/spec/alarm.md)."""

import copy
import json
from pathlib import Path

import pytest
from homeassistant.exceptions import HomeAssistantError
from homeassistant.setup import async_setup_component

from custom_components.mira.automations import validate

ROOT = Path(__file__).resolve().parents[2]
FORBIDDEN = {"domains": ["lock", "alarm_control_panel"], "entities": ["cover.garage_door"]}


def _built(path: str) -> list[dict]:
    return json.loads((ROOT / path).read_text())


@pytest.fixture(autouse=True)
async def actions_described(hass):
    """The actions they use, described as Home Assistant does: the check only
    lets through actions that name their entities."""
    for domain in ("automation", "light", "media_player"):
        assert await async_setup_component(hass, domain, {})


async def test_the_sleep_timer_is_a_valid_automation_that_runs_once_and_catches_up(hass):
    (config,) = _built("internal/timer/testdata/once.json")
    await validate(hass, config, FORBIDDEN)
    # At the due second, and again when Home Assistant starts (it was off);
    # not before the due time, and only if it never ran.
    assert {"trigger": "homeassistant", "event": "start"} in config["triggers"]
    assert {"trigger": "time", "at": "22:35:00"} in config["triggers"]
    condition = config["conditions"][0]["value_template"]
    assert "now() >= as_datetime('2099-01-01T22:35:00+01:00')" in condition and "last_triggered is none" in condition
    assert config["alias"] == "Schlaf-Timer Wohnzimmer 837deda7" and config["mode"] == "single"


async def test_a_timer_may_not_switch_what_needs_a_yes(hass):
    (config,) = _built("internal/timer/testdata/once.json")
    config = copy.deepcopy(config)
    config["actions"] = [{"action": "lock.unlock", "target": {"entity_id": "lock.haustuer"}}]
    with pytest.raises(HomeAssistantError):
        await validate(hass, config, FORBIDDEN)


async def test_the_alarms_are_valid_automations(hass):
    """A once-alarm, one on weekdays with light, one with a station."""
    configs = _built("internal/alarm/testdata/alarms.json")
    assert len(configs) == 3
    for config in configs:
        await validate(hass, config, FORBIDDEN)
