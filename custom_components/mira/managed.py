"""Mira's settings on Home Assistant's own things (docs/spec/ha-integration.md).

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

import hashlib
from typing import Any

from homeassistant.components.homeassistant.exposed_entities import async_should_expose
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers import issue_registry as ir

from .const import DOMAIN

# Mira's settings on a person and on a satellite, the fields of options["mira"].
PERSON_FIELDS = ("channels", "accept_from", "alone_is_me", "voice_threshold")
# What a person may set for themselves on the panel's card "Me"; the rest is an admin's.
PERSON_OWN_FIELDS = ("accept_from", "alone_is_me")
ACCEPT = ("all", "persons", "none")
SATELLITE_FIELDS = ("presence", "morning", "indicator", "primary")
# "primary" is stored as the area the satellite is the primary of, so the
# mark does not move with it to another room; forms show it as a switch.
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
    if data.get("voice_threshold"):
        out["voice_threshold"] = data["voice_threshold"]
    return out


def person_from_agent(person: dict[str, Any]) -> dict[str, Any]:
    return {
        "channels": list(person.get("channels") or []),
        "accept_from": person.get("accept_from") or "",
        "alone_is_me": bool(person.get("alone_is_me")),
        "voice_threshold": person.get("voice_threshold") or None,
    }


def own_notify(hass: HomeAssistant) -> set[str]:
    """Mira's own notify entities, the house's and each person's. They take
    a message for Mira to deliver, so they are never a channel: one would
    turn a person's message into a new message instead of delivering it."""
    return {e.entity_id for e in er.async_get(hass).entities.values() if e.platform == DOMAIN and e.domain == "notify"}


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


def all_devices(registry: dr.DeviceRegistry) -> list[dr.DeviceEntry]:
    """Every device entry. Iterating `registry.devices` gives the entries
    since Home Assistant 2026.11 (using it as a mapping is deprecated, gone
    in 2027.9), their ids before (docs/spec/fallbacks.md)."""
    items = list(registry.devices)
    if items and isinstance(items[0], str):
        return [d for d in (registry.async_get(i) for i in items) if d is not None]
    return items


def ring_of(hass: HomeAssistant, satellite: str) -> str:
    """The LED ring of a satellite's device, "" without one: a light named
    "LED ring" on the device or on a device of its own that hangs off it
    (TECHO5: "Dot 2 ring"). Its segments are not the ring. Read by device,
    not from the whole registry."""
    entities = er.async_get(hass)
    sat = entities.async_get(satellite)
    if sat is None or not sat.device_id:
        return ""
    devices = dr.async_get(hass)
    family = [sat.device_id] + sorted(d.id for d in all_devices(devices) if d.via_device_id == sat.device_id)
    for device_id in family:
        for e in er.async_entries_for_device(entities, device_id):
            if (
                e.domain == "light"
                and not e.disabled_by
                and (e.original_name or "").strip().lower().endswith("led ring")
            ):
                return e.entity_id
    return ""


def room_to_agent(area: str, satellite: str, data: dict[str, Any], ring: str = "") -> dict[str, Any]:
    """A room as the agent has it. Without a setting its indicator is the
    satellite's own ring, found (docs/spec/ha-integration.md)."""
    presence = data.get("presence") or []
    if isinstance(presence, str):
        presence = [presence]
    return {
        "area": area,
        "satellite": satellite,
        "presence": list(presence),
        "morning": bool(data.get("morning")),
        "indicator": data.get("indicator") or ring,
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


def room_satellites(hass: HomeAssistant, settings: dict[str, dict[str, Any]] | None = None) -> dict[str, list[str]]:
    """The satellites of each area, the primary first, then the others in
    the order they came (docs/spec/satellites-rooms.md): every Assist
    satellite that is on and has an area, and speakers with settings. The
    primary is the one marked; without a mark (until ensure_primaries has
    marked one), the oldest in the entity registry."""
    settings = satellite_settings(hass) if settings is None else settings
    registry = er.async_get(hass)
    by_area: dict[str, list[er.RegistryEntry]] = {}
    for entry in registry.entities.values():
        if entry.disabled_by:
            continue
        if entry.domain != "assist_satellite" and entry.entity_id not in settings:
            continue
        if entry.domain not in SATELLITE_DOMAINS or not (area := satellite_area(hass, entry.entity_id)):
            continue
        by_area.setdefault(area, []).append(entry)
    out: dict[str, list[str]] = {}
    for area, entries in by_area.items():
        entries.sort(key=lambda e: (e.created_at, e.entity_id))
        marked = [e for e in entries if (settings.get(e.entity_id) or {}).get("primary") == area]
        first = marked[0] if marked else entries[0]
        out[area] = [first.entity_id] + [e.entity_id for e in entries if e is not first]
    return out


# The satellite each area had as its room when ensure_primaries last ran:
# the one that was there before a second came (hass.data, not stored).
_ROOM_BEFORE = f"{DOMAIN}_room_before"


@callback
def ensure_primaries(hass: HomeAssistant) -> None:
    """Every room with several satellites has exactly one marked primary,
    visible in its settings. Without one, the satellite that was the room's
    before is marked; when that is not known (after a restart), the oldest.
    Of several marks, the first stays. A room with one satellite needs no
    mark."""
    settings = satellite_settings(hass)
    before: dict[str, str] = hass.data.setdefault(_ROOM_BEFORE, {})
    rooms = room_satellites(hass, settings)
    for area, sats in rooms.items():
        if len(sats) > 1:
            marked = [s for s in sats if (settings.get(s) or {}).get("primary") == area]
            primary = marked[0] if marked else before.get(area) if before.get(area) in sats else sats[0]
            if (settings.get(primary) or {}).get("primary") != area:
                set_satellite_settings(hass, primary, (settings.get(primary) or {}) | {"primary": area})
            # A mark of another room, brought along, or a second one goes.
            for other in sats:
                if other != primary and (settings.get(other) or {}).get("primary"):
                    set_satellite_settings(hass, other, settings[other] | {"primary": ""})
            before[area] = primary
        else:
            before[area] = sats[0]


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
    settings = satellite_settings(hass) | (satellites or {})
    rooms = []
    for area, sats in sorted(room_satellites(hass, settings).items()):
        # The room as an older service knows it is the primary's; the others
        # come on top, and an older service leaves them out.
        room = room_to_agent(area, sats[0], settings.get(sats[0]) or {}, ring_of(hass, sats[0]))
        if len(sats) > 1:
            room["others"] = [
                {
                    k: v
                    for k, v in room_to_agent(area, s, settings.get(s) or {}, ring_of(hass, s)).items()
                    if k != "area"
                }
                for s in sats[1:]
            ]
        rooms.append(room)
    return {"persons": persons_out, "rooms": rooms, "exposed": exposed_entities(hass)}


async def async_config(
    hass: HomeAssistant,
    entry: ConfigEntry,
    persons: dict[str, dict[str, Any]] | None = None,
    satellites: dict[str, dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """build_config plus the persons' login names (what the agent may not
    read itself)."""
    return build_config(hass, entry, persons, satellites) | {"logins": await person_logins(hass)}


async def person_logins(hass: HomeAssistant) -> dict[str, str]:
    """The login name of each person's Home Assistant user, by person
    entity. A calendar named like it is the person's (docs/spec/calendar.md).
    Only an admin may read users, which the agent's user is not."""
    out: dict[str, str] = {}
    for state in hass.states.async_all("person"):
        user_id = state.attributes.get("user_id")
        if not user_id or (user := await hass.auth.async_get_user(user_id)) is None:
            continue
        for cred in user.credentials:
            if cred.auth_provider_type == "homeassistant" and (login := cred.data.get("username")):
                out[state.entity_id] = str(login)
                break
    return out


def exposed_hash(ids: list[str]) -> str:
    """The fingerprint of the list, as the agent computes it for /v1/status:
    sha256 of the sorted ids joined by newlines."""
    return hashlib.sha256("\n".join(sorted(ids)).encode()).hexdigest()


def exposed_entities(hass: HomeAssistant) -> list[str]:
    """What Home Assistant exposes to Assist, decided the way it does
    itself. The agent's own user may not read that list (it is no admin), so
    the integration hands it over: it is the line for everything Mira reads
    by herself (docs/spec/ha-integration.md)."""
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
    threshold = out.get("voice_threshold")
    if threshold in (None, "", 0):
        out["voice_threshold"] = None
    else:
        try:
            threshold = float(threshold)
        except TypeError, ValueError:
            raise ValueError(f"voice_threshold must be a number, not {threshold!r}") from None
        if not 0 < threshold < 1:
            raise ValueError(f"voice_threshold must be between 0 and 1, not {threshold}")
        out["voice_threshold"] = threshold
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
        "primary": data.get("primary") or "",
    }


async def save_settings(
    hass: HomeAssistant,
    entry: ConfigEntry,
    persons: dict[str, dict[str, Any]] | None = None,
    satellites: dict[str, dict[str, Any]] | None = None,
) -> None:
    """Check the settings with the agent, then store them on the persons and
    satellites. Raises ValueError or the client's errors; nothing is stored
    then. A person's voice threshold that is left out stays as it was: the
    integration's own form does not have it."""
    current = person_settings(hass)
    persons = {
        e: normalize_person({"voice_threshold": current.get(e, {}).get("voice_threshold"), **d})
        for e, d in (persons or {}).items()
    }
    own = own_notify(hass)
    for data in persons.values():
        for c in data["channels"]:
            if c in own:
                raise ValueError(f"{c} is Mira's own and takes messages for her; a channel delivers them")
    # The switch "primary" becomes the area it is the primary of; left out
    # of a form, the mark stays as it was.
    have_sats = satellite_settings(hass)
    for entity, data in list((satellites or {}).items()):
        if not satellite_area(hass, entity):
            raise ValueError(f"{entity} has no area")
        mark = have_sats.get(entity, {}).get("primary") or ""
        if "primary" in data:
            mark = satellite_area(hass, entity) if data["primary"] else ""
        satellites[entity] = normalize_satellite(data | {"primary": mark})
    satellites = satellites or {}
    # A satellite made primary takes the mark from the others in its room.
    for entity, data in list(satellites.items()):
        if not data["primary"]:
            continue
        for other, have in have_sats.items():
            if other != entity and other not in satellites and have.get("primary") == data["primary"]:
                satellites[other] = have | {"primary": ""}
    await entry.runtime_data.client.set_managed_config(await async_config(hass, entry, persons, satellites))
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
    speakers with an area that have settings already. One without an area
    has nothing to set (satellites_without_area)."""
    have = satellite_settings(hass)
    return sorted(
        e.entity_id
        for e in er.async_get(hass).entities.values()
        if not e.disabled_by
        and (e.domain == "assist_satellite" or e.entity_id in have)
        and satellite_area(hass, e.entity_id)
    )


def satellites_without_area(hass: HomeAssistant) -> list[str]:
    """The Assist satellites that are on and have no area, their own or their
    device's: Mira knows no room for them (docs/spec/satellites-rooms.md)."""
    return sorted(
        e.entity_id
        for e in er.async_get(hass).entities.values()
        if e.domain == "assist_satellite" and not e.disabled_by and not satellite_area(hass, e.entity_id)
    )


# A repair per satellite without an area: ISSUE_NO_AREA + its entity id.
ISSUE_NO_AREA = "no_area_"


@callback
def sync_area_repairs(hass: HomeAssistant) -> None:
    """A repair for each satellite without an area, and none for the others:
    what is open is compared with what the registries say now."""
    wanted = {ISSUE_NO_AREA + e: e for e in satellites_without_area(hass)}
    for domain, issue_id in list(ir.async_get(hass).issues):
        if domain == DOMAIN and issue_id.startswith(ISSUE_NO_AREA) and issue_id not in wanted:
            ir.async_delete_issue(hass, DOMAIN, issue_id)
    entities = er.async_get(hass)
    for issue_id, entity in wanted.items():
        entry = entities.async_get(entity)
        link = f"/config/devices/device/{entry.device_id}" if entry and entry.device_id else "/config/entities"
        ir.async_create_issue(
            hass,
            DOMAIN,
            issue_id,
            is_fixable=False,
            severity=ir.IssueSeverity.WARNING,
            translation_key="satellite_no_area",
            translation_placeholders={"name": entity_name(hass, entity), "link": link},
        )


@callback
def clear_area_repairs(hass: HomeAssistant) -> None:
    """Takes away the repairs for satellites without an area."""
    for domain, issue_id in list(ir.async_get(hass).issues):
        if domain == DOMAIN and issue_id.startswith(ISSUE_NO_AREA):
            ir.async_delete_issue(hass, DOMAIN, issue_id)


def satellite_form_settings(hass: HomeAssistant) -> dict[str, dict[str, Any]]:
    """The satellites' settings as the forms show them: "primary" as a
    switch, on where the satellite is the primary of the room it is in."""
    out = {}
    for entity, data in satellite_settings(hass).items():
        out[entity] = dict(data)
        if "primary" in data:
            out[entity]["primary"] = bool(data["primary"]) and data["primary"] == satellite_area(hass, entity)
    return out
