"""Watchers, persons and rooms as subentries of the Mira entry.

Home Assistant manages them: each is a subentry, edited with HA's own entity
and area pickers. On every change the integration sends all of them to the
agent (PUT /v1/ui/ha), which then shows them read-only in its web UI. On the
first setup, what the agent already has is imported as subentries.

Subentry data is what the forms edit. The agent's own format, the one in
mira.yaml, is built from it here; fields the forms do not show survive in
"extra" (top-level watcher fields) and "trigger_extra" (not_to, not_from).
"""

from __future__ import annotations

from typing import Any

from homeassistant.config_entries import ConfigEntry, ConfigSubentry

SUBENTRY_WATCHER = "watcher"
SUBENTRY_FEED = "feed"
SUBENTRY_PERSON = "person"
SUBENTRY_ROOM = "room"

HOUSEHOLD = "household"
IMPORTANCE_AUTO = "auto"

# Watcher fields the forms edit; everything else goes to "extra".
_WATCHER_FIELDS = {
    "name", "trigger", "schedule", "kind", "url", "description", "importance", "audience", "confidentiality",
}
_TRIGGER_FIELDS = {"entity_id", "to", "from"}


def unique_id(subentry_type: str, data: dict[str, Any]) -> str:
    """One subentry per watcher name, person key and area."""
    if subentry_type == SUBENTRY_PERSON:
        return f"person:{data['key']}"
    if subentry_type == SUBENTRY_ROOM:
        return f"room:{data['area']}"
    return f"watcher:{data['name']}"


def _non_empty(d: dict[str, Any]) -> dict[str, Any]:
    return {k: v for k, v in d.items() if v not in (None, "", [], {}, "0s", 0)}


def watcher_to_agent(subentry_type: str, data: dict[str, Any]) -> dict[str, Any]:
    """Subentry data -> the agent's watcher."""
    out: dict[str, Any] = {"name": data["name"], "description": data.get("description", "")}
    if subentry_type == SUBENTRY_FEED:
        out |= {"kind": "rss", "url": data["url"], "schedule": data["schedule"]}
    else:
        trigger = {"entity_id": list(data["entity_id"])}
        for key in ("to", "from"):
            if data.get(key):
                trigger[key] = list(data[key])
        out["trigger"] = trigger | data.get("trigger_extra", {})
    importance = data.get("importance", IMPORTANCE_AUTO)
    if importance != IMPORTANCE_AUTO:
        out["importance"] = int(importance)
    if data.get("audience", HOUSEHOLD) != HOUSEHOLD:
        out["audience"] = data["audience"]
    if data.get("confidentiality") == "confidential":
        out["confidentiality"] = "confidential"
    return out | data.get("extra", {})


def watcher_from_agent(watcher: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    """The agent's watcher -> (subentry type, subentry data)."""
    data: dict[str, Any] = {
        "name": watcher["name"],
        "description": watcher.get("description") or "",
        "importance": IMPORTANCE_AUTO if watcher.get("importance") is None else str(watcher["importance"]),
        "audience": watcher.get("audience") or HOUSEHOLD,
        "confidentiality": watcher.get("confidentiality") or "normal",
        "extra": _non_empty({k: v for k, v in watcher.items() if k not in _WATCHER_FIELDS}),
    }
    if watcher.get("kind") == "rss" or watcher.get("schedule"):
        return SUBENTRY_FEED, data | {"url": watcher.get("url", ""), "schedule": watcher.get("schedule", "")}
    trigger = watcher.get("trigger") or {}
    data |= {
        "entity_id": list(trigger.get("entity_id") or []),
        "to": list(trigger.get("to") or []),
        "from": list(trigger.get("from") or []),
        "trigger_extra": _non_empty({k: v for k, v in trigger.items() if k not in _TRIGGER_FIELDS}),
    }
    return SUBENTRY_WATCHER, data


def person_to_agent(data: dict[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {"entity": data.get("entity", "")}
    if data.get("telegram_user_id"):
        out["telegram_user_id"] = int(data["telegram_user_id"])
    if data.get("notify"):
        out["notify"] = data["notify"]
    return out


def person_from_agent(key: str, person: dict[str, Any]) -> dict[str, Any]:
    telegram = person.get("telegram_user_id") or 0
    return {
        "key": key,
        "entity": person.get("entity") or "",
        "telegram_user_id": str(telegram) if telegram else "",
        "notify": person.get("notify") or "",
    }


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
    """What the agent has now, as subentries."""
    subentries = []
    for watcher in agent.get("watchers") or []:
        kind, data = watcher_from_agent(watcher)
        subentries.append(_subentry(kind, data["name"], data))
    for key, person in sorted((agent.get("persons") or {}).items()):
        subentries.append(_subentry(SUBENTRY_PERSON, key, person_from_agent(key, person)))
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
    entry: ConfigEntry,
    change: tuple[str | None, str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """All subentries in the agent's format. change = (subentry id or None
    for a new one, type, data) is applied first, to check a form before it is
    saved."""
    items = {sid: (s.subentry_type, dict(s.data), s.title) for sid, s in entry.subentries.items()}
    if change is not None:
        sid, subentry_type, data = change
        items[sid or "_new"] = (subentry_type, data, "")
    watchers, persons, rooms = [], {}, []
    for subentry_type, data, _ in sorted(items.values(), key=lambda i: unique_id(i[0], i[1])):
        if subentry_type in (SUBENTRY_WATCHER, SUBENTRY_FEED):
            watchers.append(watcher_to_agent(subentry_type, data))
        elif subentry_type == SUBENTRY_PERSON:
            persons[data["key"]] = person_to_agent(data)
        elif subentry_type == SUBENTRY_ROOM:
            rooms.append(room_to_agent(data))
    return {"watchers": watchers, "persons": persons, "rooms": rooms}
