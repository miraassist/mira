"""The repairs a user fixes in Home Assistant: restoring Mira's data after a
restore of Home Assistant (docs/spec/backup.md), creating a mail entry's
calendar, and a token an admin gives Mira when hers does not work
(docs/spec/hacs-default.md, 4)."""

from __future__ import annotations

from typing import Any

from homeassistant import data_entry_flow
from homeassistant.components.repairs import RepairsFlow
from homeassistant.config_entries import ConfigEntryState
from homeassistant.core import HomeAssistant
from homeassistant.exceptions import HomeAssistantError
from homeassistant.helpers import issue_registry as ir

from .access import HA_TOKEN_SCHEMA, check_token, give_token
from .api import MiraError
from .backup import ISSUE_RESTORE, async_discard, async_restore
from .const import DOMAIN
from .coordinator import ISSUE_ACCESS
from .mail import ISSUE_CALENDAR, calendar_issue, create_calendar


class RestoreFlow(RepairsFlow):
    """Restore Mira's data from the snapshot, or keep Mira as she is."""

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        return self.async_show_menu(step_id="init", menu_options=["restore", "discard"])

    async def async_step_restore(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        entries = [e for e in self.hass.config_entries.async_entries(DOMAIN) if e.state is ConfigEntryState.LOADED]
        if not entries:
            return await self.async_step_failed(reason="Mira is not connected")
        try:
            await async_restore(self.hass, entries[0].runtime_data.client)
        except MiraError as err:
            return await self.async_step_failed(reason=str(err))
        return self.async_create_entry(data={})

    async def async_step_failed(
        self, user_input: dict[str, Any] | None = None, reason: str = ""
    ) -> data_entry_flow.FlowResult:
        """Mira's reason; the snapshot stays, back to the choice."""
        if user_input is not None:
            return await self.async_step_init()
        return self.async_show_form(step_id="failed", description_placeholders={"reason": reason})

    async def async_step_discard(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        await async_discard(self.hass)
        return self.async_create_entry(data={})


class CalendarFlow(RepairsFlow):
    """Create the calendar a mail entry writes into, on the admin's word."""

    def __init__(self, name: str) -> None:
        self.name = name

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        return await self.async_step_confirm()

    async def async_step_confirm(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        if user_input is None:
            return self.async_show_form(step_id="confirm", description_placeholders={"name": self.name})
        try:
            exposed = await create_calendar(self.hass, self.name)
        except HomeAssistantError as err:
            return await self.async_step_failed(reason=str(err))
        ir.async_delete_issue(self.hass, DOMAIN, calendar_issue(self.name))
        if not exposed:
            return self.async_abort(reason="calendar_not_exposed", description_placeholders={"name": self.name})
        return self.async_create_entry(data={})

    async def async_step_failed(
        self, user_input: dict[str, Any] | None = None, reason: str = ""
    ) -> data_entry_flow.FlowResult:
        """The reason; the repair stays, back to the question."""
        if user_input is not None:
            return await self.async_step_confirm()
        return self.async_show_form(step_id="failed", description_placeholders={"name": self.name, "reason": reason})


class AccessFlow(RepairsFlow):
    """A token an admin pastes for Mira, checked as at the setup."""

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        return await self.async_step_confirm()

    async def async_step_confirm(self, user_input: dict[str, Any] | None = None) -> data_entry_flow.FlowResult:
        errors: dict[str, str] = {}
        if user_input is not None:
            refresh, error = check_token(self.hass, user_input["token"])
            if error:
                errors["token"] = error
            else:
                entries = [
                    e for e in self.hass.config_entries.async_entries(DOMAIN) if e.state is ConfigEntryState.LOADED
                ]
                if not entries:
                    return await self.async_step_failed(reason="Mira is not connected")
                try:
                    await give_token(
                        self.hass, entries[0], entries[0].runtime_data.client, user_input["token"], refresh
                    )
                except (MiraError, RuntimeError) as err:
                    return await self.async_step_failed(reason=str(err))
                ir.async_delete_issue(self.hass, DOMAIN, ISSUE_ACCESS)
                return self.async_create_entry(data={})
        return self.async_show_form(step_id="confirm", data_schema=HA_TOKEN_SCHEMA, errors=errors)

    async def async_step_failed(
        self, user_input: dict[str, Any] | None = None, reason: str = ""
    ) -> data_entry_flow.FlowResult:
        """Mira's reason; the repair stays, back to the question."""
        if user_input is not None:
            return await self.async_step_confirm()
        return self.async_show_form(step_id="failed", description_placeholders={"reason": reason})


async def async_create_fix_flow(hass: HomeAssistant, issue_id: str, data: dict[str, Any] | None) -> RepairsFlow:
    if issue_id == ISSUE_RESTORE:
        return RestoreFlow()
    if issue_id == ISSUE_ACCESS:
        return AccessFlow()
    if issue_id.startswith(ISSUE_CALENDAR + "_") and data and data.get("name"):
        return CalendarFlow(str(data["name"]))
    raise ValueError(f"no fix for {issue_id}")
