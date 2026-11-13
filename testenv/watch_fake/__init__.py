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
the last `stats_days` days, both following `windows` (empty: never on) with `watts`.
"""

from __future__ import annotations

from datetime import datetime, timedelta

import voluptuous as vol
from homeassistant.const import Platform
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.discovery import async_load_platform
from homeassistant.helpers.typing import ConfigType
from homeassistant.util import dt as dt_util

DOMAIN = "watch_fake"
CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)

WINDOWS = vol.Match(r"^\s*\d{1,2}:\d{2}\s*-\s*\d{1,2}:\d{2}(\s*,\s*\d{1,2}:\d{2}\s*-\s*\d{1,2}:\d{2})*\s*$")


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    if DOMAIN not in config:
        return True
    devices: dict = hass.data.setdefault(DOMAIN, {})

    async def water_run(call: ServiceCall) -> None:
        devices["meter"].run(call.data["litres"], call.data["every"], call.data.get("for"))

    async def water_stop(call: ServiceCall) -> None:
        devices["meter"].stop()

    async def power(call: ServiceCall) -> None:
        devices["plug"].set_power(call.data["watts"])

    async def routine(call: ServiceCall) -> None:
        devices["plug"].follow(call.data["windows"], call.data["watts"])

    async def past(call: ServiceCall) -> None:
        await _write_past(hass, devices["plug"].entity_id, call.data["windows"], call.data["watts"],
                          call.data["days"], call.data["stats_days"])

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
    hass.services.async_register(DOMAIN, "power", power, vol.Schema({vol.Required("watts"): vol.Coerce(float)}))
    hass.services.async_register(
        DOMAIN,
        "routine",
        routine,
        vol.Schema({vol.Required("windows"): WINDOWS, vol.Optional("watts", default=50): vol.Coerce(float)}),
    )
    hass.services.async_register(
        DOMAIN,
        "past",
        past,
        vol.Schema(
            {
                vol.Optional("windows", default=""): vol.Any("", WINDOWS),
                vol.Optional("watts", default=50): vol.Coerce(float),
                vol.Optional("days", default=10): vol.All(vol.Coerce(int), vol.Range(min=1, max=30)),
                vol.Optional("stats_days", default=90): vol.All(vol.Coerce(int), vol.Range(min=1, max=365)),
            }
        ),
    )
    hass.async_create_task(async_load_platform(hass, Platform.SENSOR, DOMAIN, {}, config))
    return True


def _on(windows: list[tuple[int, int]], local: datetime) -> bool:
    minute = local.hour * 60 + local.minute
    return any(s <= minute < e if s <= e else (minute >= s or minute < e) for s, e in windows)


async def _write_past(hass: HomeAssistant, entity_id: str, windows_text: str, watts: float, days: int, stats_days: int) -> None:
    """Write the plug's past into the recorder (the test HA only): states and hourly statistics."""
    from homeassistant.components.recorder import get_instance
    from homeassistant.components.recorder.db_schema import States, StatesMeta
    from homeassistant.components.recorder.models import StatisticMeanType
    from homeassistant.components.recorder.statistics import async_import_statistics
    from homeassistant.helpers.recorder import session_scope
    from sqlalchemy import func

    from .sensor import _window

    windows = [_window(w) for w in windows_text.split(",")] if windows_text.strip() else []
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
                value = watts if _on(windows, dt_util.as_local(t)) else 0.0
                session.add(States(metadata_id=metadata_id, state=str(value), last_updated_ts=ts, last_reported_ts=ts, origin_idx=0))
                t += timedelta(minutes=5)
                n += 1
            return n

    await instance.async_add_executor_job(write_states)
    hour = now.replace(minute=0, second=0, microsecond=0)
    statistics = []
    start = hour - timedelta(days=stats_days)
    while start < hour - timedelta(hours=1):
        on = [_on(windows, dt_util.as_local(start + timedelta(minutes=m))) for m in range(60)]
        share = sum(on) / 60
        statistics.append(
            {"start": start, "mean": watts * share, "min": watts if all(on) else 0.0, "max": watts if any(on) else 0.0}
        )
        start += timedelta(hours=1)
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
