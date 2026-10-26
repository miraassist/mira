"""Polls the agent's status, and keeps the agent's managed config in sync."""

from __future__ import annotations

from datetime import timedelta
import logging
import re
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import CoreState, HomeAssistant
from homeassistant.helpers import issue_registry as ir
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .api import MiraClient, MiraError, MiraInvalidConfig
from .const import CONF_IMPORTED, DOMAIN, VERSION
from .mail import ensure_calendars
from .managed import async_config, exposed_entities, exposed_hash
from .once import sweep

_LOGGER = logging.getLogger(__name__)

# The repair when the agent and this integration are not the same version
# (major.minor; a patch apart is fine): docs/spec/versionen.md.
ISSUE_VERSION = "version_mismatch"


def _minor(version: Any) -> tuple[int, int] | None:
    """major.minor of a version like 0.9.0 or v0.9.0; None for anything else."""
    if m := re.match(r"v?(\d+)\.(\d+)", str(version or "")):
        return int(m[1]), int(m[2])
    return None


class MiraCoordinator(DataUpdateCoordinator[dict[str, Any]]):
    """Reads /v1/status every minute.

    After every setup it also sends Mira's settings on HA's persons and
    satellites to the agent, until that worked once: an agent that was down
    or restarted with an empty database gets them on the next poll.
    """

    def __init__(
        self, hass: HomeAssistant, entry: ConfigEntry, client: MiraClient, agent_version: str | None = None
    ) -> None:
        super().__init__(
            hass, _LOGGER, config_entry=entry, name=DOMAIN, update_interval=timedelta(seconds=60)
        )
        self.client = client
        self._synced = False
        # From /v1/health at setup; a newer agent says it in /v1/status too.
        self.agent_version = agent_version

    async def _async_update_data(self) -> dict[str, Any]:
        # Finished one-time timers go, whether or not the agent answers.
        try:
            await sweep(self.hass)
        except Exception:  # noqa: BLE001 - a failing sweep must not stop the status
            _LOGGER.exception("Cleaning up finished timers failed")
        try:
            status = await self.client.status()
        except MiraError as err:
            raise UpdateFailed(str(err)) from err
        await self._sync(status)
        try:
            await ensure_calendars(self.hass, list(status.get("mail_apps") or []))
        except Exception:  # noqa: BLE001 - a missing calendar must not stop the status
            _LOGGER.exception("Creating the calendars of the mail entries failed")
        self._show_problems(status.get("problems") or [])
        self._show_version(status.get("version") or self.agent_version)
        return status

    def _show_version(self, agent: Any) -> None:
        """A repair while the agent and this integration are not the same
        version, saying which of the two to update; gone once they match."""
        ours, theirs = _minor(VERSION), _minor(agent)
        if ours is None or theirs is None or ours == theirs:
            ir.async_delete_issue(self.hass, DOMAIN, ISSUE_VERSION)
            return
        ir.async_create_issue(
            self.hass,
            DOMAIN,
            ISSUE_VERSION,
            is_fixable=False,
            severity=ir.IssueSeverity.WARNING,
            translation_key="version_agent_older" if theirs < ours else "version_agent_newer",
            translation_placeholders={"integration": VERSION, "agent": str(agent).lstrip("v")},
        )

    def _show_problems(self, problems: list[dict[str, Any]]) -> None:
        """What did not start at the agent (a skill with a wrong setting, an
        agent described in YAML that does not fit) is a repair in Home
        Assistant, in the words the agent logged. A problem that is gone is
        taken away again."""
        wanted: dict[str, dict[str, Any]] = {}
        for problem in problems:
            if isinstance(problem, dict) and problem.get("name"):
                wanted[f"{problem.get('source', 'skill')}_{problem['name']}"] = problem
        registry = ir.async_get(self.hass)
        for domain, issue_id in list(registry.issues):
            if domain == DOMAIN and issue_id.startswith(("skill_", "agent_")) and issue_id not in wanted:
                ir.async_delete_issue(self.hass, DOMAIN, issue_id)
        for issue_id, problem in wanted.items():
            ir.async_create_issue(
                self.hass,
                DOMAIN,
                issue_id,
                is_fixable=False,
                severity=ir.IssueSeverity.WARNING,
                translation_key="agent_problem" if problem.get("source") == "agent" else "skill_problem",
                translation_placeholders={"name": str(problem["name"]), "text": str(problem.get("text", ""))},
            )

    async def _sync(self, status: dict[str, Any]) -> None:
        """Sends the settings once, and the list of what Assist sees again
        whenever the agent's fingerprint of it differs from ours: a device
        that was discovered later, an agent that started with an empty
        database. Not while Home Assistant is still starting: its entities
        are not all there yet, and a short list would replace a complete one
        at the agent."""
        entry = self.config_entry
        if not entry.data.get(CONF_IMPORTED) or self.hass.state is not CoreState.running:
            return
        theirs = status.get("exposed_hash")
        if self._synced and (theirs is None or theirs == exposed_hash(exposed_entities(self.hass))):
            return
        try:
            await self.client.set_managed_config(await async_config(self.hass, entry))
        except (MiraInvalidConfig, ValueError) as err:
            # Retrying cannot fix it; the agent keeps its last valid config.
            _LOGGER.error("The agent refused watchers, persons and rooms from Home Assistant: %s", err)
        except MiraError as err:
            _LOGGER.debug("Sending watchers, persons and rooms waits for the agent: %s", err)
            return
        self._synced = True
