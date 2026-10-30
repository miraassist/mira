"""Fake TECHO5 devices for the test Home Assistant (never for a real house).

Two devices, "Fake Show" (an Echo Show with TECHO5) and "Fake Echo" (an Echo
with TECHO5 Dot, LED ring, no screen), offer what a real device offers Home
Assistant: the same ESPHome actions with the same fields, taken from a
recording of a real device (contract/<kind>.json, `testha contract record`),
and the entities Mira uses, under the same ids. What Mira relies on behaves
like the device (alarms, timers, announcements, the speaker, the screen hint,
the ring); every other action is accepted and only logged. What a device was
told shows in sensor.<node>_fake_log, so a test can check it. The satellite
and the speaker report the states a device reports, and techo5_fake.wake_word
stands in for the wake word (docs/spec/fake-geraete.md).
"""

from __future__ import annotations

from homeassistant.config_entries import ConfigEntry
from homeassistant.const import Platform
from homeassistant.core import HomeAssistant
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers.typing import ConfigType

from .const import DEVICES, DOMAIN
from .device import FakeDevice, load_contract
from .services import async_register_device_services, async_register_test_services

PLATFORMS = [
    Platform.ASSIST_SATELLITE,
    Platform.BINARY_SENSOR,
    Platform.BUTTON,
    Platform.LIGHT,
    Platform.MEDIA_PLAYER,
    Platform.SELECT,
    Platform.SENSOR,
]


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """`techo5_fake:` in configuration.yaml creates the entry once."""
    if DOMAIN in config and not hass.config_entries.async_entries(DOMAIN):
        hass.async_create_task(
            hass.config_entries.flow.async_init(DOMAIN, context={"source": "import"}, data={})
        )
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    devices: dict[str, FakeDevice] = {}
    registry = dr.async_get(hass)
    for kind, name, node in DEVICES:
        contract = await hass.async_add_executor_job(load_contract, kind)
        device_entry = registry.async_get_or_create(
            config_entry_id=entry.entry_id,
            identifiers={(DOMAIN, node)},
            name=name,
            manufacturer="TECHO5",
            model=f"{contract.get('model', kind)} (fake)",
        )
        fake = FakeDevice(hass, kind, name, node, contract, device_entry.id)
        devices[node] = fake
        entry.async_on_unload(async_register_device_services(hass, fake))
        entry.async_on_unload(fake.async_register_timers())
    entry.runtime_data = devices
    entry.async_on_unload(async_register_test_services(hass, devices))
    await hass.config_entries.async_forward_entry_setups(entry, PLATFORMS)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    return await hass.config_entries.async_unload_platforms(entry, PLATFORMS)
