"""Tasks as Home Assistant automations.

A Mira task ("Achte auf den Wasserverbrauch") is an ordinary automation:
Home Assistant's triggers and conditions decide when, Mira's actions
(mira.assess, mira.add_notice) decide what is worth saying and say it. The
automation is the source of truth; it carries the label "Mira" and is edited
in Home Assistant's automation editor. The agent only keeps a reference.

The agent runs as a restricted user and may not write automations. The
integration does it for the agent, through these websocket commands, and
only for the agent's own user. It saves only automations whose actions call
Mira and nothing else: a task observes and speaks, it never switches the
house.

Saving takes the automation editor's path: validate the item, replace it in
automations.yaml, reload that one automation.
"""

from __future__ import annotations

import asyncio
from typing import Any
import uuid

import voluptuous as vol

from homeassistant.components import websocket_api
from homeassistant.components.automation.config import async_validate_config_item
from homeassistant.config import AUTOMATION_CONFIG_PATH
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import entity_registry as er, label_registry as lr
from homeassistant.util.file import write_utf8_file_atomic
from homeassistant.util.yaml import dump, load_yaml

from .const import CONF_AGENT_USER, DOMAIN

LABEL = "Mira"
_LOCK = asyncio.Lock()

# Steps that only structure a run; anything else must be a call to Mira.
_STRUCTURE = {"variables", "condition", "delay", "stop", "wait_template", "wait_for_trigger"}


class NotAllowed(HomeAssistantError):
    """The automation does something other than asking Mira."""


def check_actions(actions: Any) -> None:
    """Raise NotAllowed unless every action calls a mira.* action, possibly
    inside choose/if/sequence/parallel."""
    if isinstance(actions, dict):
        actions = [actions]
    for step in actions or []:
        if not isinstance(step, dict):
            raise NotAllowed(f"unexpected action {step!r}")
        if "action" in step or "service" in step:
            name = str(step.get("action") or step.get("service"))
            if not name.startswith(f"{DOMAIN}."):
                raise NotAllowed(f"only {DOMAIN}.* actions are allowed, not {name}")
            continue
        if "choose" in step:
            for option in step["choose"] or []:
                check_actions(option.get("sequence"))
            check_actions(step.get("default"))
            continue
        if "if" in step:
            check_actions(step.get("then"))
            check_actions(step.get("else"))
            continue
        if "sequence" in step:
            check_actions(step["sequence"])
            continue
        if "parallel" in step:
            check_actions(step["parallel"])
            continue
        if _STRUCTURE & step.keys():
            continue
        raise NotAllowed(f"unsupported action {step!r}")


async def validate(hass: HomeAssistant, config: dict[str, Any]) -> None:
    """Home Assistant's own validation, then Mira's action rule."""
    try:
        await async_validate_config_item(hass, "mira_check", config)
    except (vol.Invalid, HomeAssistantError) as err:
        raise HomeAssistantError(str(err)) from err
    except Exception as err:  # probatio.Invalid in newer cores
        raise HomeAssistantError(str(err)) from err
    check_actions(config.get("actions", config.get("action")))


def _read(path: str) -> list[dict[str, Any]]:
    try:
        data = load_yaml(path)
    except FileNotFoundError:
        return []
    return list(data or [])


def _write(path: str, data: list[dict[str, Any]]) -> None:
    write_utf8_file_atomic(path, dump(data))


async def save(hass: HomeAssistant, config: dict[str, Any], automation_id: str | None) -> dict[str, Any]:
    """Validate, write and reload one automation; label it "Mira"."""
    await validate(hass, config)
    automation_id = automation_id or f"mira_{uuid.uuid4().hex[:12]}"
    item = {"id": automation_id} | {k: v for k, v in config.items() if k != "id"}
    path = hass.config.path(AUTOMATION_CONFIG_PATH)
    async with _LOCK:
        data = await hass.async_add_executor_job(_read, path)
        for index, current in enumerate(data):
            if current.get("id") == automation_id:
                data[index] = item
                break
        else:
            data.append(item)
        await hass.async_add_executor_job(_write, path, data)
    await hass.services.async_call("automation", "reload", {"id": automation_id}, blocking=True)
    entity_id = er.async_get(hass).async_get_entity_id("automation", "automation", automation_id)
    if entity_id is None:
        raise HomeAssistantError(
            "the automation was written but not loaded; does configuration.yaml include automations.yaml?"
        )
    _label(hass, entity_id)
    return {"id": automation_id, "entity_id": entity_id}


@callback
def _label(hass: HomeAssistant, entity_id: str) -> None:
    labels = lr.async_get(hass)
    label = labels.async_get_label_by_name(LABEL) or labels.async_create(
        LABEL, icon="mdi:robot-happy-outline", color="teal", description="Aufgaben von Mira"
    )
    entities = er.async_get(hass)
    entry = entities.async_get(entity_id)
    if entry is not None and label.label_id not in entry.labels:
        entities.async_update_entity(entity_id, labels=entry.labels | {label.label_id})


async def delete(hass: HomeAssistant, automation_id: str) -> None:
    """Remove one of Mira's automations."""
    path = hass.config.path(AUTOMATION_CONFIG_PATH)
    async with _LOCK:
        data = await hass.async_add_executor_job(_read, path)
        kept = [a for a in data if a.get("id") != automation_id]
        if len(kept) == len(data):
            raise HomeAssistantError(f"no automation {automation_id}")
        await hass.async_add_executor_job(_write, path, kept)
    entities = er.async_get(hass)
    if entity_id := entities.async_get_entity_id("automation", "automation", automation_id):
        entities.async_remove(entity_id)
    await hass.services.async_call("automation", "reload", {}, blocking=True)


async def list_tasks(hass: HomeAssistant) -> list[dict[str, Any]]:
    """Mira's automations: the ones labelled "Mira", with their config."""
    label = lr.async_get(hass).async_get_label_by_name(LABEL)
    if label is None:
        return []
    entities = er.async_get(hass)
    path = hass.config.path(AUTOMATION_CONFIG_PATH)
    configs = {a.get("id"): a for a in await hass.async_add_executor_job(_read, path)}
    out = []
    for entry in er.async_entries_for_label(entities, label.label_id):
        if entry.domain != "automation":
            continue
        state = hass.states.get(entry.entity_id)
        out.append(
            {
                "id": entry.unique_id,
                "entity_id": entry.entity_id,
                "state": state.state if state else "unavailable",
                "last_triggered": (
                    state.attributes.get("last_triggered").isoformat()
                    if state and state.attributes.get("last_triggered")
                    else None
                ),
                "config": configs.get(entry.unique_id),
            }
        )
    return out


def _agent_only(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> bool:
    """Only the agent's own user, or an admin, may use these commands."""
    agents = {e.data.get(CONF_AGENT_USER) for e in hass.config_entries.async_entries(DOMAIN)}
    if connection.user.is_admin or connection.user.id in agents:
        return True
    connection.send_error(msg["id"], "unauthorized", "only the Mira agent may manage its automations")
    return False


@websocket_api.websocket_command({vol.Required("type"): "mira/automation/validate", vol.Required("config"): dict})
@websocket_api.async_response
async def _ws_validate(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    try:
        await validate(hass, msg["config"])
    except HomeAssistantError as err:
        connection.send_result(msg["id"], {"valid": False, "error": str(err)})
        return
    connection.send_result(msg["id"], {"valid": True})


@websocket_api.websocket_command(
    {
        vol.Required("type"): "mira/automation/save",
        vol.Required("config"): dict,
        vol.Optional("automation_id"): str,
    }
)
@websocket_api.async_response
async def _ws_save(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    try:
        result = await save(hass, msg["config"], msg.get("automation_id"))
    except HomeAssistantError as err:
        connection.send_error(msg["id"], "invalid", str(err))
        return
    connection.send_result(msg["id"], result)


@websocket_api.websocket_command({vol.Required("type"): "mira/automation/delete", vol.Required("automation_id"): str})
@websocket_api.async_response
async def _ws_delete(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    try:
        await delete(hass, msg["automation_id"])
    except HomeAssistantError as err:
        connection.send_error(msg["id"], "not_found", str(err))
        return
    connection.send_result(msg["id"], {"deleted": True})


@websocket_api.websocket_command({vol.Required("type"): "mira/automation/list"})
@websocket_api.async_response
async def _ws_list(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    connection.send_result(msg["id"], {"automations": await list_tasks(hass)})


@callback
def async_register_commands(hass: HomeAssistant) -> None:
    """Register the agent's automation commands."""
    for command in (_ws_validate, _ws_save, _ws_delete, _ws_list):
        websocket_api.async_register_command(hass, command)
