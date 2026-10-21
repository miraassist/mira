"""Mira's agents from Home Assistant (docs/spec/agenten.md, Calling).

mira.agent calls an agent, synchronously with its answer for the
automation's response_variable, or asynchronously with a call id;
mira.agent_result reads a run, mira.agent_cancel ends one. When an
asynchronous run is done, the agent sends mira/agent/done and the
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

from . import automations
from .api import MiraError
from .const import DOMAIN

EVENT_AGENT_DONE = "mira_agent_done"

SERVICE_AGENT = "agent"
SERVICE_AGENT_RESULT = "agent_result"
SERVICE_AGENT_CANCEL = "agent_cancel"

AGENT_SCHEMA = vol.Schema(
    {
        vol.Required("agent"): cv.string,
        vol.Optional("input", default={}): dict,
        vol.Optional("mode", default="sync"): vol.In(["sync", "async"]),
        vol.Optional("timeout", default=60): vol.All(vol.Coerce(float), vol.Range(min=1, max=3600)),
        vol.Optional("on_timeout", default="cancel"): vol.In(["cancel", "run_on"]),
        vol.Optional("delivery", default="event"): vol.In(["event", "message"]),
        vol.Optional("receiver"): cv.string,
    }
)
CALL_ID_SCHEMA = vol.Schema({vol.Required("call_id"): cv.string})


@callback
def async_register_agent_services(hass: HomeAssistant, loaded: Callable[[], ConfigEntry]) -> None:
    """mira.agent, mira.agent_result, mira.agent_cancel and the command that
    fires mira_agent_done."""

    async def agent(call: ServiceCall) -> ServiceResponse:
        try:
            return await loaded().runtime_data.client.agent(dict(call.data))
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent could not run {call.data['agent']}: {err}") from err

    async def agent_result(call: ServiceCall) -> ServiceResponse:
        try:
            return await loaded().runtime_data.client.agent_result(call.data["call_id"])
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent has no such run: {err}") from err

    async def agent_cancel(call: ServiceCall) -> ServiceResponse:
        try:
            return await loaded().runtime_data.client.agent_cancel(call.data["call_id"])
        except MiraError as err:
            raise HomeAssistantError(f"Mira agent could not cancel the run: {err}") from err

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
@callback
def _ws_done(hass: HomeAssistant, connection: websocket_api.ActiveConnection, msg: dict[str, Any]) -> None:
    if not automations._agent_only(hass, connection, msg):
        return
    data = {"call_id": msg["call_id"], "agent": msg["agent"], "status": msg["status"], "output": _output(msg["output"])}
    if msg["error"]:
        data["error"] = msg["error"]
    hass.bus.async_fire(EVENT_AGENT_DONE, data)
    connection.send_result(msg["id"], {"fired": True})
