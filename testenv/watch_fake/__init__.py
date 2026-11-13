"""Devices Mira's device watch watches, faked for the test Home Assistant (never for a real house).

docs/spec/fake-devices.md, "The watched devices". A water meter
(`sensor.fake_wasserzahler`, device class water, litres) and a plug's power
(`sensor.fake_aquarium_leistung`, device class power, watts), driven by test
actions: `watch_fake.water_run` lets water run (the meter counts up `litres` every
`every` seconds, as a meter that reports now and then), `watch_fake.water_stop` ends
it; `watch_fake.power` sets the plug's power; `watch_fake.routine` lets the plug
follow a daily routine: `watts` within `windows` ("07:40-11:40, 14:20-21:00"), else
0, set at once and at every full minute. The fake's time is its own, as a device's.
"""

from __future__ import annotations

import voluptuous as vol
from homeassistant.const import Platform
from homeassistant.core import HomeAssistant, ServiceCall
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.discovery import async_load_platform
from homeassistant.helpers.typing import ConfigType

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
    hass.async_create_task(async_load_platform(hass, Platform.SENSOR, DOMAIN, {}, config))
    return True
