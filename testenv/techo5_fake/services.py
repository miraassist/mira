"""The device's ESPHome actions: esphome.<node>_<action>, every one the real
device has (from the contract), with the same fields. The ones Mira uses
behave like the device; all others are accepted and logged."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from typing import Any

import voluptuous as vol

from homeassistant.core import CALLBACK_TYPE, HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers.service import async_set_service_schema

from .const import ESPHOME
from .device import FakeDevice, parse_duration

Handler = Callable[[FakeDevice, dict[str, Any]], Awaitable[dict[str, Any] | None] | dict[str, Any] | None]


def _alarm_set(**flags: bool) -> Handler:
    def handle(dev: FakeDevice, d: dict[str, Any]) -> None:
        if not d.get("time"):
            raise HomeAssistantError("time is required")
        dev.alarm_set(str(d["time"]), str(d.get("days") or ""), str(d.get("label") or ""), **flags)

    return handle


def _alarm_delete(dev: FakeDevice, d: dict[str, Any]) -> None:
    dev.alarm_delete(time=str(d.get("time") or ""), label=str(d.get("label") or ""))


def _alarm_delete_id(dev: FakeDevice, d: dict[str, Any]) -> None:
    dev.alarm_delete(id=str(d.get("id") or ""))


def _alarms_list(dev: FakeDevice, d: dict[str, Any]) -> dict[str, Any]:
    return dev.listing()


def _reminder_set(dev: FakeDevice, d: dict[str, Any]) -> dict[str, Any]:
    alarm = dev.alarm_set(str(d.get("time") or ""), str(d.get("days") or ""), str(d.get("label") or ""),
                          reminder=True, ring_on=list(d.get("ring_on") or []))
    return {"id": alarm["id"]}


def _snooze_for(dev: FakeDevice, d: dict[str, Any]) -> dict[str, Any]:
    if not dev.alarm_sounding:
        return {"snoozed": False}
    dev.stop_sounding()
    return {"snoozed": True, "minutes": 9}


def _timer_start(dev: FakeDevice, d: dict[str, Any]) -> dict[str, Any]:
    try:
        seconds = parse_duration(str(d.get("duration") or ""))
    except ValueError as err:
        raise HomeAssistantError(str(err)) from err
    if not 0 < seconds <= 24 * 3600:
        raise HomeAssistantError("a timer runs for more than 0 seconds and at most 24 hours")
    return {"id": dev.timer_start(seconds, str(d.get("label") or ""))}


def _timer(op: str) -> Handler:
    def handle(dev: FakeDevice, d: dict[str, Any]) -> None:
        tid = str(d.get("id") or "")
        try:
            if op == "cancel":
                dev.timer_cancel(tid)
            elif op in ("pause", "resume"):
                dev.timer_pause(tid, op == "pause")
            else:
                seconds = parse_duration(str(d.get("duration") or ""))
                dev.timer_change(tid, seconds if op == "add" else -seconds)
        except ValueError as err:
            raise HomeAssistantError(str(err)) from err

    return handle


def _notify(dev: FakeDevice, d: dict[str, Any]) -> None:
    """The screen hint (a Show shows it; the logged entry is what a test checks)."""


BEHAVIOUR: dict[str, Handler] = {
    "alarm_set": _alarm_set(),
    "alarm_set_silent": _alarm_set(silent=True),
    "alarm_set_light_only": _alarm_set(light_only=True),
    "alarm_delete": _alarm_delete,
    "alarm_delete_id": _alarm_delete_id,
    "alarms_list": _alarms_list,
    "alarm_snooze_for": _snooze_for,
    "reminder_set": _reminder_set,
    "timer_start": _timer_start,
    "timer_cancel": _timer("cancel"),
    "timer_pause": _timer("pause"),
    "timer_resume": _timer("resume"),
    "timer_add": _timer("add"),
    "timer_remove": _timer("remove"),
    "notify": _notify,
}


@callback
def async_register_device_services(hass: HomeAssistant, dev: FakeDevice) -> CALLBACK_TYPE:
    """Register esphome.<node>_<action> for every action of the contract."""
    names = []
    for action, spec in dev.contract.get("services", {}).items():
        name = f"{dev.node}_{action}"
        behaviour = BEHAVIOUR.get(action)
        response = bool(spec.get("response"))

        def make(action: str = action, behaviour: Handler | None = behaviour, response: bool = response):
            async def handle(call: ServiceCall) -> ServiceResponse:
                data = dict(call.data)
                dev.record("action", action=action, data=data)
                result = behaviour(dev, data) if behaviour else None
                if hasattr(result, "__await__"):
                    result = await result
                return (result or {}) if response else None

            return handle

        hass.services.async_register(
            ESPHOME, name, make(), schema=vol.Schema({}, extra=vol.ALLOW_EXTRA),
            supports_response=SupportsResponse.OPTIONAL if response else SupportsResponse.NONE,
        )
        # The fields, so Home Assistant lists them like the real device's
        # (Mira finds the screen hint by them).
        async_set_service_schema(hass, ESPHOME, name, {
            "name": name, "description": f"{action} (TECHO5 fake)",
            "fields": {f: {"name": f, "required": False, "selector": {"text": {}}} for f in spec.get("fields", [])},
        })
        names.append(name)

    @callback
    def remove() -> None:
        for name in names:
            hass.services.async_remove(ESPHOME, name)

    return remove
