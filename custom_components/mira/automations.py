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
nothing that runs arbitrary code or changes Home Assistant itself (shell
commands, restarts). What an action switches is learnt from Home Assistant:
it resolves areas, devices, floors, labels and groups to entities, and an
action whose description names no entities as its target (mqtt.publish,
rest_command, scene.apply) could switch anything, so a task calls it only
inside a script exposed to Assist. Those go into an automation only through
Home Assistant's own editor.

Saving takes the automation editor's path: validate the item, replace it in
automations.yaml, reload that one automation. Mira may also change the
household's own tasks, the ones the editor saved: she reads one with get,
finds them with list (all), and saves them keeping their labels. An action
that was in the task before and stays unchanged is the admin's decision and
is not checked again; what is new or changed is.
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
import json
import re
from typing import Any
import uuid

import voluptuous as vol

from homeassistant.components import websocket_api
from homeassistant.components.homeassistant.exposed_entities import async_should_expose
from homeassistant.components.automation.config import async_validate_config_item
from homeassistant.config import AUTOMATION_CONFIG_PATH
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import (
    area_registry as ar,
    device_registry as dr,
    entity_registry as er,
    label_registry as lr,
)
from homeassistant.helpers.service import async_get_all_descriptions
from homeassistant.helpers.target import TargetSelection, async_extract_referenced_entity_ids
from homeassistant.util.file import write_utf8_file_atomic
from homeassistant.util.yaml import dump, load_yaml

from .const import CONF_AGENT_USER, DOMAIN
from .labels import LABEL_CONFIRM, LABEL_TASKS, ensure_label
_LOCK = asyncio.Lock()

# Domains a task never calls: they run arbitrary code or change Home
# Assistant itself.
_NEVER = {
    "shell_command", "python_script", "pyscript", "hassio", "homeassistant",
    "automation", "recorder", "backup", "update", "system_log", "logger",
}
# Actions without entities as their target that a task may still call: they
# only tell someone something.
_UNTARGETED = {"notify", "persistent_notification"}
# How deep a scene or group is followed into its members.
_MEMBER_DEPTH = 3


class NotAllowed(HomeAssistantError):
    """The automation does something a task may not do."""


@dataclass(frozen=True)
class _Rule:
    """What a task may not reach, and Home Assistant to learn what an action
    reaches."""

    hass: HomeAssistant
    services: dict[str, dict[str, Any]]
    domains: set[str]
    entities: set[str]
    # The steps the task had before, as _key gives them: not checked again.
    known: frozenset[str] = field(default_factory=frozenset)


_TEMPLATE = re.compile(r"\{[{%#]")
# Steps that cannot switch anything: they wait, set a variable, stop, or test.
_HARMLESS = ("condition", "delay", "wait_template", "wait_for_trigger", "variables", "stop", "set_conversation_response")
_TARGET_KEYS = ("entity_id", "area_id", "device_id", "floor_id", "label_id")
_BROAD_KEYS = ("area_id", "device_id", "floor_id", "label_id")


def _templated(value: Any) -> bool:
    """Whether a value is, or holds, a template."""
    if isinstance(value, str):
        return bool(_TEMPLATE.search(value))
    if isinstance(value, dict):
        return any(_templated(v) for v in value.values())
    if isinstance(value, (list, tuple)):
        return any(_templated(v) for v in value)
    return False


def _key(step: Any) -> str:
    """A step as one string, to compare it with the steps a task had."""
    return json.dumps(step, sort_keys=True, default=str)


def _steps(actions: Any) -> set[str]:
    """Every step of actions, nested ones too, as _key gives them."""
    if isinstance(actions, dict):
        actions = [actions]
    if not isinstance(actions, (list, tuple)):
        return set()
    out: set[str] = set()
    for step in actions:
        if not isinstance(step, dict):
            continue
        out.add(_key(step))
        for key in ("then", "else", "sequence", "parallel", "default"):
            out |= _steps(step.get(key))
        for option in step.get("choose") or []:
            if isinstance(option, dict):
                out |= _steps(option.get("sequence"))
        if isinstance(step.get("repeat"), dict):
            out |= _steps(step["repeat"].get("sequence"))
    return out


async def check_actions(
    hass: HomeAssistant,
    actions: Any,
    forbidden_domains: set[str] = frozenset(),
    forbidden_entities: set[str] = frozenset(),
    before: Any = None,
) -> None:
    """Raise NotAllowed if an action calls what a task may not: a domain in
    _NEVER or forbidden_domains, an entity in forbidden_entities or reaching
    one (through an area, device, floor, label, group or scene), a script not
    exposed to Assist, or an action that names no entities as its target –
    that one could switch anything; it belongs in a script.

    The check is complete or it refuses: it follows every step Home
    Assistant knows (if, choose, repeat, parallel, sequence), the name of an
    action and its targets must be written out, not a template, and a step it
    does not understand is refused. A template could name what the check is
    there to keep out.

    A step that was among before (the task's actions until now) unchanged is
    not checked again: it is the admin's decision."""
    rule = _Rule(
        hass,
        await async_get_all_descriptions(hass),
        set(forbidden_domains),
        set(forbidden_entities),
        frozenset(_steps(before)),
    )
    _check_actions(rule, actions)


def _check_actions(rule: _Rule, actions: Any) -> None:
    if isinstance(actions, dict):
        actions = [actions]
    if actions is None:
        return
    if not isinstance(actions, (list, tuple)):
        raise NotAllowed(f"unexpected actions {actions!r}")
    for step in actions:
        _check_step(rule, step)


def _check_step(rule: _Rule, step: Any) -> None:
    if not isinstance(step, dict):
        raise NotAllowed(f"unexpected action {step!r}")
    if _key(step) in rule.known:
        return
    if "service_template" in step:
        raise NotAllowed("the action of a task must be written out, not a template")
    name = step.get("action") or step.get("service")
    if name is not None:
        _check_call(rule, step, name)
        return
    if "scene" in step:
        if not isinstance(step["scene"], str) or _templated(step["scene"]):
            raise NotAllowed("the scene of a task must be written out, not a template")
        _check_entities(rule, {step["scene"]})
        return
    if "domain" in step and "device_id" in step:
        _check_device_action(rule, step)
        return
    if "event" in step:
        raise NotAllowed("a task may not fire events")
    nested = False
    for key in ("then", "else", "sequence", "parallel", "default"):
        if key in step:
            nested = True
            _check_actions(rule, step[key])
    if "choose" in step:
        nested = True
        for option in step["choose"] or []:
            if not isinstance(option, dict):
                raise NotAllowed(f"unexpected choice {option!r}")
            _check_actions(rule, option.get("sequence"))
    if "repeat" in step:
        nested = True
        repeat = step["repeat"]
        if not isinstance(repeat, dict):
            raise NotAllowed(f"unexpected repeat {repeat!r}")
        _check_actions(rule, repeat.get("sequence"))
    if nested or "if" in step or any(key in step for key in _HARMLESS):
        return
    raise NotAllowed(f"unexpected action {step!r}")


def _check_call(rule: _Rule, step: dict[str, Any], name: Any) -> None:
    if not isinstance(name, str) or _templated(name):
        raise NotAllowed("the action of a task must be written out, not a template")
    domain, _, service = name.partition(".")
    if domain == DOMAIN:
        return
    if domain in _NEVER or domain in rule.domains:
        raise NotAllowed(f"a task may not call {name}")
    target = _target(step, name)
    described = rule.services.get(domain, {}).get(service)
    if domain == "script" and (described is None or "target" not in described):
        # A script called by its name: it is what it reaches.
        target.setdefault("entity_id", []).append(name)
    elif (described is None or "target" not in described) and domain not in _UNTARGETED:
        raise NotAllowed(
            f"a task may not call {name}: it names no entities, so it could switch anything;"
            " put it in a script and expose that to Assist"
        )
    broad = "all" in target.get("entity_id", []) or any(target.get(key) for key in _BROAD_KEYS)
    if broad and (domain in rule.domains or any(e.split(".", 1)[0] == domain for e in rule.entities)):
        raise NotAllowed(f"{name} on areas or devices might reach a protected entity; name the entities")
    selected = async_extract_referenced_entity_ids(rule.hass, TargetSelection(target), primary_entities_only=False)
    # In an area or on a device an action switches its own domain's
    # entities only, as Home Assistant does: a light in the hall, not the
    # lock there.
    indirect = {e for e in selected.indirectly_referenced if e.split(".", 1)[0] == domain}
    _check_entities(rule, selected.referenced | indirect)


def _target(step: dict[str, Any], name: str) -> dict[str, list[str]]:
    """Where an action points, gathered as Home Assistant does: its target,
    its data (data_template too) and an entity_id on the step itself."""
    target: dict[str, list[str]] = {}
    holders = [(key, step.get(key)) for key in ("target", "data", "data_template")]
    holders.append(("step", {"entity_id": step["entity_id"]} if "entity_id" in step else None))
    for key, holder in holders:
        if holder is None:
            continue
        if not isinstance(holder, dict):
            raise NotAllowed(f"the {key} of {name} must be written out, not a template")
        for target_key in _TARGET_KEYS:
            value = holder.get(target_key)
            if value is None:
                continue
            if _templated(value):
                raise NotAllowed(f"the targets of {name} must be written out, not a template")
            ids = value.split(",") if isinstance(value, str) else value if isinstance(value, list) else [value]
            target.setdefault(target_key, []).extend(str(v).strip() for v in ids)
    return target


def _check_device_action(rule: _Rule, step: dict[str, Any]) -> None:
    """A device action names the entity it switches; one without (an
    integration's own action on the device) could do anything."""
    if step["domain"] in _NEVER or step["domain"] in rule.domains:
        raise NotAllowed(f"a task may not use {step['domain']} devices")
    entity = step.get("entity_id")
    if not isinstance(entity, str) or _templated(entity):
        raise NotAllowed(
            f"a task may use a {step['domain']} device only through one of its entities;"
            " put the action in a script and expose that to Assist"
        )
    entity_id = er.async_resolve_entity_id(er.async_get(rule.hass), entity)
    if entity_id is None:
        raise NotAllowed(f"unknown entity {entity}")
    _check_entities(rule, {entity_id})


def _check_entities(rule: _Rule, entity_ids: set[str], depth: int = 0) -> None:
    """Refuse a protected entity, a script not exposed to Assist, and a scene
    or group holding either."""
    for entity_id in entity_ids:
        domain = entity_id.split(".", 1)[0]
        if entity_id in rule.entities or domain in rule.domains:
            raise NotAllowed(f"a task may not switch {entity_id}; that needs an explicit yes each time")
        if domain == "script" and not _exposed(rule.hass, entity_id):
            raise NotAllowed(f"a task may run {entity_id} only once it is exposed to Assist")
        state = rule.hass.states.get(entity_id)
        members = state.attributes.get("entity_id") if state else None
        if isinstance(members, str):
            members = [members]
        if isinstance(members, list) and members:
            if depth >= _MEMBER_DEPTH:
                raise NotAllowed(f"{entity_id} nests too deep to check")
            _check_entities(rule, {str(m) for m in members}, depth + 1)


def _exposed(hass: HomeAssistant, entity_id: str) -> bool:
    try:
        return async_should_expose(hass, "conversation", entity_id)
    except KeyError:  # Home Assistant's exposed entities not loaded
        return False


async def validate(
    hass: HomeAssistant,
    config: dict[str, Any],
    forbidden: dict[str, Any] | None = None,
    before: dict[str, Any] | None = None,
) -> None:
    """Home Assistant's own validation, then the rule what a task may call;
    before is the task as it was, whose unchanged steps pass."""
    try:
        await async_validate_config_item(hass, "mira_check", config)
    except (vol.Invalid, HomeAssistantError) as err:
        raise HomeAssistantError(str(err)) from err
    except Exception as err:  # probatio.Invalid in newer cores
        raise HomeAssistantError(str(err)) from err
    forbidden = forbidden or {}
    # Entities labelled "Mira: nachfragen" need a yes each time, too.
    labelled = {e.entity_id for e in er.async_entries_for_label(er.async_get(hass), LABEL_CONFIRM)}
    await check_actions(
        hass,
        _actions(config),
        set(forbidden.get("domains") or []),
        set(forbidden.get("entities") or []) | labelled,
        _actions(before) if before else None,
    )


def _actions(config: dict[str, Any]) -> Any:
    return config.get("actions", config.get("action"))


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
    label_id: str | None = LABEL_TASKS,
) -> dict[str, Any]:
    """Validate, write and reload one automation; label it "Mira" (or the
    given label; None keeps its labels as they are). An automation saved
    before keeps its unchanged steps unchecked."""
    automation_id = automation_id or f"mira_{uuid.uuid4().hex[:12]}"
    item = {"id": automation_id} | {k: v for k, v in config.items() if k != "id"}
    path = hass.config.path(AUTOMATION_CONFIG_PATH)
    async with _LOCK:
        data = await hass.async_add_executor_job(_read, path)
        before = next((a for a in data if a.get("id") == automation_id), None)
        await validate(hass, config, forbidden, before)
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
    if label_id is not None:
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


class NotEditable(HomeAssistantError):
    """The automation is not one the editor saved."""


async def get_task(hass: HomeAssistant, entity_id: str) -> dict[str, Any]:
    """One automation's configuration as the editor saved it."""
    state = hass.states.get(entity_id)
    if state is None or state.domain != "automation":
        raise HomeAssistantError(f"no automation {entity_id}")
    automation_id = state.attributes.get("id")
    path = hass.config.path(AUTOMATION_CONFIG_PATH)
    for config in await hass.async_add_executor_job(_read, path):
        if automation_id and config.get("id") == automation_id:
            return {"id": automation_id, "entity_id": entity_id, "config": config}
    raise NotEditable(f"{entity_id} is not in automations.yaml; it is changed where it is defined")


async def list_all(hass: HomeAssistant) -> list[dict[str, Any]]:
    """Every automation, with name, area, labels and whether the editor
    saved it (only those Mira can change)."""
    entities = er.async_get(hass)
    devices = dr.async_get(hass)
    areas = ar.async_get(hass)
    path = hass.config.path(AUTOMATION_CONFIG_PATH)
    editable = {a.get("id") for a in await hass.async_add_executor_job(_read, path)}
    out = []
    for state in hass.states.async_all("automation"):
        entry = entities.async_get(state.entity_id)
        area_id = entry.area_id if entry else None
        if entry and area_id is None and entry.device_id and (device := devices.async_get(entry.device_id)):
            area_id = device.area_id
        area = areas.async_get_area(area_id) if area_id else None
        out.append(
            {
                "entity_id": state.entity_id,
                "id": state.attributes.get("id"),
                "name": state.name,
                "area": area.name if area else None,
                "labels": sorted(entry.labels) if entry else [],
                "state": state.state,
                "editable": bool(state.attributes.get("id")) and state.attributes.get("id") in editable,
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
    {
        vol.Required("type"): "mira/automation/validate",
        vol.Required("config"): dict,
        vol.Optional("forbidden"): _FORBIDDEN,
        # A change of an existing task: its unchanged steps are not checked
        # again, as when saving (docs/spec/aufgaben-aendern.md).
        vol.Optional("automation_id"): str,
    }
)
@websocket_api.async_response
async def _ws_validate(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    before = None
    if automation_id := msg.get("automation_id"):
        data = await hass.async_add_executor_job(_read, hass.config.path(AUTOMATION_CONFIG_PATH))
        before = next((a for a in data if a.get("id") == automation_id), None)
    try:
        await validate(hass, msg["config"], msg.get("forbidden"), before)
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
        vol.Optional("keep_labels", default=False): bool,
    }
)
@websocket_api.async_response
async def _ws_save(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    label = None if msg["keep_labels"] else LABEL_TASKS
    try:
        result = await save(hass, msg["config"], msg.get("automation_id"), msg.get("forbidden"), label)
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


@websocket_api.websocket_command({vol.Required("type"): "mira/automation/get", vol.Required("entity_id"): str})
@websocket_api.async_response
async def _ws_get(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    try:
        result = await get_task(hass, msg["entity_id"])
    except NotEditable as err:
        connection.send_error(msg["id"], "not_editable", str(err))
        return
    except HomeAssistantError as err:
        connection.send_error(msg["id"], "not_found", str(err))
        return
    connection.send_result(msg["id"], result)


@websocket_api.websocket_command(
    {vol.Required("type"): "mira/automation/list", vol.Optional("all", default=False): bool}
)
@websocket_api.async_response
async def _ws_list(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not _agent_only(hass, connection, msg):
        return
    if msg["all"]:
        connection.send_result(msg["id"], {"automations": await list_all(hass)})
        return
    connection.send_result(msg["id"], {"automations": await list_tasks(hass)})


@callback
def async_register_commands(hass: HomeAssistant) -> None:
    """Register the agent's automation commands."""
    for command in (_ws_validate, _ws_save, _ws_delete, _ws_list, _ws_get):
        websocket_api.async_register_command(hass, command)
