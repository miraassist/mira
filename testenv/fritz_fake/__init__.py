"""Mira FRITZ!Box's answering machine, faked for the test Home Assistant (never for a real house).

docs/spec/fake-devices.md, "The answering machine". Mounted as `custom_components/mira_fritz`
(`mira_fritz:` in the configuration), because the Mira service knows the answering machine by
that domain. Two sensors as Mira FRITZ!Box's (new messages, missed calls) and the actions the
Mira service calls (`get_messages`, `get_calls`, `mark_heard`, `mark_calls_seen`,
`delete_messages`, `get_phonebook`), with the same fields and answers. Test actions:
`mira_fritz.fake_add` adds new recordings dated now, all at once (`recordings`: their callers'
names; `minutes`); `mira_fritz.fake_reset` empties the box. Everything lives in memory.
"""

from __future__ import annotations

from dataclasses import dataclass, field

import voluptuous as vol
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse, callback
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.discovery import async_load_platform
from homeassistant.helpers.typing import ConfigType
from homeassistant.util import dt as dt_util

DOMAIN = "mira_fritz"
CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)
MACHINE = "Anrufbeantworter"
MEDIA = "/local/fake/two-seconds.wav"


@dataclass
class Box:
    """The answering machine's recordings and the missed calls; the sensors listen."""

    messages: list[dict] = field(default_factory=list)
    calls: list[dict] = field(default_factory=list)
    listeners: list = field(default_factory=list)
    next_index: int = 1

    @callback
    def changed(self) -> None:
        for listener in self.listeners:
            listener()

    def new_messages(self) -> int:
        return sum(1 for m in self.messages if m["new"])

    def missed(self) -> int:
        return sum(1 for c in self.calls if c["new"])


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    if DOMAIN not in config:
        return True
    box = hass.data[DOMAIN] = Box()

    async def get_messages(call: ServiceCall) -> ServiceResponse:
        new_only = call.data.get("new_only", False)
        return {"messages": [dict(m) for m in box.messages if m["new"] or not new_only]}

    async def get_calls(call: ServiceCall) -> ServiceResponse:
        new_only = call.data.get("new_only", False)
        kind = call.data.get("kind")
        return {"calls": [dict(c) for c in box.calls if (c["new"] or not new_only) and (not kind or c["kind"] == kind)]}

    async def mark_heard(call: ServiceCall) -> None:
        for m in box.messages:
            if m["id"] in call.data["message_ids"]:
                m["new"] = False
        box.changed()

    async def mark_calls_seen(call: ServiceCall) -> None:
        for c in box.calls:
            c["new"] = False
        box.changed()

    async def delete_messages(call: ServiceCall) -> None:
        box.messages = [m for m in box.messages if m["id"] not in call.data["message_ids"]]
        box.changed()

    async def get_phonebook(call: ServiceCall) -> ServiceResponse:
        return {"books": [], "url": "", "contacts": []}

    async def fake_add(call: ServiceCall) -> None:
        now = dt_util.now().replace(microsecond=0).isoformat()
        for name in call.data["recordings"]:
            box.messages.append(
                {
                    "id": f"0:{box.next_index}",
                    "name": name,
                    "date": now,
                    "minutes": call.data["minutes"],
                    "new": True,
                    "media_content_id": MEDIA,
                    "answering_machine_name": MACHINE,
                }
            )
            box.next_index += 1
        box.changed()

    async def fake_reset(call: ServiceCall) -> None:
        box.messages, box.calls = [], []
        box.changed()

    ids = vol.Schema({vol.Required("message_ids"): vol.All(cv.ensure_list, [cv.string])})
    hass.services.async_register(
        DOMAIN, "get_messages", get_messages, vol.Schema({vol.Optional("new_only"): cv.boolean}),
        supports_response=SupportsResponse.ONLY,
    )
    hass.services.async_register(
        DOMAIN, "get_calls", get_calls,
        vol.Schema({vol.Optional("kind"): cv.string, vol.Optional("new_only"): cv.boolean}),
        supports_response=SupportsResponse.ONLY,
    )
    hass.services.async_register(DOMAIN, "mark_heard", mark_heard, ids)
    hass.services.async_register(DOMAIN, "mark_calls_seen", mark_calls_seen, vol.Schema({}))
    hass.services.async_register(DOMAIN, "delete_messages", delete_messages, ids)
    hass.services.async_register(
        DOMAIN, "get_phonebook", get_phonebook, vol.Schema({}), supports_response=SupportsResponse.ONLY
    )
    hass.services.async_register(
        DOMAIN, "fake_add", fake_add,
        vol.Schema(
            {
                vol.Required("recordings"): vol.All(cv.ensure_list, [cv.string]),
                vol.Optional("minutes", default=1): vol.Coerce(int),
            }
        ),
    )
    hass.services.async_register(DOMAIN, "fake_reset", fake_reset, vol.Schema({}))
    hass.async_create_task(async_load_platform(hass, "sensor", DOMAIN, {}, config))
    return True
