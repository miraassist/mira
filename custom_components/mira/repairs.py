"""The repairs a user fixes in Home Assistant: restoring Mira's data after a
restore of Home Assistant (docs/spec/backup.md)."""

from __future__ import annotations

from typing import Any

from homeassistant import data_entry_flow
from homeassistant.components.repairs import RepairsFlow
from homeassistant.config_entries import ConfigEntryState
from homeassistant.core import HomeAssistant

from .api import MiraError
from .backup import ISSUE_RESTORE, async_discard, async_restore
from .const import DOMAIN


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


async def async_create_fix_flow(hass: HomeAssistant, issue_id: str, data: dict[str, Any] | None) -> RepairsFlow:
    if issue_id == ISSUE_RESTORE:
        return RestoreFlow()
    raise ValueError(f"no fix for {issue_id}")
