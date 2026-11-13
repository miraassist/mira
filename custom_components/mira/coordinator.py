"""Polls the service's status, and keeps the service's managed config in sync."""

from __future__ import annotations

import logging
import re
from datetime import timedelta
from typing import Any

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import CoreState, HomeAssistant
from homeassistant.helpers import issue_registry as ir
from homeassistant.helpers.hassio import is_hassio
from homeassistant.helpers.update_coordinator import DataUpdateCoordinator, UpdateFailed

from .access import revoke_made_token, token_works
from .api import MiraClient, MiraError, MiraInvalidConfig
from .const import CONF_AGENT_TOKEN, CONF_IMPORTED, DOMAIN, VERSION
from .mail import show_missing_calendars
from .managed import async_config, exposed_entities, exposed_hash

_LOGGER = logging.getLogger(__name__)

# The repair when the service and this integration are not the same version
# (major.minor; a patch apart is fine): docs/spec/versions.md.
ISSUE_VERSION = "version_mismatch"
# The repair while the token an admin gave Mira does not work, or none was
# given yet (docs/spec/hacs-default.md, 4).
ISSUE_ACCESS = "new_access"


def _minor(version: Any) -> tuple[int, int] | None:
    """major.minor of a version like 0.9.0 or v0.9.0; None for anything else."""
    if m := re.match(r"v?(\d+)\.(\d+)", str(version or "")):
        return int(m[1]), int(m[2])
    return None


def _full(version: Any) -> tuple[int, int, int] | None:
    """major.minor.patch of a version like 0.9.1 or v0.9 (a missing patch is
    0); None for anything else."""
    if m := re.match(r"v?(\d+)\.(\d+)(?:\.(\d+))?", str(version or "")):
        return int(m[1]), int(m[2]), int(m[3] or 0)
    return None


# The repair's text per source of a problem Mira reports; an unknown source
# reads as a skill's, as before there were others.
# "ha": Home Assistant's prompt is missing, Mira does not answer
# (docs/spec/context.md, 1.2).
# "model": a model fails for its key or its address (docs/spec/models.md,
# When a model fails).
_PROBLEM_KEYS = {
    "skill": "skill_problem",
    "agent": "agent_problem",
    "hook": "hook_problem",
    "ha": "ha_problem",
    "model": "model_problem",
}


class MiraCoordinator(DataUpdateCoordinator[dict[str, Any]]):
    """Reads /v1/status every minute.

    After every setup it also sends Mira's settings on HA's persons and
    satellites to the service, until that worked once: a service that was down
    or restarted with an empty database gets them on the next poll.
    """

    def __init__(
        self, hass: HomeAssistant, entry: ConfigEntry, client: MiraClient, agent_version: str | None = None
    ) -> None:
        super().__init__(hass, _LOGGER, config_entry=entry, name=DOMAIN, update_interval=timedelta(seconds=60))
        self.client = client
        self._synced = False
        # From /v1/health at setup; a newer service says it in /v1/status too.
        self.agent_version = agent_version
        # The Supervisor was asked once to look for the app's update.
        self._store_reloaded = False

    async def _async_update_data(self) -> dict[str, Any]:
        try:
            status = await self.client.status()
        except MiraError as err:
            raise UpdateFailed(str(err)) from err
        await self._sync(status)
        show_missing_calendars(self.hass, list(status.get("mail_apps") or []))
        await self._check_access()
        self._show_problems(status.get("problems") or [])
        self._show_version(status.get("version") or self.agent_version)
        return status

    async def _check_access(self) -> None:
        """The token an admin gave Mira (docs/spec/hacs-default.md, 4): one
        that does not work (revoked, its user inactive or no admin), or the
        owner's token the integration made itself before, which is revoked
        here, raises the repair asking for a token; nothing is made. A
        service with Home Assistant's token in its own file has no id here:
        nothing to check."""
        refresh_id = self.config_entry.data.get(CONF_AGENT_TOKEN)
        if not refresh_id:
            return
        revoke_made_token(self.hass, self.config_entry)
        if token_works(self.hass, refresh_id):
            ir.async_delete_issue(self.hass, DOMAIN, ISSUE_ACCESS)
            return
        if ir.async_get(self.hass).async_get_issue(DOMAIN, ISSUE_ACCESS) is None:
            _LOGGER.warning(
                "Mira's token does not work (revoked, or its user is inactive or no admin); "
                "the repair asks an admin for a new one"
            )
        ir.async_create_issue(
            self.hass,
            DOMAIN,
            ISSUE_ACCESS,
            is_fixable=True,
            is_persistent=False,
            severity=ir.IssueSeverity.ERROR,
            translation_key=ISSUE_ACCESS,
        )

    def _show_version(self, agent: Any) -> None:
        """A repair while the service and this integration are not the same
        version, saying which of the two to update; gone once they match. The
        app's update is asked for a patch apart too: the repair leaves that
        alone, the Supervisor offers it all the same (docs/spec/versions.md)."""
        full_ours, full_theirs = _full(VERSION), _full(agent)
        if full_ours is not None and full_theirs is not None and full_theirs < full_ours:
            self._ask_for_the_app_update()
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

    def _ask_for_the_app_update(self) -> None:
        """On Home Assistant OS, the Supervisor reloads its app store once, so
        the HA app's update shows now and not at its next check
        (docs/spec/versions.md, Both updates offered together)."""
        if self._store_reloaded or not is_hassio(self.hass):
            return
        self._store_reloaded = True
        self.hass.async_create_task(self._reload_store())

    async def _reload_store(self) -> None:
        # Imported here: the Supervisor exists only on Home Assistant OS.
        from homeassistant.components.hassio import get_supervisor_client

        try:
            await get_supervisor_client(self.hass).store.reload()
        except Exception as err:  # noqa: BLE001
            _LOGGER.warning("Could not ask the Supervisor for the app's update (%s); it comes at its next check", err)
            return
        _LOGGER.info("Asked the Supervisor to look for the app's update (integration %s)", VERSION)

    def _show_problems(self, problems: list[dict[str, Any]]) -> None:
        """What did not start at the service (a skill with a wrong setting, an
        agent described in YAML that does not fit) is a repair in Home
        Assistant, in the words the service logged. A problem that is gone is
        taken away again."""
        wanted: dict[str, dict[str, Any]] = {}
        for problem in problems:
            if isinstance(problem, dict) and problem.get("name"):
                wanted[f"{problem.get('source', 'skill')}_{problem['name']}"] = problem
        registry = ir.async_get(self.hass)
        for domain, issue_id in list(registry.issues):
            if (
                domain == DOMAIN
                and issue_id.startswith(("skill_", "agent_", "hook_", "ha_", "model_"))
                and issue_id not in wanted
            ):
                ir.async_delete_issue(self.hass, DOMAIN, issue_id)
        for issue_id, problem in wanted.items():
            ir.async_create_issue(
                self.hass,
                DOMAIN,
                issue_id,
                is_fixable=False,
                severity=ir.IssueSeverity.WARNING,
                translation_key=_PROBLEM_KEYS.get(str(problem.get("source")), "skill_problem"),
                translation_placeholders={"name": str(problem["name"]), "text": str(problem.get("text", ""))},
            )

    async def _sync(self, status: dict[str, Any]) -> None:
        """Sends the settings once, and the list of what Assist sees again
        whenever the service's fingerprint of it differs from ours: a device
        that was discovered later, a service that started with an empty
        database. Not while Home Assistant is still starting: its entities
        are not all there yet, and a short list would replace a complete one
        at the service."""
        entry = self.config_entry
        if not entry.data.get(CONF_IMPORTED) or self.hass.state is not CoreState.running:
            return
        theirs = status.get("exposed_hash")
        if self._synced and (theirs is None or theirs == exposed_hash(exposed_entities(self.hass))):
            return
        try:
            await self.client.set_managed_config(await async_config(self.hass, entry))
        except (MiraInvalidConfig, ValueError) as err:
            # Retrying cannot fix it; the service keeps its last valid config.
            _LOGGER.error("The service refused watchers, persons and rooms from Home Assistant: %s", err)
        except MiraError as err:
            _LOGGER.debug("Sending watchers, persons and rooms waits for the service: %s", err)
            return
        self._synced = True
