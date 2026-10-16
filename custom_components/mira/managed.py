"""Mira's settings on Home Assistant's own things (docs/spec/ha-anbindung.md).

Mira keeps no lists of her own for what Home Assistant has. Her settings
are entity registry options under the "mira" domain, the way Assist stores
"should_expose", on
- each person: Telegram id, push service, "alone at home, the satellites
  are me" (every HA person is a person for Mira anyway);
- each satellite: its motion sensors, the morning announcement, announce or
  indicate, the indicator light. The room is the satellite's area.
They are edited in the integration's options ("Configure").

On every change the integration sends all of it to the agent (PUT
/v1/ui/ha), which then shows it read-only in its web UI. On the first setup,
what the agent already has is imported. The agent's own format, the one in
mira.yaml, is built here.

Tasks are automations labelled "Mira"
(automations.py).
"""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers import device_registry as dr, entity_registry as er

from .const import DOMAIN

# Mira's settings on a person and on a satellite, the fields of options["mira"].
PERSON_FIELDS = ("telegram_user_id", "notify", "alone_is_me")
SATELLITE_FIELDS = ("presence", "morning", "mode", "indicator")
SATELLITE_DOMAINS = ("assist_satellite", "media_player")

def _settings(hass: HomeAssistant, domains: tuple[str, ...]) -> dict[str, dict[str, Any]]:
    return {
        e.entity_id: dict(e.options[DOMAIN])
        for e in er.async_get(hass).entities.values()
        if e.domain in domains and e.options.get(DOMAIN)
    }


def person_settings(hass: HomeAssistant) -> dict[str, dict[str, Any]]:
    """Mira's settings on Home Assistant's persons, by entity id."""
    return _settings(hass, ("person",))


def satellite_settings(hass: HomeAssistant) -> dict[str, dict[str, Any]]:
    """Mira's settings on the satellites, by entity id."""
    return _settings(hass, SATELLITE_DOMAINS)


def _store(hass: HomeAssistant, entity_id: str, fields: tuple[str, ...], data: dict[str, Any]) -> None:
    options = {k: data[k] for k in fields if data.get(k)}
    er.async_get(hass).async_update_entity_options(entity_id, DOMAIN, options or None)


def set_person_settings(hass: HomeAssistant, entity_id: str, data: dict[str, Any]) -> None:
    """Store Mira's settings on the person; empty ones remove the block."""
    _store(hass, entity_id, PERSON_FIELDS, data)


def set_satellite_settings(hass: HomeAssistant, entity_id: str, data: dict[str, Any]) -> None:
    """Store Mira's settings on the satellite; empty ones remove the block."""
    _store(hass, entity_id, SATELLITE_FIELDS, data)


def satellite_area(hass: HomeAssistant, entity_id: str) -> str:
    """The satellite's area: the entity's own, else its device's."""
    entry = er.async_get(hass).async_get(entity_id)
    if entry is None:
        return ""
    if entry.area_id:
        return entry.area_id
    if entry.device_id and (device := dr.async_get(hass).async_get(entry.device_id)):
        return device.area_id or ""
    return ""


def person_key(hass: HomeAssistant, entity_id: str) -> str:
    """The HA person's own id, which the agent keys persons by."""
    entry = er.async_get(hass).async_get(entity_id)
    if entry and entry.unique_id:
        return entry.unique_id
    return entity_id.split(".", 1)[1]


def person_to_agent(data: dict[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {"entity": data.get("entity", "")}
    if data.get("telegram_user_id"):
        out["telegram_user_id"] = int(data["telegram_user_id"])
    if data.get("notify"):
        out["notify"] = data["notify"]
    if data.get("alone_is_me"):
        out["alone_is_me"] = True
    return out


def person_from_agent(person: dict[str, Any]) -> dict[str, Any]:
    telegram = person.get("telegram_user_id") or 0
    return {
        "telegram_user_id": str(telegram) if telegram else "",
        "notify": person.get("notify") or "",
        "alone_is_me": bool(person.get("alone_is_me")),
    }


def room_to_agent(area: str, satellite: str, data: dict[str, Any]) -> dict[str, Any]:
    presence = data.get("presence") or []
    if isinstance(presence, str):
        presence = [presence]
    return {
        "area": area,
        "satellite": satellite,
        "presence": list(presence),
        "morning": bool(data.get("morning")),
        "mode": data.get("mode") or "announce",
        "indicator": data.get("indicator") or "",
    }


def import_settings(hass: HomeAssistant, agent: dict[str, Any]) -> None:
    """What the agent has now, as settings on HA's persons and satellites."""
    registry = er.async_get(hass)
    have = person_settings(hass)
    for person in (agent.get("persons") or {}).values():
        entity = person.get("entity") or ""
        if entity and entity not in have and registry.async_get(entity):
            set_person_settings(hass, entity, person_from_agent(person))
    have = satellite_settings(hass)
    for room in agent.get("rooms") or []:
        satellite = room.get("satellite") or ""
        if satellite and satellite not in have and registry.async_get(satellite):
            presence = room.get("presence") or []
            set_satellite_settings(
                hass,
                satellite,
                {
                    "presence": [presence] if isinstance(presence, str) else presence,
                    "morning": bool(room.get("morning")),
                    "mode": room.get("mode") or "",
                    "indicator": room.get("indicator") or "",
                },
            )


def build_config(
    hass: HomeAssistant,
    entry: ConfigEntry,
    person: tuple[str, dict[str, Any]] | None = None,
    satellite: tuple[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Person and satellite settings in the agent's format. person or
    satellite = (entity id, settings) is applied first, to check a form
    before it is saved."""
    persons_in = person_settings(hass)
    if person is not None:
        persons_in[person[0]] = person[1]
    persons = {}
    for entity, data in sorted(persons_in.items()):
        if any(data.get(k) for k in PERSON_FIELDS):
            persons[person_key(hass, entity)] = person_to_agent(data | {"entity": entity})
    satellites_in = satellite_settings(hass)
    if satellite is not None:
        satellites_in[satellite[0]] = satellite[1]
    rooms: dict[str, dict[str, Any]] = {}
    for entity, data in sorted(satellites_in.items()):
        if any(data.get(k) for k in SATELLITE_FIELDS) and (area := satellite_area(hass, entity)):
            if area in rooms:
                raise ValueError(f"{rooms[area]['satellite']} and {entity} are both set up for area {area}")
            rooms[area] = room_to_agent(area, entity, data)
    # Every Assist satellite with an area speaks for it; the settings only
    # add to that. A second one in an area that has one already is left out.
    for entry in sorted(er.async_get(hass).entities.values(), key=lambda e: e.entity_id):
        if entry.domain != "assist_satellite" or entry.disabled_by or entry.entity_id in satellites_in:
            continue
        if (area := satellite_area(hass, entry.entity_id)) and area not in rooms:
            rooms[area] = room_to_agent(area, entry.entity_id, {})
    return {"persons": persons, "rooms": [rooms[a] for a in sorted(rooms)]}
