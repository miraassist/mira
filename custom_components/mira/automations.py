"""Tasks as Home Assistant automations.

A Mira task ("Achte auf den Wasserverbrauch") is an ordinary automation:
Home Assistant's triggers and conditions decide when, Mira's actions
(mira.assess, mira.send_message) decide what is worth saying and say it. The
automation is the source of truth; it carries the label "Mira" and is edited
in Home Assistant's automation editor. The agent only keeps a reference.

The agent runs as a restricted user and may not write automations. The
integration does it for the agent, through these websocket commands, and
only for the agent's own user. A task may switch and notify (the radio, a
push to the phone) – the user hears exactly which actions before saying
yes – but not what the agent itself only does after an explicit yes
(locks, alarm, garage door: its confirm list, sent with each command), and
nothing that runs arbitrary code or changes Home Assistant itself (scripts,
shell commands, restarts). Those go into an automation only through Home
Assistant's own editor.

Saving takes the automation editor's path: validate the item, replace it in
automations.yaml, reload that one automation.
"""

from __future__ import annotations

import asyncio
import re
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
from .labels import LABEL_CONFIRM, LABEL_TASKS, ensure_label
_LOCK = asyncio.Lock()

# Domains a task never calls: they run arbitrary code or change Home
# Assistant itself.
_NEVER = {
    "script", "shell_command", "python_script", "pyscript", "hassio", "homeassistant",
    "automation", "recorder", "backup", "update", "system_log", "logger",
}


class NotAllowed(HomeAssistantError):
    """The automation does something a task may not do."""


def _entities(step: dict[str, Any]) -> tuple[list[str], bool]:
    """The entity ids an action names, and whether it also targets areas,
    devices or labels (which may contain anything)."""
    ids: list[str] = []
    broad = False
    for holder in (step.get("target") or {}, step.get("data") or {}):
        if not isinstance(holder, dict):
            continue
        value = holder.get("entity_id")
        if isinstance(value, str):
            ids.extend(v.strip() for v in value.split(","))
        elif isinstance(value, list):
            ids.extend(str(v) for v in value)
        if any(holder.get(k) for k in ("area_id", "device_id", "floor_id", "label_id")):
            broad = True
    return ids, broad


_TEMPLATE = re.compile(r"\{[{%#]")
# Steps that cannot switch anything: they wait, set a variable, stop, or test.
_HARMLESS = ("condition", "delay", "wait_template", "wait_for_trigger", "variables", "stop", "set_conversation_response")
_TARGET_KEYS = ("entity_id", "area_id", "device_id", "floor_id", "label_id")


def _templated(value: Any) -> bool:
    """Whether a value is, or holds, a template."""
    if isinstance(value, str):
        return bool(_TEMPLATE.search(value))
    if isinstance(value, dict):
        return any(_templated(v) for v in value.values())
    if isinstance(value, (list, tuple)):
        return any(_templated(v) for v in value)
    return False


def check_actions(actions: Any, forbidden_domains: set[str] = frozenset(), forbidden_entities: set[str] = frozenset()) -> None:
    """Raise NotAllowed if an action calls what a task may not: a domain in
    _NEVER or forbidden_domains, an entity in forbidden_entities, or – when
    such entities are configured for the called domain – a broad target
    that might include one.

    The check is complete or it refuses: it follows every step Home
    Assistant knows (if, choose, repeat, parallel, sequence), the name of an
    action and its targets must be written out, not a template, and a step it
    does not understand is refused. A template could name what the check is
    there to keep out."""
    if isinstance(actions, dict):
        actions = [actions]
    if actions is None:
        return
    if not isinstance(actions, (list, tuple)):
        raise NotAllowed(f"unexpected actions {actions!r}")
    for step in actions:
        _check_step(step, forbidden_domains, forbidden_entities)


def _check_step(step: Any, forbidden_domains: set[str], forbidden_entities: set[str]) -> None:
    if not isinstance(step, dict):
        raise NotAllowed(f"unexpected action {step!r}")
    if "service_template" in step:
        raise NotAllowed("the action of a task must be written out, not a template")
    name = step.get("action") or step.get("service")
    if name is not None:
        _check_call(step, name, forbidden_domains, forbidden_entities)
        return
    if "scene" in step:
        if _templated(step["scene"]):
            raise NotAllowed("the scene of a task must be written out, not a template")
        return
    if "domain" in step and "device_id" in step:
        if step["domain"] in _NEVER or step["domain"] in forbidden_domains:
            raise NotAllowed(f"a task may not use {step['domain']} devices")
        return
    if "event" in step:
        raise NotAllowed("a task may not fire events")
    nested = False
    for key in ("then", "else", "sequence", "parallel", "default"):
        if key in step:
            nested = True
            check_actions(step[key], forbidden_domains, forbidden_entities)
    if "choose" in step:
        nested = True
        for option in step["choose"] or []:
            if not isinstance(option, dict):
                raise NotAllowed(f"unexpected choice {option!r}")
            check_actions(option.get("sequence"), forbidden_domains, forbidden_entities)
    if "repeat" in step:
        nested = True
        repeat = step["repeat"]
        if not isinstance(repeat, dict):
            raise NotAllowed(f"unexpected repeat {repeat!r}")
        check_actions(repeat.get("sequence"), forbidden_domains, forbidden_entities)
    if nested or "if" in step or any(key in step for key in _HARMLESS):
        return
    raise NotAllowed(f"unexpected action {step!r}")


def _check_call(step: dict[str, Any], name: Any, forbidden_domains: set[str], forbidden_entities: set[str]) -> None:
    if not isinstance(name, str) or _templated(name):
        raise NotAllowed("the action of a task must be written out, not a template")
    domain = name.split(".", 1)[0]
    if domain == DOMAIN:
        return
    if domain in _NEVER or domain in forbidden_domains:
        raise NotAllowed(f"a task may not call {name}")
    for holder in (step.get("target"), step.get("data")):
        if isinstance(holder, dict) and any(_templated(holder.get(key)) for key in _TARGET_KEYS):
            raise NotAllowed(f"the targets of {name} must be written out, not a template")
    ids, broad = _entities(step)
    for entity_id in ids:
        if entity_id == "all":
            broad = True
        elif entity_id in forbidden_entities or entity_id.split(".", 1)[0] in forbidden_domains:
            raise NotAllowed(f"a task may not switch {entity_id}; that needs an explicit yes each time")
    if broad and any(e.split(".", 1)[0] == domain for e in forbidden_entities):
        raise NotAllowed(f"{name} on areas or devices might reach a protected entity; name the entities")


async def validate(hass: HomeAssistant, config: dict[str, Any], forbidden: dict[str, Any] | None = None) -> None:
    """Home Assistant's own validation, then the rule what a task may call."""
    try:
        await async_validate_config_item(hass, "mira_check", config)
    except (vol.Invalid, HomeAssistantError) as err:
        raise HomeAssistantError(str(err)) from err
    except Exception as err:  # probatio.Invalid in newer cores
        raise HomeAssistantError(str(err)) from err
    forbidden = forbidden or {}
    # Entities labelled "Mira: nachfragen" need a yes each time, too.
    labelled = {e.entity_id for e in er.async_entries_for_label(er.async_get(hass), LABEL_CONFIRM)}
    check_actions(
        config.get("actions", config.get("action")),
        set(forbidden.get("domains") or []),
        set(forbidden.get("entities") or []) | labelled,
    )


def _read(path: str) -> list[dict[str, Any]]:
    try:
        data = load_yaml(path)
    except FileNotFoundError:
        return []
    return list(data or [])


def _write(path: str, data: list[dict[str, Any]]) -> None:
    write_utf8_file_atomic(path, dump(data))


async def save(
    hass: HomeAssistant,
    config: dict[str, Any],
    automation_id: str | None,
    forbidden: dict[str, Any] | None = None,
    label_id: str = LABEL_TASKS,
) -> dict[str, Any]:
    """Validate, write and reload one automation; label it "Mira" (or the
    given label)."""
    await validate(hass, config, forbidden)
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
    _label(hass, entity_id, label_id)
    return {"id": automation_id, "entity_id": entity_id}


@callback
def _label(hass: HomeAssistant, entity_id: str, label_id: str = LABEL_TASKS) -> None:
    label = ensure_label(hass, label_id)
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


async def list_tasks(hass: HomeAssistant, label_id: str = LABEL_TASKS) -> list[dict[str, Any]]:
    """Mira's automations: the ones labelled "Mira" (or the given label),
    with their config."""
    label = lr.async_get(hass).async_get_label(label_id)
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
                "current": int(state.attributes.get("current") or 0) if state else 0,
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


_FORBIDDEN = vol.Schema({vol.Optional("domains"): [str], vol.Optional("entities"): [str]})


@websocket_api.websocket_command(
    {vol.Required("type"): "mira/automation/validate", vol.Required("config"): dict, vol.Optional("forbidden"): _FORBIDDEN}
)
@websocket_api.async_response
async def _ws_validate(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    try:
        await validate(hass, msg["config"], msg.get("forbidden"))
    except HomeAssistantError as err:
        connection.send_result(msg["id"], {"valid": False, "error": str(err)})
        return
    connection.send_result(msg["id"], {"valid": True})


@websocket_api.websocket_command(
    {
        vol.Required("type"): "mira/automation/save",
        vol.Required("config"): dict,
        vol.Optional("automation_id"): str,
        vol.Optional("forbidden"): _FORBIDDEN,
    }
)
@websocket_api.async_response
async def _ws_save(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    try:
        result = await save(hass, msg["config"], msg.get("automation_id"), msg.get("forbidden"))
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
