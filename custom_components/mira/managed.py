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

from homeassistant.components.homeassistant.exposed_entities import async_should_expose
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers import device_registry as dr, entity_registry as er

from .const import DOMAIN

# Mira's settings on a person and on a satellite, the fields of options["mira"].
PERSON_FIELDS = ("channels", "accept_from", "alone_is_me")
ACCEPT = ("all", "persons", "none")
SATELLITE_FIELDS = ("presence", "morning", "indicator")
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
    if data.get("channels"):
        out["channels"] = list(data["channels"])
    if data.get("accept_from") and data["accept_from"] != "all":
        out["accept_from"] = data["accept_from"]
    if data.get("alone_is_me"):
        out["alone_is_me"] = True
    return out


def person_from_agent(person: dict[str, Any]) -> dict[str, Any]:
    return {
        "channels": list(person.get("channels") or []),
        "accept_from": person.get("accept_from") or "",
        "alone_is_me": bool(person.get("alone_is_me")),
    }


def own_notify(hass: HomeAssistant) -> set[str]:
    """Mira's own notify entities, the house's and each person's. They take
    a message for Mira to deliver, so they are never a channel: one would
    turn a person's message into a new message instead of delivering it."""
    return {
        e.entity_id for e in er.async_get(hass).entities.values() if e.platform == DOMAIN and e.domain == "notify"
    }


def notify_targets(hass: HomeAssistant) -> list[str]:
    """What a person's channels can be: notify entities (e.g. a chat of
    HA's Telegram integration) and legacy notify services (the companion
    app), without Mira's own."""
    entities = {s.entity_id for s in hass.states.async_all("notify")} - own_notify(hass)
    services = {
        f"notify.{name}"
        for name in hass.services.async_services_for_domain("notify")
        if name not in ("send_message", "persistent_notification", "notify")
    }
    return sorted(entities | services)


def room_to_agent(area: str, satellite: str, data: dict[str, Any]) -> dict[str, Any]:
    presence = data.get("presence") or []
    if isinstance(presence, str):
        presence = [presence]
    return {
        "area": area,
        "satellite": satellite,
        "presence": list(presence),
        "morning": bool(data.get("morning")),
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
                    "indicator": room.get("indicator") or "",
                },
            )


def build_config(
    hass: HomeAssistant,
    entry: ConfigEntry,
    persons: dict[str, dict[str, Any]] | None = None,
    satellites: dict[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Person and satellite settings in the agent's format. persons and
    satellites (entity id: settings) are applied first, to check a form
    before it is saved."""
    persons_in = person_settings(hass) | (persons or {})
    persons_out = {}
    for entity, data in sorted(persons_in.items()):
        if any(data.get(k) for k in PERSON_FIELDS):
            persons_out[person_key(hass, entity)] = person_to_agent(data | {"entity": entity})
    satellites_in = satellite_settings(hass) | (satellites or {})
    rooms: dict[str, dict[str, Any]] = {}
    for entity, data in sorted(satellites_in.items()):
        if any(data.get(k) for k in SATELLITE_FIELDS) and (area := satellite_area(hass, entity)):
            if area in rooms:
                raise ValueError(f"{rooms[area]['satellite']} and {entity} are both set up for area {area}")
            rooms[area] = room_to_agent(area, entity, data)
    # Every Assist satellite with an area speaks for it; the settings only
    # add to that. A second one in an area that has one already is left out.
    for entry in sorted(er.async_get(hass).entities.values(), key=lambda e: e.entity_id):
        taken = {r["satellite"] for r in rooms.values()}
        if entry.domain != "assist_satellite" or entry.disabled_by or entry.entity_id in taken:
            continue
        if (area := satellite_area(hass, entry.entity_id)) and area not in rooms:
            rooms[area] = room_to_agent(area, entry.entity_id, {})
    return {"persons": persons_out, "rooms": [rooms[a] for a in sorted(rooms)], "exposed": exposed_entities(hass)}


def exposed_entities(hass: HomeAssistant) -> list[str]:
    """What Home Assistant exposes to Assist, decided the way it does
    itself. The agent's own user may not read that list (it is no admin), so
    the integration hands it over: it is the line for everything Mira reads
    by herself (docs/spec/ha-anbindung.md)."""
    return sorted(e for e in hass.states.async_entity_ids() if async_should_expose(hass, "conversation", e))


def normalize_person(data: dict[str, Any]) -> dict[str, Any]:
    """A person's settings from a form; raises ValueError on a bad one."""
    out = {k: data.get(k) for k in PERSON_FIELDS}
    channels = out.get("channels") or []
    if isinstance(channels, str):
        channels = [c.strip() for c in channels.split(",")]
    channels = [c.strip() for c in channels if c and c.strip()]
    for c in channels:
        if not c.startswith("notify."):
            raise ValueError(f"{c!r} is not a notify target")
    out["channels"] = channels
    accept = out.get("accept_from") or "all"
    if accept not in ACCEPT:
        raise ValueError(f"accept_from must be all, persons or none, not {accept!r}")
    out["accept_from"] = "" if accept == "all" else accept
    out["alone_is_me"] = bool(out.get("alone_is_me"))
    return out


def normalize_satellite(data: dict[str, Any]) -> dict[str, Any]:
    """A satellite's settings from a form."""
    presence = data.get("presence") or []
    if isinstance(presence, str):
        presence = [p.strip() for p in presence.split(",")]
    return {
        "presence": [p for p in presence if p],
        "morning": bool(data.get("morning")),
        "indicator": str(data.get("indicator") or "").strip(),
    }


async def save_settings(
    hass: HomeAssistant,
    entry: ConfigEntry,
    persons: dict[str, dict[str, Any]] | None = None,
    satellites: dict[str, dict[str, Any]] | None = None,
) -> None:
    """Check the settings with the agent, then store them on the persons and
    satellites. Raises ValueError or the client's errors; nothing is stored
    then."""
    persons = {e: normalize_person(d) for e, d in (persons or {}).items()}
    own = own_notify(hass)
    for data in persons.values():
        for c in data["channels"]:
            if c in own:
                raise ValueError(f"{c} is Mira's own and takes messages for her; a channel delivers them")
    satellites = {e: normalize_satellite(d) for e, d in (satellites or {}).items()}
    for entity in satellites:
        if not satellite_area(hass, entity):
            raise ValueError(f"{entity} has no area")
    await entry.runtime_data.client.set_managed_config(build_config(hass, entry, persons, satellites))
    for entity, data in persons.items():
        set_person_settings(hass, entity, data)
    for entity, data in satellites.items():
        set_satellite_settings(hass, entity, data)


def entity_name(hass: HomeAssistant, entity_id: str) -> str:
    state = hass.states.get(entity_id)
    return state.name if state else entity_id


def all_persons(hass: HomeAssistant) -> list[str]:
    """Home Assistant's persons, by entity id."""
    return sorted(e.entity_id for e in er.async_get(hass).entities.values() if e.domain == "person")


def all_satellites(hass: HomeAssistant) -> list[str]:
    """The satellites Mira may use: every Assist satellite with an area, and
    speakers that have settings already."""
    have = satellite_settings(hass)
    return sorted(
        e.entity_id
        for e in er.async_get(hass).entities.values()
        if not e.disabled_by
        and ((e.domain == "assist_satellite" and satellite_area(hass, e.entity_id)) or e.entity_id in have)
    )
