"""Rooms and Mira's settings for persons, managed by Home Assistant.

Rooms are subentries of the Mira entry, edited with HA's area and entity
pickers. Persons are Home Assistant's own: every HA person is a person for
Mira. What Mira needs on top (Telegram id, push service, "alone at home,
the satellites are me") is stored on the person itself, as entity registry
options under the "mira" domain, the way Assist stores "should_expose".
They are edited in the integration's options ("Configure").

On every change the integration sends all of it to the agent (PUT
/v1/ui/ha), which then shows it read-only in its web UI. On the first setup,
what the agent already has is imported. The agent's own format, the one in
mira.yaml, is built here.

Tasks are not subentries: they are automations labelled "Mira"
(automations.py).
"""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry, ConfigSubentry
from homeassistant.core import HomeAssistant
from homeassistant.helpers import entity_registry as er

from .const import DOMAIN

SUBENTRY_PERSON = "person"  # earlier versions; removed on setup
SUBENTRY_ROOM = "room"
# Mira's settings on a person, the fields of options["mira"].
PERSON_FIELDS = ("telegram_user_id", "notify", "alone_is_me")

def unique_id(subentry_type: str, data: dict[str, Any]) -> str:
    """One subentry per area."""
    return f"room:{data['area']}"


def person_settings(hass: HomeAssistant) -> dict[str, dict[str, Any]]:
    """Mira's settings on Home Assistant's persons, by entity id."""
    return {
        e.entity_id: dict(e.options[DOMAIN])
        for e in er.async_get(hass).entities.values()
        if e.domain == "person" and e.options.get(DOMAIN)
    }


def set_person_settings(hass: HomeAssistant, entity_id: str, data: dict[str, Any]) -> None:
    """Store Mira's settings on the person; empty ones remove the block."""
    options = {k: data[k] for k in PERSON_FIELDS if data.get(k)}
    er.async_get(hass).async_update_entity_options(entity_id, DOMAIN, options or None)


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


def import_person_settings(hass: HomeAssistant, agent: dict[str, Any]) -> None:
    """The agent's persons with an entity, as settings on those persons."""
    have = person_settings(hass)
    registry = er.async_get(hass)
    for person in (agent.get("persons") or {}).values():
        entity = person.get("entity") or ""
        if entity and entity not in have and registry.async_get(entity):
            set_person_settings(hass, entity, person_from_agent(person))


def room_to_agent(data: dict[str, Any]) -> dict[str, Any]:
    return {
        "area": data["area"],
        "satellite": data.get("satellite") or "",
        "presence": data.get("presence") or "",
        "morning": bool(data.get("morning")),
        "mode": data.get("mode") or "announce",
        "indicator": data.get("indicator") or "",
    }


def import_subentries(agent: dict[str, Any]) -> list[ConfigSubentry]:
    """The agent's rooms, as subentries."""
    subentries = []
    for room in agent.get("rooms") or []:
        data = {k: room.get(k) or "" for k in ("area", "satellite", "presence", "indicator")} | {
            "morning": bool(room.get("morning")),
            "mode": room.get("mode") or "announce",
        }
        subentries.append(_subentry(SUBENTRY_ROOM, room["area"], data))
    return subentries


def _subentry(subentry_type: str, title: str, data: dict[str, Any]) -> ConfigSubentry:
    return ConfigSubentry(
        data=data, subentry_type=subentry_type, title=title, unique_id=unique_id(subentry_type, data)
    )


def build_config(
    hass: HomeAssistant,
    entry: ConfigEntry,
    change: tuple[str | None, str, dict[str, Any]] | None = None,
    person: tuple[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """Rooms and person settings in the agent's format. change = (subentry
    id or None for a new one, type, data) or person = (entity id, settings)
    is applied first, to check a form before it is saved."""
    items = {
        sid: dict(s.data) for sid, s in entry.subentries.items() if s.subentry_type == SUBENTRY_ROOM
    }
    if change is not None:
        sid, _, data = change
        items[sid or "_new"] = data
    rooms = [room_to_agent(data) for data in sorted(items.values(), key=lambda d: d["area"])]
    settings = person_settings(hass)
    if person is not None:
        settings[person[0]] = person[1]
    persons = {}
    for entity, data in sorted(settings.items()):
        if any(data.get(k) for k in PERSON_FIELDS):
            persons[person_key(hass, entity)] = person_to_agent(data | {"entity": entity})
    return {"persons": persons, "rooms": rooms}
