"""A fake Zigbee2MQTT bridge for the test Home Assistant (never for a real house).

It speaks MQTT on the test broker as Zigbee2MQTT 2.14 does, after its source
(lib/extension/bridge.ts, lib/extension/homeassistant.ts): the retained
`bridge/state` and `bridge/info`, the bridge device in Home Assistant with its
"Permit join" switch, the requests `permit_join`, `device/rename` and
`device/remove` with their responses, and for a device that joins the events
`device_joined`, `device_announce`, `device_interview` and its discovery
messages. A device joins on the test action `z2m_fake.join`, only while
pairing is open, as a real one can only then (docs/spec/device-onboarding.md).
After the real run the fake follows what the real bridge sent.
"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from typing import Any

import voluptuous as vol

from homeassistant.components import mqtt
from homeassistant.core import HomeAssistant, ServiceCall, ServiceResponse, SupportsResponse, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import config_validation as cv
from homeassistant.helpers.storage import Store
from homeassistant.helpers.typing import ConfigType

from .devices import KINDS, definition, discovery_configs, state_payload

_LOGGER = logging.getLogger(__name__)

DOMAIN = "z2m_fake"
BASE = "zigbee2mqtt"
DISCOVERY = "homeassistant"
VERSION = "2.14.3"
COORDINATOR = "0x00124b0024c0ffee"
BRIDGE_ID = f"bridge_{COORDINATOR}"
BRIDGE_IDENTIFIER = f"zigbee2mqtt_{BRIDGE_ID}"
ORIGIN = {"name": "Zigbee2MQTT", "sw": VERSION, "url": "https://www.zigbee2mqtt.io"}

CONFIG_SCHEMA = cv.empty_config_schema(DOMAIN)


async def async_setup(hass: HomeAssistant, config: ConfigType) -> bool:
    """`z2m_fake:` in configuration.yaml starts the bridge."""
    if DOMAIN not in config:
        return True
    bridge = FakeBridge(hass)
    hass.data[DOMAIN] = bridge

    async def start(_: Any = None) -> None:
        if not await mqtt.async_wait_for_mqtt_client(hass):
            _LOGGER.error("z2m_fake: MQTT is not available, the fake bridge does not start")
            return
        await bridge.start()

    hass.async_create_task(start())

    async def join(call: ServiceCall) -> ServiceResponse:
        return await bridge.join(call.data["kind"], call.data.get("ieee"), call.data.get("interview_seconds", 2))

    async def remove(call: ServiceCall) -> ServiceResponse:
        return await bridge.remove(call.data["device"])

    hass.services.async_register(
        DOMAIN,
        "join",
        join,
        schema=vol.Schema(
            {
                vol.Required("kind"): vol.In(KINDS),
                vol.Optional("ieee"): cv.string,
                vol.Optional("interview_seconds"): vol.All(vol.Coerce(float), vol.Range(min=0, max=120)),
            }
        ),
        supports_response=SupportsResponse.OPTIONAL,
    )
    hass.services.async_register(
        DOMAIN, "remove", remove, schema=vol.Schema({vol.Required("device"): cv.string}),
        supports_response=SupportsResponse.OPTIONAL,
    )
    return True


class FakeBridge:
    def __init__(self, hass: HomeAssistant) -> None:
        self.hass = hass
        self.store: Store[dict[str, Any]] = Store(hass, 1, f"{DOMAIN}.devices")
        # ieee -> {"kind": ..., "friendly_name": ..., "interviewed": bool}
        self.devices: dict[str, dict[str, Any]] = {}
        self.permit_end: float | None = None  # epoch seconds
        self._permit_timer: asyncio.TimerHandle | None = None
        self._next_ieee = 0x00158D0001A2B300

    # MQTT helpers --------------------------------------------------------------------------------------------------

    async def publish(self, topic: str, payload: Any, retain: bool = False) -> None:
        text = payload if isinstance(payload, str) else json.dumps(payload, ensure_ascii=False)
        await mqtt.async_publish(self.hass, topic, text, qos=1 if topic.startswith(DISCOVERY) else 0, retain=retain)

    async def event(self, kind: str, data: dict[str, Any]) -> None:
        await self.publish(f"{BASE}/bridge/event", {"type": kind, "data": data})

    async def respond(self, endpoint: str, request: Any, data: dict[str, Any], error: str | None = None) -> None:
        response: dict[str, Any] = {"data": {} if error else data, "status": "error" if error else "ok"}
        if error:
            response["error"] = error
        if isinstance(request, dict) and "transaction" in request:
            response["transaction"] = request["transaction"]
        await self.publish(f"{BASE}/bridge/response/{endpoint}", response)

    # Start ---------------------------------------------------------------------------------------------------------

    async def start(self) -> None:
        self.devices = (await self.store.async_load() or {}).get("devices", {})
        await self.publish(f"{BASE}/bridge/state", {"state": "online"}, retain=True)
        await self.publish_info()
        await self.publish_devices()
        for cfg in self.bridge_configs():
            await self.publish(cfg[0], cfg[1], retain=True)
        for ieee, dev in self.devices.items():
            if dev.get("interviewed"):
                await self.discover(ieee)
        await mqtt.async_subscribe(self.hass, f"{BASE}/bridge/request/#", self._on_request)
        _LOGGER.info("z2m_fake: bridge online with %d devices", len(self.devices))

    async def save(self) -> None:
        await self.store.async_save({"devices": self.devices})

    def permit_join(self) -> bool:
        return self.permit_end is not None and self.permit_end > time.time()

    async def publish_info(self) -> None:
        info = {
            "version": VERSION,
            "commit": "fake",
            "coordinator": {"ieee_address": COORDINATOR, "type": "zStack3x0", "meta": {}},
            "network": {"channel": 11, "pan_id": 6754, "extended_pan_id": "0xdddddddddddddddd"},
            "log_level": "info",
            "permit_join": self.permit_join(),
            "permit_join_end": int(self.permit_end * 1000) if self.permit_join() else None,
            "restart_required": False,
            "config": {"mqtt": {"base_topic": BASE}, "homeassistant": {"enabled": True}},
        }
        await self.publish(f"{BASE}/bridge/info", info, retain=True)

    async def publish_devices(self) -> None:
        listed = [
            {
                "ieee_address": COORDINATOR,
                "type": "Coordinator",
                "friendly_name": "Coordinator",
                "supported": True,
                "interview_state": "SUCCESSFUL",
                "disabled": False,
            }
        ]
        for ieee, dev in self.devices.items():
            entry = {
                "ieee_address": ieee,
                "type": "EndDevice" if dev["kind"] == "temperature" else "Router",
                "friendly_name": dev["friendly_name"],
                "supported": dev["kind"] != "unsupported",
                "interview_state": "SUCCESSFUL" if dev.get("interviewed") else "FAILED",
                "disabled": False,
            }
            if dev.get("interviewed"):
                entry["definition"] = definition(dev["kind"])
            listed.append(entry)
        await self.publish(f"{BASE}/bridge/devices", listed, retain=True)

    def bridge_configs(self) -> list[tuple[str, dict[str, Any]]]:
        device = {
            "identifiers": [BRIDGE_IDENTIFIER],
            "name": "Zigbee2MQTT Bridge",
            "model": "Bridge",
            "manufacturer": "Zigbee2MQTT",
            "hw_version": "zStack3x0 20240710",
            "sw_version": VERSION,
        }
        state = f"{BASE}/bridge/state"
        info = f"{BASE}/bridge/info"
        return [
            (
                f"{DISCOVERY}/binary_sensor/{BRIDGE_ID}/connection_state/config",
                {
                    "name": "Connection state",
                    "device_class": "connectivity",
                    "entity_category": "diagnostic",
                    "state_topic": state,
                    "value_template": "{{ value_json.state }}",
                    "payload_on": "online",
                    "payload_off": "offline",
                    "object_id": "zigbee2mqtt_bridge_connection_state",
                    "default_entity_id": "binary_sensor.zigbee2mqtt_bridge_connection_state",
                    "unique_id": f"{BRIDGE_ID}_connection_state_{BASE}",
                    "device": device,
                    "origin": ORIGIN,
                },
            ),
            (
                f"{DISCOVERY}/switch/{BRIDGE_ID}/permit_join/config",
                {
                    "name": "Permit join",
                    "icon": "mdi:human-greeting-proximity",
                    "state_topic": info,
                    "value_template": "{{ value_json.permit_join | lower }}",
                    "command_topic": f"{BASE}/bridge/request/permit_join",
                    "state_on": "true",
                    "state_off": "false",
                    "payload_on": '{"time": 254}',
                    "payload_off": '{"time": 0}',
                    "availability": [{"topic": state, "value_template": "{{ value_json.state }}"}],
                    "object_id": "zigbee2mqtt_bridge_permit_join",
                    "default_entity_id": "switch.zigbee2mqtt_bridge_permit_join",
                    "unique_id": f"{BRIDGE_ID}_permit_join_{BASE}",
                    "device": device,
                    "origin": ORIGIN,
                },
            ),
        ]

    # Requests ------------------------------------------------------------------------------------------------------

    async def _on_request(self, msg: mqtt.ReceiveMessage) -> None:
        endpoint = msg.topic[len(f"{BASE}/bridge/request/") :]
        try:
            request: Any = json.loads(msg.payload) if msg.payload else ""
        except ValueError:
            request = msg.payload
        _LOGGER.info("z2m_fake: request %s %s", endpoint, msg.payload)
        if endpoint == "permit_join":
            await self._permit_join(request)
        elif endpoint == "device/rename":
            await self._rename(request)
        elif endpoint == "device/remove":
            ieee = self.resolve(request.get("id") if isinstance(request, dict) else request)
            if ieee is None:
                await self.respond(endpoint, request, {}, f"Device '{request}' does not exist")
            else:
                await self.remove(ieee)
                await self.respond(endpoint, request, {"id": request.get("id"), "block": False, "force": False})
        else:
            await self.respond(endpoint, request, {}, f"Request '{endpoint}' is unsupported by the fake")

    async def _permit_join(self, request: Any) -> None:
        if isinstance(request, dict):
            if "time" not in request:
                await self.respond("permit_join", request, {}, "Invalid payload")
                return
            seconds = int(request["time"])
        else:
            seconds = int(request)
        if self._permit_timer:
            self._permit_timer.cancel()
            self._permit_timer = None
        if seconds > 0:
            self.permit_end = time.time() + seconds
            self._permit_timer = self.hass.loop.call_later(seconds, self._permit_over)
        else:
            self.permit_end = None
        await self.publish_info()
        await self.respond("permit_join", request, {"time": seconds})

    @callback
    def _permit_over(self) -> None:
        # The coordinator ends pairing itself when the time is over; Zigbee2MQTT publishes its info then.
        self._permit_timer = None
        self.permit_end = None
        self.hass.async_create_task(self.publish_info())

    def resolve(self, name: str | None) -> str | None:
        if not name:
            return None
        for ieee, dev in self.devices.items():
            if name in (ieee, dev["friendly_name"]):
                return ieee
        return None

    async def _rename(self, request: Any) -> None:
        if not isinstance(request, dict) or "from" not in request or "to" not in request:
            await self.respond("device/rename", request, {}, "Invalid payload")
            return
        ieee = self.resolve(request["from"])
        if ieee is None:
            await self.respond("device/rename", request, {}, f"Device '{request['from']}' does not exist")
            return
        to = str(request["to"]).strip()
        ha_rename = bool(request.get("homeassistant_rename", False))
        dev = self.devices[ieee]
        old = dev["friendly_name"]
        dev["friendly_name"] = to
        await self.save()
        await self.publish(f"{BASE}/{old}", "", retain=True)
        if ha_rename:
            # Clear before rename so Home Assistant takes the new name and entity ids; Zigbee2MQTT waits 2 s so Home
            # Assistant deletes the old entities first (its issue 12610).
            await self.clear_discovery(ieee, old)
            await asyncio.sleep(2)
        await self.discover(ieee)
        await self.publish_devices()
        await self.respond("device/rename", request, {"from": old, "to": to, "homeassistant_rename": ha_rename})

    # Devices -------------------------------------------------------------------------------------------------------

    async def discover(self, ieee: str) -> None:
        dev = self.devices[ieee]
        for topic, payload in discovery_configs(dev["kind"], ieee, dev["friendly_name"], BASE, BRIDGE_IDENTIFIER, ORIGIN):
            await self.publish(f"{DISCOVERY}/{topic}", payload, retain=True)
        await self.publish(f"{BASE}/{dev['friendly_name']}", state_payload(dev["kind"]), retain=True)

    async def clear_discovery(self, ieee: str, friendly_name: str) -> None:
        dev = self.devices[ieee]
        for topic, _ in discovery_configs(dev["kind"], ieee, friendly_name, BASE, BRIDGE_IDENTIFIER, ORIGIN):
            await self.publish(f"{DISCOVERY}/{topic}", "", retain=True)

    async def join(self, kind: str, ieee: str | None, interview_seconds: float) -> dict[str, Any]:
        if not self.permit_join():
            _LOGGER.warning("z2m_fake: %s did not join, pairing is closed", kind)
            return {"joined": False, "reason": "pairing is closed"}
        if ieee and ieee in self.devices:
            # A known device that is paired again announces itself; it does not join anew (assumption until the
            # real run: zigbee-herdsman reports deviceJoined only for a new address).
            await self.event("device_announce", {"friendly_name": self.devices[ieee]["friendly_name"], "ieee_address": ieee})
            return {"joined": False, "announced": True, "ieee": ieee}
        if not ieee:
            self._next_ieee += 1
            ieee = f"0x{self._next_ieee:016x}"
            while ieee in self.devices:
                self._next_ieee += 1
                ieee = f"0x{self._next_ieee:016x}"
        self.devices[ieee] = {"kind": kind, "friendly_name": ieee, "interviewed": False}
        await self.save()
        await self.publish_devices()
        await self.event("device_joined", {"friendly_name": ieee, "ieee_address": ieee})
        await self.event("device_announce", {"friendly_name": ieee, "ieee_address": ieee})
        await self.event("device_interview", {"friendly_name": ieee, "status": "started", "ieee_address": ieee})
        self.hass.async_create_task(self._interview(ieee, interview_seconds))
        return {"joined": True, "ieee": ieee}

    async def _interview(self, ieee: str, seconds: float) -> None:
        await asyncio.sleep(seconds)
        dev = self.devices.get(ieee)
        if dev is None:
            return
        name = dev["friendly_name"]
        if dev["kind"] == "interview_failed":
            await self.event("device_interview", {"friendly_name": name, "status": "failed", "ieee_address": ieee})
            await self.publish_devices()
            return
        dev["interviewed"] = True
        await self.save()
        await self.publish_devices()
        await self.event(
            "device_interview",
            {
                "friendly_name": name,
                "status": "successful",
                "ieee_address": ieee,
                "supported": dev["kind"] != "unsupported",
                "definition": definition(dev["kind"]),
            },
        )
        await self.discover(ieee)

    async def remove(self, name: str) -> dict[str, Any]:
        ieee = self.resolve(name)
        if ieee is None:
            raise HomeAssistantError(f"z2m_fake: no device {name}")
        dev = self.devices[ieee]
        await self.clear_discovery(ieee, dev["friendly_name"])
        await self.publish(f"{BASE}/{dev['friendly_name']}", "", retain=True)
        del self.devices[ieee]
        await self.save()
        await self.publish_devices()
        await self.event("device_leave", {"ieee_address": ieee, "friendly_name": dev["friendly_name"]})
        return {"removed": ieee}
