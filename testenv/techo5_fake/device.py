"""The state of one fake TECHO5 device: alarms, timers, what it was told."""

from __future__ import annotations

from collections.abc import Callable
from datetime import datetime, timedelta
import json
from pathlib import Path
import re
import secrets
from typing import Any

from homeassistant.components.intent import TimerEventType, TimerInfo, async_register_timer_handler
from homeassistant.core import CALLBACK_TYPE, HomeAssistant, callback
from homeassistant.helpers.event import async_call_later
from homeassistant.util import dt as dt_util

CONTRACT_DIR = Path(__file__).parent / "contract"
LOG_SIZE = 50
WEEKDAYS = {0, 1, 2, 3, 4}


def load_contract(kind: str) -> dict[str, Any]:
    """What a real device of this kind offers (recorded by testha contract record)."""
    return json.loads((CONTRACT_DIR / f"{kind}.json").read_text())


def parse_duration(text: str) -> int:
    """Seconds of a duration the way TECHO5 takes it: "10 minutes",
    "1 hour and 30 minutes", "90 seconds", "in 20 minutes", "1h30m",
    "00:10:00", "1:30" (hours and minutes) or a bare number of minutes."""
    t = str(text).strip().lower().removeprefix("in ").strip()
    if re.fullmatch(r"\d+(\.\d+)?", t):
        return int(float(t) * 60)
    if re.fullmatch(r"\d+:\d{2}:\d{2}", t):
        h, m, s = (int(x) for x in t.split(":"))
        return h * 3600 + m * 60 + s
    if re.fullmatch(r"\d+:\d{2}", t):
        h, m = (int(x) for x in t.split(":"))
        return h * 3600 + m * 60
    total, found = 0, False
    for number, unit in re.findall(r"(\d+(?:\.\d+)?)\s*(h|hours?|m|min|minutes?|s|sec|seconds?)\b", t):
        found = True
        n = float(number)
        total += n * (3600 if unit.startswith("h") else 60 if unit.startswith("m") else 1)
    if not found:
        raise ValueError(f"duration {text!r} not understood")
    return int(total)


class FakeDevice:
    """One device. Entities read it and listen for changes."""

    def __init__(self, hass: HomeAssistant, kind: str, name: str, node: str, contract: dict[str, Any], device_id: str) -> None:
        self.hass = hass
        self.kind = kind
        self.name = name
        self.node = node
        self.contract = contract
        self.device_id = device_id
        self.alarms: list[dict[str, Any]] = []
        self.local_timers: dict[str, dict[str, Any]] = {}
        self.ha_timers: dict[str, TimerInfo] = {}
        self.alarm_sounding = False
        self.timer_sounding = False
        self.log: list[dict[str, Any]] = []
        self._listeners: list[Callable[[], None]] = []

    # --- listeners and the log -------------------------------------------

    @callback
    def add_listener(self, cb: Callable[[], None]) -> CALLBACK_TYPE:
        self._listeners.append(cb)
        return lambda: self._listeners.remove(cb)

    @callback
    def changed(self) -> None:
        for cb in list(self._listeners):
            cb()

    @callback
    def record(self, kind: str, **data: Any) -> None:
        """What the device was told, newest last; a test reads it from
        sensor.<node>_fake_log."""
        self.log.append({"at": dt_util.now().isoformat(timespec="seconds"), "kind": kind, **data})
        del self.log[:-LOG_SIZE]
        self.changed()

    def has_entity(self, entity: str) -> bool:
        """Whether a real device of this kind has the entity "domain.suffix"."""
        return entity in self.contract.get("entities", [])

    # --- alarms ----------------------------------------------------------

    def _next(self, time: str, days: str) -> str:
        now = dt_util.now()
        hour, minute = (int(x) for x in time.split(":")[:2])
        for add in range(8):
            day = (now + timedelta(days=add)).replace(hour=hour, minute=minute, second=0, microsecond=0)
            if day <= now:
                continue
            if days == "weekdays" and day.weekday() not in WEEKDAYS:
                continue
            if days == "weekends" and day.weekday() in WEEKDAYS:
                continue
            return day.isoformat()
        return ""

    @callback
    def alarm_set(self, time: str, days: str = "", label: str = "", **flags: bool) -> dict[str, Any]:
        days = days or "once"
        alarm = {
            "id": secrets.token_hex(6), "time": time[:5], "days": days, "label": label or "",
            "on": True, "next": self._next(time, days), "reminder": False, "ring_on": [],
            "silent": False, "light_only": False,
        } | flags
        self.alarms.append(alarm)
        self.changed()
        return alarm

    @callback
    def alarm_delete(self, time: str = "", label: str = "", id: str = "") -> int:
        before = len(self.alarms)
        self.alarms = [
            a for a in self.alarms
            if not ((id and a["id"] == id) or (not id and a["time"] == time[:5] and (not label or a["label"] == label)))
        ]
        self.changed()
        return before - len(self.alarms)

    def next_alarm(self) -> datetime | None:
        nexts = [dt_util.parse_datetime(a["next"]) for a in self.alarms if a["on"] and a["next"]]
        return min((n for n in nexts if n), default=None)

    def listing(self) -> dict[str, Any]:
        """What <node>_alarms_list answers."""
        now = dt_util.now()
        timers = [
            {"id": tid, "label": t["label"], "left_seconds": max(0, int((t["ends"] - now).total_seconds())) if t["running"] else t["left"],
             "total_seconds": t["total"], "running": t["running"], "local": True}
            for tid, t in self.local_timers.items()
        ] + [
            {"id": t.id, "label": t.name or "", "left_seconds": t.seconds_left, "total_seconds": t.seconds,
             "running": t.is_active, "local": False}
            for t in self.ha_timers.values()
        ]
        return {"ringing": None, "alarms": list(self.alarms), "snoozed": [], "followed": [], "timers": timers}

    # --- the device's own timers (timer_start …) -------------------------

    @callback
    def timer_start(self, seconds: int, label: str = "") -> str:
        tid = "local:" + secrets.token_hex(2)
        self.local_timers[tid] = {"label": label, "total": seconds, "left": seconds, "running": True,
                                  "ends": dt_util.now() + timedelta(seconds=seconds), "cancel": None}
        self._schedule(tid)
        self.changed()
        return tid

    @callback
    def _schedule(self, tid: str) -> None:
        t = self.local_timers[tid]
        if t["cancel"]:
            t["cancel"]()
        left = max(0.0, (t["ends"] - dt_util.now()).total_seconds())

        @callback
        def ring(_now: datetime) -> None:
            self.local_timers.pop(tid, None)
            self.record("timer_rang", id=tid, label=t["label"])
            self.timer_sounding = True
            self.changed()

        t["cancel"] = async_call_later(self.hass, left, ring)

    def _ids(self, tid: str) -> list[str]:
        if tid == "all":
            return list(self.local_timers)
        if tid not in self.local_timers:
            raise ValueError(f"no timer {tid} of this device (Assist's timers are changed by voice)")
        return [tid]

    @callback
    def timer_cancel(self, tid: str) -> None:
        for i in self._ids(tid):
            t = self.local_timers.pop(i)
            if t["cancel"]:
                t["cancel"]()
        self.changed()

    @callback
    def timer_pause(self, tid: str, pause: bool) -> None:
        now = dt_util.now()
        for i in self._ids(tid):
            t = self.local_timers[i]
            if pause and t["running"]:
                t["left"] = max(0, int((t["ends"] - now).total_seconds()))
                t["running"] = False
                if t["cancel"]:
                    t["cancel"]()
                    t["cancel"] = None
            elif not pause and not t["running"]:
                t["running"] = True
                t["ends"] = now + timedelta(seconds=t["left"])
                self._schedule(i)
        self.changed()

    @callback
    def timer_change(self, tid: str, seconds: int) -> None:
        t = self.local_timers.get(tid)
        if t is None:
            raise ValueError(f"no timer {tid} of this device")
        if t["running"]:
            left = (t["ends"] - dt_util.now()).total_seconds() + seconds
            if left <= 0:
                raise ValueError("that is more than the timer has left; cancel it instead")
            t["ends"] = dt_util.now() + timedelta(seconds=left)
            self._schedule(tid)
        else:
            if t["left"] + seconds <= 0:
                raise ValueError("that is more than the timer has left; cancel it instead")
            t["left"] += seconds
        t["total"] += seconds
        self.changed()

    # --- Home Assistant's timers on this device ---------------------------

    @callback
    def async_register_timers(self) -> CALLBACK_TYPE:
        """Like a TECHO5 satellite, the device runs Home Assistant's timers
        (HassStartTimer …): Home Assistant offers the timer tools for it."""

        @callback
        def handle(event_type: TimerEventType, timer: TimerInfo) -> None:
            if event_type in (TimerEventType.STARTED, TimerEventType.UPDATED):
                self.ha_timers[timer.id] = timer
            else:
                self.ha_timers.pop(timer.id, None)
            if event_type is TimerEventType.FINISHED:
                self.record("timer_rang", id=timer.id, label=timer.name or "")
                self.timer_sounding = True
            self.changed()

        return async_register_timer_handler(self.hass, self.device_id, handle)

    @callback
    def stop_sounding(self) -> None:
        self.alarm_sounding = self.timer_sounding = False
        self.changed()
