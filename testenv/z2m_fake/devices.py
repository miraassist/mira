"""The kinds of devices the fake bridge can pair, as Zigbee2MQTT 2.14 describes and discovers them.

Definitions and discovery payloads follow lib/extension/bridge.ts (getDefinitionPayload) and
lib/extension/homeassistant.ts (exposeToConfig, getDevicePayload) of Zigbee2MQTT; the devices are real models
(an Aqara sensor, an IKEA plug), their exposes shortened to what Home Assistant gets as entities.
"""

from __future__ import annotations

from typing import Any

KINDS = ["temperature", "plug", "unsupported", "interview_failed"]

# kind -> definition (vendor, model, description) and its entities:
# (component, object_id, payload without the common fields)
_KINDS: dict[str, dict[str, Any]] = {
    "temperature": {
        "definition": {
            "model": "WSDCGQ11LM",
            "vendor": "Aqara",
            "description": "Temperature, humidity and pressure sensor",
            "source": "native",
            "supports_ota": False,
            "exposes": [
                {"type": "numeric", "name": "temperature", "property": "temperature", "unit": "°C", "access": 1},
                {"type": "numeric", "name": "humidity", "property": "humidity", "unit": "%", "access": 1},
                {"type": "numeric", "name": "pressure", "property": "pressure", "unit": "hPa", "access": 1},
                {"type": "numeric", "name": "battery", "property": "battery", "unit": "%", "access": 1, "category": "diagnostic"},
                {"type": "numeric", "name": "voltage", "property": "voltage", "unit": "mV", "access": 1, "category": "diagnostic"},
                {"type": "numeric", "name": "linkquality", "property": "linkquality", "unit": "lqi", "access": 1, "category": "diagnostic"},
            ],
            "options": [],
        },
        "entities": [
            ("sensor", "temperature", {"device_class": "temperature", "state_class": "measurement", "unit_of_measurement": "°C", "enabled_by_default": True}),
            ("sensor", "humidity", {"device_class": "humidity", "state_class": "measurement", "unit_of_measurement": "%", "enabled_by_default": True}),
            ("sensor", "pressure", {"device_class": "atmospheric_pressure", "state_class": "measurement", "unit_of_measurement": "hPa", "enabled_by_default": True}),
            ("sensor", "battery", {"device_class": "battery", "state_class": "measurement", "unit_of_measurement": "%", "entity_category": "diagnostic", "enabled_by_default": True}),
            ("sensor", "voltage", {"device_class": "voltage", "state_class": "measurement", "unit_of_measurement": "mV", "entity_category": "diagnostic", "enabled_by_default": False}),
            ("sensor", "linkquality", {"name": "Linkquality", "icon": "mdi:signal", "state_class": "measurement", "unit_of_measurement": "lqi", "entity_category": "diagnostic", "enabled_by_default": False}),
        ],
        "state": {"temperature": 21.4, "humidity": 48.2, "pressure": 1012, "battery": 100, "voltage": 3025, "linkquality": 120},
    },
    "plug": {
        "definition": {
            "model": "E2206",
            "vendor": "IKEA",
            "description": "INSPELNING smart plug",
            "source": "native",
            "supports_ota": True,
            "exposes": [
                {"type": "switch", "features": [{"type": "binary", "name": "state", "property": "state", "access": 7, "value_on": "ON", "value_off": "OFF"}]},
                {"type": "numeric", "name": "power", "property": "power", "unit": "W", "access": 5},
                {"type": "numeric", "name": "energy", "property": "energy", "unit": "kWh", "access": 5},
                {"type": "numeric", "name": "voltage", "property": "voltage", "unit": "V", "access": 5},
                {"type": "numeric", "name": "current", "property": "current", "unit": "A", "access": 5},
                {"type": "numeric", "name": "linkquality", "property": "linkquality", "unit": "lqi", "access": 1, "category": "diagnostic"},
            ],
            "options": [],
        },
        "entities": [
            ("switch", "switch", {"name": None, "value_template": "{{ value_json.state }}", "state_on": "ON", "state_off": "OFF", "payload_on": '{"state": "ON"}', "payload_off": '{"state": "OFF"}', "command": True}),
            ("sensor", "power", {"device_class": "power", "state_class": "measurement", "unit_of_measurement": "W", "enabled_by_default": True}),
            ("sensor", "energy", {"device_class": "energy", "state_class": "total_increasing", "unit_of_measurement": "kWh", "enabled_by_default": True}),
            ("sensor", "voltage", {"device_class": "voltage", "state_class": "measurement", "unit_of_measurement": "V", "enabled_by_default": False}),
            ("sensor", "current", {"device_class": "current", "state_class": "measurement", "unit_of_measurement": "A", "enabled_by_default": False}),
            ("sensor", "linkquality", {"name": "Linkquality", "icon": "mdi:signal", "state_class": "measurement", "unit_of_measurement": "lqi", "entity_category": "diagnostic", "enabled_by_default": False}),
        ],
        "state": {"state": "OFF", "power": 0, "energy": 0.12, "voltage": 231, "current": 0, "linkquality": 160},
    },
    "unsupported": {
        # Zigbee2MQTT generates a definition from the clusters of a device it does not know.
        "definition": {
            "model": "TS0001_fake",
            "vendor": "_TZ3000_fake",
            "description": "Automatically generated definition",
            "source": "generated",
            "supports_ota": False,
            "exposes": [
                {"type": "numeric", "name": "linkquality", "property": "linkquality", "unit": "lqi", "access": 1, "category": "diagnostic"},
            ],
            "options": [],
        },
        "entities": [
            ("sensor", "linkquality", {"name": "Linkquality", "icon": "mdi:signal", "state_class": "measurement", "unit_of_measurement": "lqi", "entity_category": "diagnostic", "enabled_by_default": False}),
        ],
        "state": {"linkquality": 90},
    },
    "interview_failed": {"definition": None, "entities": [], "state": {}},
}


def definition(kind: str) -> dict[str, Any] | None:
    return _KINDS[kind]["definition"]


def state_payload(kind: str) -> dict[str, Any]:
    return dict(_KINDS[kind]["state"])


def discovery_configs(
    kind: str, ieee: str, friendly_name: str, base: str, bridge_identifier: str, origin: dict[str, Any]
) -> list[tuple[str, dict[str, Any]]]:
    """The discovery topics (below the discovery prefix) and payloads of a device, as Zigbee2MQTT publishes them."""
    spec = _KINDS[kind]
    d = spec["definition"]
    if d is None:
        return []
    device = {
        "identifiers": [f"zigbee2mqtt_{ieee}"],
        "name": friendly_name,
        "model": d["description"],
        "model_id": d["model"],
        "manufacturer": d["vendor"],
        "via_device": bridge_identifier,
    }
    state_topic = f"{base}/{friendly_name}"
    slug = friendly_name.replace(" ", "_").lower()
    configs = []
    for component, object_id, extra in spec["entities"]:
        extra = dict(extra)
        command = extra.pop("command", False)
        # Zigbee2MQTT's suggested object id: the device's name, plus the entity's own part unless it is the
        # component itself ("switch" gives "<name>", "temperature" gives "<name>_temperature").
        if object_id.startswith(component) and "_" in object_id:
            oid = f"{slug}_{object_id.split('_', 1)[1]}"
        elif not object_id.startswith(component):
            oid = f"{slug}_{object_id}"
        else:
            oid = slug
        payload: dict[str, Any] = {
            "availability": [{"topic": f"{base}/bridge/state", "value_template": "{{ value_json.state }}"}],
            "device": device,
            "object_id": oid,
            "default_entity_id": f"{component}.{oid}",
            "origin": origin,
            "state_topic": state_topic,
            "unique_id": f"{ieee}_{object_id}_{base}",
            "value_template": f"{{{{ value_json.{object_id} }}}}",
        }
        payload.update(extra)
        if command:
            payload["command_topic"] = f"{state_topic}/set"
        configs.append((f"{component}/{ieee}/{object_id}/config", payload))
    return configs
