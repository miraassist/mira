"""One-time automations: "in 30 minutes, stop the music and turn off the
light" (docs/spec/geplante-aktionen.md).

An ordinary Home Assistant automation with the label "Mira: einmalig". It
fires at the due time and again when Home Assistant starts, but its condition
is "not before the due time, and never ran" – so a timer that was due while
Home Assistant was down runs as soon as it is back, and never twice. The due
time, the owner and the room stand in the automation's variables. When it
has run, the integration removes it (sweep, every minute with the status
poll). Not a task: it does not carry the label "Mira".

The agent calls these websocket commands, like it does for tasks. A timer
may not switch what needs an explicit yes (locks, alarm, garage door), and
a person has at most ten of them per room.
"""

from __future__ import annotations

from datetime import datetime, timedelta
import logging
from typing import Any
import uuid

import voluptuous as vol

from homeassistant.components import websocket_api
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.util import dt as dt_util

from . import automations
from .labels import LABEL_ONCE

_LOGGER = logging.getLogger(__name__)

# At most this many open timers for one person in one room.
MAX_PER_OWNER_AREA = 10
# The clock may be a moment behind the trigger.
_SLACK = timedelta(seconds=2)


def build(
    title: str, due: datetime, owner: str, area: str, kind: str, actions: list[dict[str, Any]], tag: str = ""
) -> dict[str, Any]:
    """The automation for one timer.

    The alias ends in a tag that is unique to the timer. Home Assistant makes
    the entity id from the alias and restores a new entity's state from the
    last one with that id, even a removed one: a second "Schlaf-Timer
    Wohnzimmer" would inherit "already ran" from the first and never run.
    """
    local = dt_util.as_local(due).replace(microsecond=0)
    stamp = local.isoformat()
    return {
        "alias": f"{title} {tag}".strip(),
        "description": f"Mira: {title}",
        "variables": {
            "mira_title": title, "mira_due": stamp, "mira_owner": owner, "mira_area": area, "mira_kind": kind,
        },
        "triggers": [
            {"trigger": "time", "at": local.strftime("%H:%M:%S")},
            {"trigger": "homeassistant", "event": "start"},
        ],
        "conditions": [
            {
                "condition": "template",
                "value_template": (
                    "{{ now() >= as_datetime('%s') - timedelta(seconds=%d) "
                    "and this.attributes.last_triggered is none }}" % (stamp, int(_SLACK.total_seconds()))
                ),
            }
        ],
        "actions": actions,
        "mode": "single",
    }


async def list_once(hass: HomeAssistant) -> list[dict[str, Any]]:
    """The one-time automations, with what the variables say."""
    out = []
    for item in await automations.list_tasks(hass, LABEL_ONCE):
        config = item.get("config") or {}
        variables = config.get("variables") or {}
        if not variables.get("mira_due"):
            continue
        out.append(
            {
                "id": item["id"],
                "entity_id": item["entity_id"],
                "title": variables.get("mira_title") or config.get("alias", ""),
                "due": variables["mira_due"],
                "owner": variables.get("mira_owner", ""),
                "area": variables.get("mira_area", ""),
                "kind": variables.get("mira_kind", ""),
                "actions": config.get("actions") or [],
                "ran": bool(item.get("last_triggered")),
                "running": item.get("current", 0),
            }
        )
    return out


async def save(
    hass: HomeAssistant,
    *,
    title: str,
    due: str,
    owner: str,
    area: str,
    kind: str,
    actions: list[dict[str, Any]],
    forbidden: dict[str, Any] | None,
    automation_id: str | None,
) -> dict[str, Any]:
    """Create a timer, or replace one (extending it)."""
    when = dt_util.parse_datetime(due)
    if when is None or when.tzinfo is None:
        raise HomeAssistantError("due must be a date and time with a time zone")
    if when <= dt_util.now():
        raise HomeAssistantError("due is in the past")
    if not actions:
        raise HomeAssistantError("a timer needs at least one action")
    current = await list_once(hass)
    if automation_id is None:
        same = [t for t in current if t["owner"] == owner and t["area"] == area and not t["ran"]]
        if len(same) >= MAX_PER_OWNER_AREA:
            raise HomeAssistantError(f"too many timers: at most {MAX_PER_OWNER_AREA} per person and room")
    elif not any(t["id"] == automation_id for t in current):
        raise HomeAssistantError(f"no timer {automation_id}")
    automation_id = automation_id or f"mira_{uuid.uuid4().hex[:12]}"
    config = build(title, when, owner, area, kind, actions, automation_id[-8:])
    return await automations.save(hass, config, automation_id, forbidden, LABEL_ONCE)


async def delete(hass: HomeAssistant, automation_id: str) -> None:
    """Remove a timer; only a one-time automation, never a task."""
    if not any(t["id"] == automation_id for t in await list_once(hass)):
        raise HomeAssistantError(f"no timer {automation_id}")
    await automations.delete(hass, automation_id)


async def sweep(hass: HomeAssistant) -> None:
    """Remove what has run and is finished. Nothing else is touched: what is
    due and has not run catches up by itself, what is not due waits."""
    for timer in await list_once(hass):
        if timer["ran"] and not timer["running"]:
            try:
                await automations.delete(hass, timer["id"])
            except HomeAssistantError as err:
                _LOGGER.warning("Could not remove the finished timer %s: %s", timer["title"], err)


_FORBIDDEN = vol.Schema({vol.Optional("domains"): [str], vol.Optional("entities"): [str]})


@websocket_api.websocket_command(
    {
        vol.Required("type"): "mira/once/save",
        vol.Required("title"): str,
        vol.Required("due"): str,
        vol.Optional("owner", default=""): str,
        vol.Optional("area", default=""): str,
        vol.Optional("kind", default="timer"): str,
        vol.Required("actions"): [dict],
        vol.Optional("forbidden"): _FORBIDDEN,
        vol.Optional("automation_id"): str,
    }
)
@websocket_api.async_response
async def _ws_save(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not automations._agent_only(hass, connection, msg):
        return
    try:
        result = await save(
            hass,
            title=msg["title"],
            due=msg["due"],
            owner=msg["owner"],
            area=msg["area"],
            kind=msg["kind"],
            actions=msg["actions"],
            forbidden=msg.get("forbidden"),
            automation_id=msg.get("automation_id"),
        )
    except HomeAssistantError as err:
        connection.send_error(msg["id"], "invalid", str(err))
        return
    connection.send_result(msg["id"], result)


@websocket_api.websocket_command({vol.Required("type"): "mira/once/list"})
@websocket_api.async_response
async def _ws_list(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not automations._agent_only(hass, connection, msg):
        return
    connection.send_result(msg["id"], {"timers": await list_once(hass)})


@websocket_api.websocket_command({vol.Required("type"): "mira/once/delete", vol.Required("automation_id"): str})
@websocket_api.async_response
async def _ws_delete(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not automations._agent_only(hass, connection, msg):
        return
    try:
        await delete(hass, msg["automation_id"])
    except HomeAssistantError as err:
        connection.send_error(msg["id"], "not_found", str(err))
        return
    connection.send_result(msg["id"], {"deleted": True})


@callback
def async_register_commands(hass: HomeAssistant) -> None:
    for command in (_ws_save, _ws_list, _ws_delete):
        websocket_api.async_register_command(hass, command)
