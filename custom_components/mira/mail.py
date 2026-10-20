"""The mail calendar (docs/spec/mail-kalender.md): passes the mails of IMAP
entries named like a mail application on to the agent, and keeps the
calendars they are written into.

Mira's own Home Assistant user may not listen to imap_content (admins only),
so the integration does. The name of the IMAP entry's device is the switch
(the device is what one renames after setting up an entry; the entry's own
title does not count): "Pakete" belongs to the house, "Pakete stefan" to the
person with that login; the calendar has the same name.
"""

from __future__ import annotations

from datetime import datetime
import logging
from typing import Any

from homeassistant.components.homeassistant.exposed_entities import async_expose_entity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import CALLBACK_TYPE, CoreState, Event, HomeAssistant, callback
from homeassistant.helpers import device_registry as dr, entity_registry as er

from .api import MiraError

_LOGGER = logging.getLogger(__name__)

EVENT_IMAP = "imap_content"

# Calendars that could not be created, told once and not tried again until
# Home Assistant restarts.
_failed: set[str] = set()


def mail_entries(hass: HomeAssistant, apps: list[str]) -> list[tuple[ConfigEntry, str]]:
    """The IMAP entries whose device's name starts with a mail application,
    each with that name."""
    out = []
    for entry in hass.config_entries.async_entries("imap"):
        name = device_name(hass, entry.entry_id)
        if app_of(name, apps):
            out.append((entry, name))
    return out


def device_name(hass: HomeAssistant, entry_id: str) -> str:
    """The name of an IMAP entry's device as the user sees it ("" without a
    device)."""
    for device in dr.async_entries_for_config_entry(dr.async_get(hass), entry_id):
        return calendar_name(device.name_by_user or device.name or "")
    return ""


def app_of(title: str, apps: list[str]) -> str:
    """The application an entry's name belongs to ("Pakete stefan" is
    pakete); "" when none."""
    words = title.lower().split()
    return words[0] if words and words[0] in apps else ""


def calendar_name(title: str) -> str:
    return " ".join(title.split())


@callback
def async_setup_mail(hass: HomeAssistant, entry: ConfigEntry) -> CALLBACK_TYPE:
    """Listens to new mails of the IMAP entries."""

    async def handle(event: Event) -> None:
        data = event.data
        # Not a new mail: the same last mail once more.
        if not data.get("initial"):
            return
        coordinator = entry.runtime_data.coordinator
        apps = list((coordinator.data or {}).get("mail_apps") or [])
        imap = hass.config_entries.async_get_entry(str(data.get("entry_id", "")))
        name = device_name(hass, imap.entry_id) if imap else ""
        if imap is None or not app_of(name, apps):
            return
        try:
            mail = await hass.services.async_call(
                "imap", "fetch", {"entry": imap.entry_id, "uid": data["uid"]}, blocking=True, return_response=True
            )
        except Exception as err:  # noqa: BLE001 - one mail must not stop the next
            _LOGGER.warning("Could not fetch the mail of %s: %s", name, err)
            return
        mail = mail or {}
        try:
            out = await entry.runtime_data.client.mail(
                {
                    "entry": name,
                    "sender": str(mail.get("sender") or data.get("sender") or ""),
                    "subject": str(mail.get("subject") or data.get("subject") or ""),
                    "date": _date(mail.get("date")),
                    "text": str(mail.get("text") or ""),
                }
            )
        except MiraError as err:
            _LOGGER.warning("The agent could not read the mail of %s: %s", name, err)
            return
        _LOGGER.debug("Mail of %s: %s", name, out)

    return hass.bus.async_listen(EVENT_IMAP, handle)


def _date(value: Any) -> str | None:
    """The mail's date with its offset, or None (the agent then takes now)."""
    if not value:
        return None
    try:
        date = datetime.fromisoformat(str(value))
    except ValueError:
        return None
    return date.isoformat() if date.tzinfo else None


async def ensure_calendars(hass: HomeAssistant, apps: list[str]) -> None:
    """A mail entry without its calendar gets a local calendar of that name,
    exposed to Assist."""
    if not apps or hass.state is not CoreState.running:
        return
    have = {
        calendar_name(str(s.attributes.get("friendly_name") or "")).lower()
        for s in hass.states.async_all("calendar")
    }
    for _imap, name in mail_entries(hass, apps):
        if name.lower() in have or name.lower() in _failed:
            continue
        result = await hass.config_entries.flow.async_init(
            "local_calendar", context={"source": "user"}, data={"calendar_name": name}
        )
        created = result.get("result")
        if not isinstance(created, ConfigEntry):
            _failed.add(name.lower())
            _LOGGER.warning("Could not create the calendar %s: %s", name, result.get("reason", result.get("type")))
            continue
        for reg in er.async_entries_for_config_entry(er.async_get(hass), created.entry_id):
            if reg.domain == "calendar":
                async_expose_entity(hass, "conversation", reg.entity_id, True)
        _LOGGER.info("Created the calendar %s for the mails of the IMAP device of the same name", name)
        have.add(name.lower())
