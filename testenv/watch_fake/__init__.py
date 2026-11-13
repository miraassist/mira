"""Devices Mira's device watch watches, faked for the test Home Assistant (never for a real house).

docs/spec/fake-devices.md, "The watched devices". A water meter
(`sensor.fake_wasserzahler`, device class water, litres) and a plug's power
(`sensor.fake_aquarium_leistung`, device class power, watts), driven by test
actions: `watch_fake.water_run` lets water run (the meter counts up `litres` every
`every` seconds, as a meter that reports now and then), `watch_fake.water_stop` ends
it; `watch_fake.power` sets the plug's power; `watch_fake.routine` lets the plug
follow a daily routine: `watts` within `windows` ("07:40-11:40, 14:20-21:00"), else
0, set at once and at every full minute. The fake's time is its own, as a device's.
`watch_fake.past` gives the plug a past, as a plug that has run for months: states every 5
minutes of the last `days` days before its first recorded state, and hourly statistics of
the last `stats_days` days, both following `windows` (empty: never on) with `watts`, or
`cycle` ("20:40": minutes on and off) with `watts`; `peak_every` hours adds to the
statistics an hour with a run of `peak` watts (a device run now and then).
Two more plugs: a fridge's (`sensor.fake_kuhlschrank_leistung`), which `watch_fake.cycle`
switches on and off again and again, and a dishwasher's (`sensor.fake_spulmaschine_leistung`),
which `watch_fake.program` runs through its steps ("600:80, 1200:2000, 300:0, 1800:80":
seconds and watts each), then 0 W. `power`, `routine`, `cycle`, `program` and `past` take
the plug's `entity_id` (default: the aquarium's for `power`, `routine` and `past`).

The watched group (house.py), for Mira's group watch: floors and areas, windows and doors, motion sensors,
temperatures and lights in them, and a phone per person. `watch_fake.set` sets one of them (`entity_id`, `state`:
on, off, a temperature, unavailable or unknown).

The devices (devices.py), for the battery and the availability watch: batteries, a leak sensor and a plug that
belong to devices, under the config entry "Fake Geräte" that watch_fake makes for itself (an import flow).
`watch_fake.set` sets their entities too; `watch_fake.reachable` (`device`: its name or key, `reachable`) loses a
whole device or brings it back.
"""

from __future__ import annotations

from datetime import datetime, timedelta

import voluptuous as vol
from homeassistant.config_entries import SOURCE_IMPORT, ConfigEntry
from homeassistant.const import Platform
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.discovery import async_load_platform
from homeassistant.helpers.typing import ConfigType
from homeassistant.util import dt as dt_util

from .house import PHONES, ensure_structure

DOMAIN = "watch_fake"
CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)

DEVICE_PLATFORMS = [Platform.SENSOR, Platform.BINARY_SENSOR, Platform.SWITCH]
STEPS = vol.Match(r"^\s*\d+(\.\d+)?\s*:\s*\d+(\.\d+)?(\s*,\s*\d+(\.\d+)?\s*:\s*\d+(\.\d+)?)*\s*$")
CYCLE = vol.Match(r"^\s*\d+\s*:\s*\d+\s*$")
WINDOWS = vol.Match(r"^\s*\d{1,2}:\d{2}\s*-\s*\d{1,2}:\d{2}(\s*,\s*\d{1,2}:\d{2}\s*-\s*\d{1,2}:\d{2})*\s*$")


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    if DOMAIN not in config:
        return True
    devices: dict = hass.data.setdefault(DOMAIN, {"settable": [], "devices": []})
    ensure_structure(hass)

    async def water_run(call: ServiceCall) -> None:
        devices["meter"].run(call.data["litres"], call.data["every"], call.data.get("for"))

    async def water_stop(call: ServiceCall) -> None:
        devices["meter"].stop()

    def plug(call: ServiceCall, default: str):
        wanted = call.data.get("entity_id")
        for key in ("plug", "fridge", "dishwasher"):
            if devices[key].entity_id == wanted or (wanted is None and key == default):
                return devices[key]
        raise vol.Invalid(f"{wanted} is none of the fake plugs")

    async def power(call: ServiceCall) -> None:
        plug(call, "plug").set_power(call.data["watts"])

    async def routine(call: ServiceCall) -> None:
        plug(call, "plug").follow(call.data["windows"], call.data["watts"])

    async def cycle(call: ServiceCall) -> None:
        plug(call, "fridge").cycle(call.data["watts"], call.data["on"], call.data["off"])

    async def program(call: ServiceCall) -> None:
        steps = [(float(sec), float(w)) for sec, w in (part.split(":") for part in call.data["steps"].split(","))]
        plug(call, "dishwasher").program(steps)

    async def past(call: ServiceCall) -> None:
        await _write_past(hass, plug(call, "plug").entity_id, call.data)

    hass.services.async_register(
        DOMAIN,
        "water_run",
        water_run,
        vol.Schema(
            {
                vol.Optional("litres", default=10): vol.Coerce(float),
                vol.Optional("every", default=300): vol.All(vol.Coerce(float), vol.Range(min=1)),
                vol.Optional("for"): vol.All(vol.Coerce(float), vol.Range(min=1)),
            }
        ),
    )
    hass.services.async_register(DOMAIN, "water_stop", water_stop, vol.Schema({}))
    entity = {vol.Optional("entity_id"): cv.entity_id}
    hass.services.async_register(
        DOMAIN, "power", power, vol.Schema({**entity, vol.Required("watts"): vol.Coerce(float)})
    )
    hass.services.async_register(
        DOMAIN,
        "routine",
        routine,
        vol.Schema({**entity, vol.Required("windows"): WINDOWS, vol.Optional("watts", default=50): vol.Coerce(float)}),
    )
    seconds = vol.All(vol.Coerce(float), vol.Range(min=1))
    hass.services.async_register(
        DOMAIN,
        "cycle",
        cycle,
        vol.Schema(
            {
                **entity,
                vol.Optional("watts", default=45): vol.Coerce(float),
                vol.Required("on"): seconds,
                vol.Required("off"): seconds,
            }
        ),
    )
    hass.services.async_register(DOMAIN, "program", program, vol.Schema({**entity, vol.Required("steps"): STEPS}))
    hass.services.async_register(
        DOMAIN,
        "past",
        past,
        vol.Schema(
            {
                **entity,
                vol.Optional("windows", default=""): vol.Any("", WINDOWS),
                vol.Optional("cycle", default=""): vol.Any("", CYCLE),
                vol.Optional("peak_every", default=0): vol.All(vol.Coerce(int), vol.Range(min=0, max=24 * 30)),
                vol.Optional("peak", default=2000): vol.Coerce(float),
                vol.Optional("watts", default=50): vol.Coerce(float),
                vol.Optional("days", default=10): vol.All(vol.Coerce(int), vol.Range(min=1, max=30)),
                vol.Optional("stats_days", default=90): vol.All(vol.Coerce(int), vol.Range(min=1, max=365)),
            }
        ),
    )
    async def set_state(call: ServiceCall) -> None:
        for entity in devices["settable"] + devices["devices"]:
            if entity.entity_id == call.data["entity_id"]:
                entity.set_state(call.data["state"])
                return
        raise vol.Invalid(f"{call.data['entity_id']} is none of the fake house's entities")

    hass.services.async_register(
        DOMAIN,
        "set",
        set_state,
        vol.Schema(
            {
                vol.Required("entity_id"): cv.entity_id,
                vol.Required("state"): vol.Any("on", "off", "unavailable", "unknown", vol.Match(r"^-?\d+(\.\d+)?$")),
            }
        ),
    )
    async def reachable(call: ServiceCall) -> None:
        wanted = call.data["device"]
        found = [e for e in devices["devices"] if wanted in (e.key, e.device_info["name"])]
        if not found:
            names = sorted({e.device_info["name"] for e in devices["devices"]})
            raise vol.Invalid(f"{wanted} is none of the fake devices: {', '.join(names)}")
        for entity in found:
            entity.set_reachable(call.data["reachable"])

    hass.services.async_register(
        DOMAIN,
        "reachable",
        reachable,
        vol.Schema({vol.Required("device"): cv.string, vol.Required("reachable"): cv.boolean}),
    )
    if not hass.config_entries.async_entries(DOMAIN):
        hass.async_create_task(hass.config_entries.flow.async_init(DOMAIN, context={"source": SOURCE_IMPORT}, data={}))
    for platform in (Platform.SENSOR, Platform.BINARY_SENSOR, Platform.LIGHT):
        hass.async_create_task(async_load_platform(hass, platform, DOMAIN, {}, config))
    for _name, phone in PHONES:
        hass.async_create_task(
            async_load_platform(hass, Platform.NOTIFY, DOMAIN, {"name": phone, "phone": phone}, config)
        )
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """The fake devices (devices.py)."""
    hass.data.setdefault(DOMAIN, {"settable": [], "devices": []})["devices"].clear()
    await hass.config_entries.async_forward_entry_setups(entry, DEVICE_PLATFORMS)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    return await hass.config_entries.async_unload_platforms(entry, DEVICE_PLATFORMS)


def _on(windows: list[tuple[int, int]], local: datetime) -> bool:
    minute = local.hour * 60 + local.minute
    return any(s <= minute < e if s <= e else (minute >= s or minute < e) for s, e in windows)


def _cycle_on(cycle: tuple[int, int], at: datetime) -> bool:
    """On within a cycle of `on` minutes on and `off` minutes off, counted from midnight UTC."""
    on, off = cycle
    minute = int(at.timestamp() // 60)
    return minute % (on + off) < on


async def _write_past(hass: HomeAssistant, entity_id: str, data: dict) -> None:
    """Write the plug's past into the recorder (the test HA only): states and hourly statistics."""
    windows_text, watts, days, stats_days = data["windows"], data["watts"], data["days"], data["stats_days"]
    cycle = tuple(int(x) for x in data["cycle"].split(":")) if data["cycle"].strip() else None
    peak_every, peak = data["peak_every"], data["peak"]
    from homeassistant.components.recorder import get_instance
    from homeassistant.components.recorder.db_schema import States, StatesMeta
    from homeassistant.components.recorder.models import StatisticMeanType
    from homeassistant.components.recorder.statistics import async_import_statistics
    from homeassistant.helpers.recorder import session_scope
    from sqlalchemy import func

    from .sensor import _window

    windows = [_window(w) for w in windows_text.split(",")] if windows_text.strip() else []

    def on_at(t: datetime) -> bool:
        return _cycle_on(cycle, t) if cycle else _on(windows, dt_util.as_local(t))

    instance = get_instance(hass)
    now = dt_util.now()

    def write_states() -> int:
        with session_scope(session=instance.get_session()) as session:
            metadata_id = session.query(StatesMeta.metadata_id).filter(StatesMeta.entity_id == entity_id).scalar()
            if metadata_id is None:
                raise ValueError(f"{entity_id} has no recorded state yet")
            first = session.query(func.min(States.last_updated_ts)).filter(States.metadata_id == metadata_id).scalar()
            end = first if first is not None else now.timestamp()
            # On the full 5 minutes, as a plug that reports every 5 minutes.
            t, n = (now - timedelta(days=days)).replace(second=0, microsecond=0), 0
            t -= timedelta(minutes=t.minute % 5)
            while t.timestamp() < end:
                ts = t.timestamp()
                value = watts if on_at(t) else 0.0
                session.add(States(metadata_id=metadata_id, state=str(value), last_updated_ts=ts, last_reported_ts=ts, origin_idx=0))
                t += timedelta(minutes=5)
                n += 1
            return n

    await instance.async_add_executor_job(write_states)
    hour = now.replace(minute=0, second=0, microsecond=0)
    statistics = []
    start = hour - timedelta(days=stats_days)
    n = 0
    while start < hour - timedelta(hours=1):
        on = [on_at(start + timedelta(minutes=m)) for m in range(60)]
        share = sum(on) / 60
        row = {"start": start, "mean": watts * share, "min": watts if all(on) else 0.0, "max": watts if any(on) else 0.0}
        if peak_every and n % peak_every == 0:
            # An hour with a device run in it.
            row = {"start": start, "mean": peak / 2, "min": 0.0, "max": peak}
        statistics.append(row)
        start += timedelta(hours=1)
        n += 1
    async_import_statistics(
        hass,
        {
            "mean_type": StatisticMeanType.ARITHMETIC,
            "has_sum": False,
            "name": None,
            "source": "recorder",
            "statistic_id": entity_id,
            "unit_class": "power",
            "unit_of_measurement": "W",
        },
        statistics,
    )
