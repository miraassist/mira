"""Mira's agents from Home Assistant (docs/spec/agents.md, Calling).

mira.agent calls an agent, synchronously with its answer for the
automation's response_variable, or asynchronously with a call id; with
skill instead of agent it starts that skill's thread
(docs/spec/remote-agents.md, F);
mira.agent_result reads a run, mira.agent_cancel ends one. When an
asynchronous run is done, the service sends mira/agent/done and the
integration fires the event mira_agent_done: Mira's own user may not fire
events, and no other user can forge it this way.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from typing import Any

import voluptuous as vol
from homeassistant.components import websocket_api
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import config_validation as cv

from .api import MiraError, MiraNotFound
from .const import DOMAIN

EVENT_AGENT_DONE = "mira_agent_done"

SERVICE_AGENT = "agent"
SERVICE_AGENT_RESULT = "agent_result"
SERVICE_AGENT_CANCEL = "agent_cancel"

# The fields of a call of an agent and their defaults, sent along.
AGENT_DEFAULTS = {"input": {}, "mode": "sync", "timeout": 60, "on_timeout": "cancel", "delivery": "event"}
# Not allowed with skill (docs/spec/remote-agents.md, F).
NOT_WITH_SKILL = ("agent", "mode", "timeout", "on_timeout", "delivery")


def _agent_or_skill(data: dict[str, Any]) -> dict[str, Any]:
    """Exactly one of agent and skill; with skill only input and receiver,
    and the input a text or {"input": text}; for an agent the input is an
    object and the defaults go along."""
    if "skill" in data:
        extra = [k for k in NOT_WITH_SKILL if k in data]
        if extra:
            raise vol.Invalid(f"with skill, these fields are not allowed: {', '.join(extra)}")
        return data
    if "agent" not in data:
        raise vol.Invalid("name an agent or a skill")
    if not isinstance(data.get("input", {}), dict):
        raise vol.Invalid("input of an agent is an object")
    return {**AGENT_DEFAULTS, **data}


AGENT_SCHEMA = vol.All(
    vol.Schema(
        {
            vol.Optional("agent"): cv.string,
            vol.Optional("skill"): cv.string,
            vol.Optional("input"): vol.Any(dict, cv.string),
            vol.Optional("mode"): vol.In(["sync", "async"]),
            vol.Optional("timeout"): vol.All(vol.Coerce(float), vol.Range(min=1, max=3600)),
            vol.Optional("on_timeout"): vol.In(["cancel", "run_on"]),
            vol.Optional("delivery"): vol.In(["event", "message"]),
            vol.Optional("receiver"): cv.string,
        }
    ),
    _agent_or_skill,
)
CALL_ID_SCHEMA = vol.Schema({vol.Required("call_id"): cv.string})


@callback
def async_register_agent_services(hass: HomeAssistant, loaded: Callable[[], ConfigEntry]) -> None:
    """mira.agent, mira.agent_result, mira.agent_cancel and the command that
    fires mira_agent_done."""

    async def agent(call: ServiceCall) -> ServiceResponse:
        if "skill" in call.data:
            return await start_skill(call)
        try:
            return await loaded().runtime_data.client.agent(dict(call.data))
        except MiraError as err:
            raise HomeAssistantError(f"Mira could not run {call.data['agent']}: {err}") from err

    async def start_skill(call: ServiceCall) -> ServiceResponse:
        """mira.agent with skill: the skill's thread starts as if its owner
        had asked (docs/spec/remote-agents.md, F); a service older than
        that has no route for it (404) and the action says so."""
        try:
            return await loaded().runtime_data.client.start_skill(dict(call.data))
        except MiraNotFound as err:
            raise HomeAssistantError(translation_domain=DOMAIN, translation_key="skill_needs_update") from err
        except MiraError as err:
            raise HomeAssistantError(
                translation_domain=DOMAIN,
                translation_key="skill_failed",
                translation_placeholders={"skill": call.data["skill"], "error": str(err)},
            ) from err

    async def agent_result(call: ServiceCall) -> ServiceResponse:
        try:
            return await loaded().runtime_data.client.agent_result(call.data["call_id"])
        except MiraError as err:
            raise HomeAssistantError(f"Mira has no such run: {err}") from err

    async def agent_cancel(call: ServiceCall) -> ServiceResponse:
        try:
            return await loaded().runtime_data.client.agent_cancel(call.data["call_id"])
        except MiraError as err:
            raise HomeAssistantError(f"Mira could not cancel the run: {err}") from err

    hass.services.async_register(
        DOMAIN, SERVICE_AGENT, agent, schema=AGENT_SCHEMA, supports_response=SupportsResponse.OPTIONAL
    )
    hass.services.async_register(
        DOMAIN, SERVICE_AGENT_RESULT, agent_result, schema=CALL_ID_SCHEMA, supports_response=SupportsResponse.ONLY
    )
    hass.services.async_register(
        DOMAIN, SERVICE_AGENT_CANCEL, agent_cancel, schema=CALL_ID_SCHEMA, supports_response=SupportsResponse.OPTIONAL
    )
    websocket_api.async_register_command(hass, _ws_done)


def _output(text: str) -> Any:
    """A run's output for the automation: the JSON it is, or its text."""
    if not text:
        return None
    try:
        return json.loads(text)
    except ValueError:
        return text


@websocket_api.websocket_command(
    {
        vol.Required("type"): "mira/agent/done",
        vol.Required("call_id"): str,
        vol.Required("agent"): str,
        vol.Required("status"): str,
        vol.Optional("output", default=""): str,
        vol.Optional("error", default=""): str,
    }
)
@websocket_api.require_admin
@callback
def _ws_done(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    data = {"call_id": msg["call_id"], "agent": msg["agent"], "status": msg["status"], "output": _output(msg["output"])}
    if msg["error"]:
        data["error"] = msg["error"]
    hass.bus.async_fire(EVENT_AGENT_DONE, data)
    connection.send_result(msg["id"], {"fired": True})
