"""The mail calendar (docs/spec/mail-calendar.md): passes the mails of IMAP
entries named like a mail application on to the agent, and keeps the
calendars they are written into.

Mira's own Home Assistant user may not listen to imap_content (admins only),
so the integration does. The name of the IMAP entry's device is the switch
(the device is what one renames after setting up an entry; the entry's own
title does not count): "Pakete" belongs to the house, "Pakete stefan" to the
person with that login; the calendar has the same name. A missing calendar
is a repair the admin confirms (docs/spec/hacs-default.md, decision 3).
"""

from __future__ import annotations

import logging
from datetime import datetime
from typing import Any

from homeassistant.components.homeassistant.exposed_entities import async_expose_entity
from homeassistant.config_entries import ConfigEntry
from homeassistant.core import CALLBACK_TYPE, CoreState, Event, HomeAssistant, callback
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import device_registry as dr
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers import issue_registry as ir

from .api import MiraError
from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

EVENT_IMAP = "imap_content"

ISSUE_CALENDAR = "mail_calendar"


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
        data_ = coordinator.data or {}
        # The mail calendar's applications and Mira's inbox ("Postfach anna",
        # docs/spec/inbox.md, step 6); an older agent names no inbox_apps.
        apps = list(data_.get("mail_apps") or []) + list(data_.get("inbox_apps") or [])
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
                    # Optional: the service knows an inbox mail by its entry (the uid is only logged).
                    "imap_entry": imap.entry_id,
                    "uid": str(data.get("uid") or ""),
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


@callback
def show_missing_calendars(hass: HomeAssistant, apps: list[str]) -> None:
    """A repair for each mail entry without its calendar; one whose calendar
    is there, or whose entry is gone, goes."""
    if hass.state is not CoreState.running:
        return
    have = {
        calendar_name(str(s.attributes.get("friendly_name") or "")).lower() for s in hass.states.async_all("calendar")
    }
    wanted = {calendar_issue(name): name for _imap, name in mail_entries(hass, apps) if name.lower() not in have}
    for domain, issue_id in list(ir.async_get(hass).issues):
        if domain == DOMAIN and issue_id.startswith(ISSUE_CALENDAR + "_") and issue_id not in wanted:
            ir.async_delete_issue(hass, DOMAIN, issue_id)
    for issue_id, name in wanted.items():
        ir.async_create_issue(
            hass,
            DOMAIN,
            issue_id,
            is_fixable=True,
            is_persistent=False,
            severity=ir.IssueSeverity.WARNING,
            translation_key=ISSUE_CALENDAR,
            translation_placeholders={"name": name},
            data={"name": name},
        )


def calendar_issue(name: str) -> str:
    return f"{ISSUE_CALENDAR}_{'_'.join(name.lower().split())}"


async def create_calendar(hass: HomeAssistant, name: str) -> bool:
    """The local calendar of this name, exposed to Assist; the admin asked
    for it in the repair. False when it was made but could not be exposed;
    raises HomeAssistantError with the reason when it was not made."""
    try:
        result = await hass.config_entries.flow.async_init(
            "local_calendar", context={"source": "user"}, data={"calendar_name": name}
        )
    except HomeAssistantError as err:
        reason = str(err) or type(err).__name__
        _LOGGER.warning("Could not create the calendar %s: %s", name, reason)
        raise HomeAssistantError(reason) from err
    created = result.get("result")
    if not isinstance(created, ConfigEntry):
        reason = str(result.get("reason") or result.get("errors") or result.get("type"))
        _LOGGER.warning("Could not create the calendar %s: %s", name, reason)
        raise HomeAssistantError(reason)
    calendars = [
        reg.entity_id
        for reg in er.async_entries_for_config_entry(er.async_get(hass), created.entry_id)
        if reg.domain == "calendar"
    ]
    if not calendars:
        _LOGGER.warning("Created the calendar %s, but found no entity of it to expose to Assist", name)
        return False
    for entity_id in calendars:
        async_expose_entity(hass, "conversation", entity_id, True)
    _LOGGER.info("Created the calendar %s for the mails of the IMAP device of the same name", name)
    return True
