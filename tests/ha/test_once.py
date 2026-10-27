"""One-time automations (sleep timers): what they look like, the limit, the
tidy-up, who may manage them."""

from datetime import timedelta
from unittest.mock import AsyncMock, patch

import pytest

from homeassistant.exceptions import HomeAssistantError
from homeassistant.setup import async_setup_component
from homeassistant.util import dt as dt_util

from custom_components.mira import once
from custom_components.mira.automations import validate

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}
ACTIONS = [
    {"action": "media_player.media_stop", "target": {"entity_id": ["media_player.show_speaker"]}},
    {"action": "light.turn_off", "target": {"entity_id": ["light.farblampe"]}},
]
FORBIDDEN = {"domains": ["lock", "alarm_control_panel"], "entities": ["cover.garage_door"]}


@pytest.fixture(autouse=True)
async def actions_described(hass):
    """The actions the timers use, described as Home Assistant does: the
    check only lets through actions that name their entities."""
    for domain in ("light", "media_player"):
        assert await async_setup_component(hass, domain, {})


def _timer(id_, owner="anna", area="wohnzimmer", ran=False, running=0):
    return {
        "id": id_, "entity_id": f"automation.{id_}", "title": "Schlaf-Timer", "due": "2099-01-01T22:35:00+01:00",
        "owner": owner, "area": area, "kind": "sleep", "actions": ACTIONS, "ran": ran, "running": running,
    }


async def test_a_timer_is_a_valid_automation_that_runs_once_and_catches_up(hass):
    assert await async_setup_component(hass, "automation", {})
    due = dt_util.now() + timedelta(minutes=30)
    config = once.build("Schlaf-Timer", due, "anna", "wohnzimmer", "sleep", ACTIONS, "837deda7")
    await validate(hass, config, FORBIDDEN)

    stamp = dt_util.as_local(due).replace(microsecond=0).isoformat()
    assert config["variables"] == {
        "mira_title": "Schlaf-Timer", "mira_due": stamp, "mira_owner": "anna", "mira_area": "wohnzimmer",
        "mira_kind": "sleep",
    }
    # Every timer has its own entity id, so no state is restored from an earlier one.
    other = once.build("Schlaf-Timer", due, "anna", "wohnzimmer", "sleep", ACTIONS, "11223344")
    assert config["alias"] == "Schlaf-Timer 837deda7" and other["alias"] != config["alias"]
    # At the due second, and again when Home Assistant starts (it was off).
    assert {"trigger": "homeassistant", "event": "start"} in config["triggers"]
    assert any(t["trigger"] == "time" and t["at"] == dt_util.as_local(due).strftime("%H:%M:%S") for t in config["triggers"])
    # Not before the due time, and only if it never ran.
    condition = config["conditions"][0]["value_template"]
    assert "now() >= as_datetime('%s')" % stamp in condition and "last_triggered is none" in condition
    assert config["mode"] == "single"


async def test_a_timer_may_not_switch_what_needs_a_yes(hass):
    assert await async_setup_component(hass, "automation", {})
    config = once.build(
        "Nachts", dt_util.now() + timedelta(hours=1), "anna", "flur", "timer",
        [{"action": "lock.unlock", "target": {"entity_id": "lock.haustuer"}}],
    )
    with pytest.raises(HomeAssistantError):
        await validate(hass, config, FORBIDDEN)


async def test_at_most_ten_timers_per_person_and_room(hass):
    due = (dt_util.now() + timedelta(hours=1)).isoformat()
    full = [_timer(f"t{i}") for i in range(once.MAX_PER_OWNER_AREA)]
    saved = AsyncMock(return_value={"id": "new", "entity_id": "automation.new"})
    with patch.object(once, "list_once", AsyncMock(return_value=full)), patch.object(once.automations, "save", saved):
        kwargs = dict(title="x", due=due, kind="sleep", actions=ACTIONS, forbidden=FORBIDDEN, automation_id=None)
        with pytest.raises(HomeAssistantError, match="too many"):
            await once.save(hass, owner="anna", area="wohnzimmer", **kwargs)
        saved.assert_not_called()
        # Another person, another room and a finished timer do not count.
        await once.save(hass, owner="ben", area="wohnzimmer", **kwargs)
        await once.save(hass, owner="anna", area="kueche", **kwargs)
        assert saved.await_count == 2
        # Extending an existing one replaces it, whatever the count.
        await once.save(hass, owner="anna", area="wohnzimmer", **(kwargs | {"automation_id": "t3"}))
        assert saved.await_args.args[2] == "t3" and saved.await_args.args[4] == once.LABEL_ONCE


async def test_no_timer_in_the_past_or_without_actions(hass):
    with patch.object(once, "list_once", AsyncMock(return_value=[])):
        base = dict(owner="", area="", kind="sleep", forbidden=None, automation_id=None)
        with pytest.raises(HomeAssistantError, match="past"):
            await once.save(hass, title="x", due=(dt_util.now() - timedelta(minutes=1)).isoformat(), actions=ACTIONS, **base)
        with pytest.raises(HomeAssistantError, match="time zone"):
            await once.save(hass, title="x", due="2099-01-01T10:00:00", actions=ACTIONS, **base)
        with pytest.raises(HomeAssistantError, match="at least one"):
            await once.save(hass, title="x", due=(dt_util.now() + timedelta(hours=1)).isoformat(), actions=[], **base)


async def test_the_sweep_removes_only_what_ran_and_is_finished(hass):
    timers = [
        _timer("waiting"),                      # not due, never ran: stays
        _timer("late"),                         # due, never ran: catches up by itself, stays
        _timer("fading", ran=True, running=1),  # ran, still fading out: stays
        _timer("done", ran=True),               # ran, finished: goes
    ]
    delete = AsyncMock()
    with patch.object(once, "list_once", AsyncMock(return_value=timers)), patch.object(once.automations, "delete", delete):
        await once.sweep(hass)
    delete.assert_awaited_once_with(hass, "done")


async def test_only_one_time_automations_can_be_deleted_here(hass):
    delete = AsyncMock()
    with patch.object(once, "list_once", AsyncMock(return_value=[_timer("t1")])), patch.object(once.automations, "delete", delete):
        await once.delete(hass, "t1")
        with pytest.raises(HomeAssistantError):
            await once.delete(hass, "a-task")
    delete.assert_awaited_once_with(hass, "t1")


async def test_list_reads_the_variables_and_the_run_state(hass):
    item = {
        "id": "t1", "entity_id": "automation.t1", "last_triggered": None, "current": 0,
        "config": once.build("Schlaf-Timer", dt_util.now() + timedelta(minutes=5), "anna", "bad", "sleep", ACTIONS, "abcd1234"),
    }
    other = {"id": "x", "entity_id": "automation.x", "config": {"alias": "no variables"}}
    with patch.object(once.automations, "list_tasks", AsyncMock(return_value=[item, other])):
        timers = await once.list_once(hass)
    assert [(t["id"], t["title"], t["owner"], t["area"], t["kind"], t["ran"], t["running"]) for t in timers] == [
        ("t1", "Schlaf-Timer", "anna", "bad", "sleep", False, 0)
    ]


async def test_commands_only_for_the_agent(hass, aioclient_mock, entry, hass_ws_client, hass_read_only_access_token):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await async_setup_component(hass, "automation", {})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    admin = await hass_ws_client(hass)
    await admin.send_json_auto_id({"type": "mira/once/list"})
    msg = await admin.receive_json()
    assert msg["success"] and msg["result"] == {"timers": []}
    other = await hass_ws_client(hass, hass_read_only_access_token)
    await other.send_json_auto_id({"type": "mira/once/list"})
    msg = await other.receive_json()
    assert not msg["success"] and msg["error"]["code"] == "unauthorized"
