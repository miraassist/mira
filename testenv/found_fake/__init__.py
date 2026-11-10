"""Devices Home Assistant finds, faked for the test Home Assistant (never for a real house).

docs/spec/found-devices.md, Testing. The test action `found_fake.discover` starts a
discovered config flow, as Bluetooth, Zeroconf or an integration's discovery would,
of one of four kinds: `confirm` (a step without fields), `button` (a step without
fields that fails until `found_fake.press` was called, as a Hue Bridge's link
button), `fields` (a step with a key) and `wifi` (a step with the fields `ssid` and
`password`, as Improv's). Adding it creates one device without an area, with a
measuring entity and a diagnostic one. `found_fake.add_device` creates a device
without a flow, as a maker's app would.
"""

from __future__ import annotations

import logging
from typing import Any

import voluptuous as vol

from homeassistant.config_entries import SOURCE_IMPORT, SOURCE_INTEGRATION_DISCOVERY, ConfigEntry
from homeassistant.const import EntityCategory
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers.typing import ConfigType
from homeassistant.util import ulid as ulid_util

_LOGGER = logging.getLogger(__name__)

DOMAIN = "found_fake"
KINDS = ("confirm", "button", "fields", "wifi")
CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    if DOMAIN not in config:
        return True
    data = hass.data.setdefault(DOMAIN, {"pressed": set(), "provisioned": {}})

    async def discover(call: ServiceCall) -> ServiceResponse:
        kind = call.data["kind"]
        uid = ulid_util.ulid_now().lower()
        name = call.data.get("name") or f"Fake {kind}"
        result = await hass.config_entries.flow.async_init(
            DOMAIN, context={"source": SOURCE_INTEGRATION_DISCOVERY}, data={"uid": uid, "kind": kind, "name": name}
        )
        return {"flow_id": result.get("flow_id"), "uid": uid, "type": str(result.get("type"))}

    async def press(call: ServiceCall) -> None:
        data["pressed"].add(call.data["uid"])

    async def add_device(call: ServiceCall) -> ServiceResponse:
        entry = next((e for e in hass.config_entries.async_entries(DOMAIN) if e.data.get("app")), None)
        if entry is None:
            await hass.config_entries.flow.async_init(DOMAIN, context={"source": SOURCE_IMPORT}, data={"app": True})
            entry = next(e for e in hass.config_entries.async_entries(DOMAIN) if e.data.get("app"))
        uid = ulid_util.ulid_now().lower()
        device = _device(hass, entry, uid, call.data["name"], call.data.get("manufacturer") or "Fake Maker",
                         call.data.get("model") or "Fake Lamp", call.data.get("entities", "measure"))
        return {"device_id": device.id, "uid": uid}

    hass.services.async_register(DOMAIN, "discover", discover,
                                 schema=vol.Schema({vol.Required("kind"): vol.In(KINDS), vol.Optional("name"): str}),
                                 supports_response=SupportsResponse.ONLY)
    hass.services.async_register(DOMAIN, "press", press, schema=vol.Schema({vol.Required("uid"): str}))
    hass.services.async_register(DOMAIN, "add_device", add_device, schema=vol.Schema({
        vol.Required("name"): str, vol.Optional("manufacturer"): str, vol.Optional("model"): str,
        vol.Optional("entities"): vol.In(["measure", "diagnostic", "none"])}),
        supports_response=SupportsResponse.ONLY)
    return True


def _device(hass: HomeAssistant, entry: ConfigEntry, uid: str, name: str, manufacturer: str, model: str,
            entities: str = "measure") -> dr.DeviceEntry:
    """A device without an area, with a measuring and a diagnostic entity
    (entities "measure"), only a diagnostic one ("diagnostic"), or none."""
    device = dr.async_get(hass).async_get_or_create(
        config_entry_id=entry.entry_id, identifiers={(DOMAIN, uid)}, name=name, manufacturer=manufacturer, model=model
    )
    ents = er.async_get(hass)
    if entities == "measure":
        ents.async_get_or_create("sensor", DOMAIN, f"{uid}_temperature", config_entry=entry, device_id=device.id,
                                 original_name="Temperature")
    if entities in ("measure", "diagnostic"):
        ents.async_get_or_create("sensor", DOMAIN, f"{uid}_signal", config_entry=entry, device_id=device.id,
                                 original_name="Signal", entity_category=EntityCategory.DIAGNOSTIC)
    return device


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    if entry.data.get("app"):
        return True
    _device(hass, entry, entry.data["uid"], entry.title, "Fake Maker", f"Fake {entry.data['kind']}")
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    return True
