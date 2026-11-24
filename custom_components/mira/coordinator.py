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
from .const import CONF_AGENT_TOKEN, CONF_IMPORTED, DOMAIN, MIN_SERVICE, VERSION
from .mail import show_missing_calendars
from .managed import async_config, exposed_entities, exposed_hash

_LOGGER = logging.getLogger(__name__)

# The repairs when one side is below the other's minimum
# (docs/spec/versions.md, Minimum versions instead of one version); the old
# repair for two different versions is deleted where it is still there.
ISSUE_SERVICE_OLD = "service_too_old"
ISSUE_INTEGRATION_OLD = "integration_too_old"
ISSUE_VERSION_OLD = "version_mismatch"
# The repair while the token an admin gave Mira does not work, or none was
# given yet (docs/spec/hacs-default.md, 4).
ISSUE_ACCESS = "new_access"


def _full(version: Any) -> tuple[int, int, int] | None:
    """major.minor.patch of a version like 0.9.1 or v0.9.1; None for anything
    else ("dev", a build without a version): such a version is never below a
    minimum (docs/spec/versions.md, decision 9)."""
    if m := re.fullmatch(r"v?(\d+)\.(\d+)\.(\d+)", str(version or "")):
        return int(m[1]), int(m[2]), int(m[3])
    return None


def _below(version: Any, minimum: Any) -> bool:
    """version is a version below minimum; False when either is none."""
    v, m = _full(version), _full(minimum)
    return v is not None and m is not None and v < m


# The repair's text per source of a problem Mira reports; an unknown source
# reads as a skill's, as before there were others.
# "ha": Home Assistant's prompt is missing, Mira does not answer
# (docs/spec/context.md, 1.2).
# "model": a model fails for its key or its address (docs/spec/models.md,
# When a model fails).
# "registry": entries of Home Assistant's registries Mira cannot read
# (docs/spec/parsing.md, PA-01).
# "config": mira.yaml cannot be read, Mira runs limited (PA-07).
# "model_file": the setup's model of before cannot move into mira.yaml
# (docs/spec/main-in-file.md in miraassist/mira-app, decision 3).
_PROBLEM_KEYS = {
    "skill": "skill_problem",
    "agent": "agent_problem",
    "hook": "hook_problem",
    "ha": "ha_problem",
    "model": "model_problem",
    "registry": "registry_problem",
    "config": "config_problem",
    "model_file": "model_file_problem",
}


class MiraCoordinator(DataUpdateCoordinator[dict[str, Any]]):
    """Reads /v1/status every minute.

    After every setup it also sends Mira's settings on HA's persons and
    satellites to the service, until that worked once: a service that was down
    or restarted with an empty database gets them on the next poll.
    """

    def __init__(
        self,
        hass: HomeAssistant,
        entry: ConfigEntry,
        client: MiraClient,
        agent_version: str | None = None,
        agent_min_integration: str | None = None,
    ) -> None:
        super().__init__(hass, _LOGGER, config_entry=entry, name=DOMAIN, update_interval=timedelta(seconds=60))
        self.client = client
        self._synced = False
        # From /v1/health at setup; a newer service says it in /v1/status too.
        self.agent_version = agent_version
        self.agent_min_integration = agent_min_integration
        # The versions that were no x.y.z, logged once each.
        self._odd_versions: set[str] = set()
        # The Supervisor was asked once to look for the app's update.
        self._store_reloaded = False

    async def _async_update_data(self) -> dict[str, Any]:
        try:
            status = await self.client.status()
        except MiraError as err:
            raise UpdateFailed(str(err)) from err
        await self._sync(status)
        show_missing_calendars(self.hass, list(status.get("mail_apps") or []))
        await self._check_access(bool(status.get("ha_token_refused")))
        self._show_problems(status.get("problems") or [])
        self._show_version(
            status.get("version") or self.agent_version,
            status.get("min_integration") or self.agent_min_integration,
        )
        return status

    async def _check_access(self, refused: bool = False) -> None:
        """The token an admin gave Mira (docs/spec/hacs-default.md, 4): one
        that does not work (revoked, its user inactive or no admin), or the
        owner's token the integration made itself before, which is revoked
        here, raises the repair asking for a token; nothing is made. A
        service with Home Assistant's token in its own file has no id here:
        nothing to check. refused is the service's ha_token_refused in
        /v1/status (docs/spec/ha-api.md, decision 10): Home Assistant refused
        Mira's token on a call through Mira's interface; it raises the same
        repair until the service reports it no more. An older service sends
        no such field: nothing changes."""
        refresh_id = self.config_entry.data.get(CONF_AGENT_TOKEN)
        if refresh_id:
            revoke_made_token(self.hass, self.config_entry)
        works = not refresh_id or token_works(self.hass, refresh_id)
        if works and not refused:
            ir.async_delete_issue(self.hass, DOMAIN, ISSUE_ACCESS)
            return
        if ir.async_get(self.hass).async_get_issue(DOMAIN, ISSUE_ACCESS) is None:
            _LOGGER.warning(
                "Mira's token does not work (%s); the repair asks an admin for a new one",
                "revoked, or its user is inactive or no admin"
                if not works
                else "Home Assistant refused it on a call through Mira's interface",
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

    def _show_version(self, agent: Any, min_integration: Any = None) -> None:
        """A repair while one side is below the other's minimum
        (docs/spec/versions.md, Minimum versions instead of one version): the
        service below MIN_SERVICE asks to update the service (the HA app on
        Home Assistant OS, else the container), this integration below the
        service's min_integration asks to update the integration. Being apart
        is no problem in itself; the old repair for that is deleted. A service
        without min_integration (older) asks nothing of the integration; a
        version that is no x.y.z is never below a minimum."""
        ir.async_delete_issue(self.hass, DOMAIN, ISSUE_VERSION_OLD)
        for value in (agent, min_integration):
            if value and _full(value) is None and str(value) not in self._odd_versions:
                self._odd_versions.add(str(value))
                _LOGGER.debug("Mira's version %r is no x.y.z: no version repair for it", value)
        service = str(agent or "").lstrip("v")
        if _below(agent, MIN_SERVICE):
            self._ask_for_the_app_update()
            ir.async_create_issue(
                self.hass,
                DOMAIN,
                ISSUE_SERVICE_OLD,
                is_fixable=False,
                severity=ir.IssueSeverity.WARNING,
                translation_key="service_too_old_app" if is_hassio(self.hass) else "service_too_old",
                translation_placeholders={"minimum": MIN_SERVICE, "service": service},
            )
        else:
            ir.async_delete_issue(self.hass, DOMAIN, ISSUE_SERVICE_OLD)
        if _below(VERSION, min_integration):
            ir.async_create_issue(
                self.hass,
                DOMAIN,
                ISSUE_INTEGRATION_OLD,
                is_fixable=False,
                severity=ir.IssueSeverity.WARNING,
                translation_key=ISSUE_INTEGRATION_OLD,
                translation_placeholders={"minimum": str(min_integration).lstrip("v"), "integration": VERSION},
            )
        else:
            ir.async_delete_issue(self.hass, DOMAIN, ISSUE_INTEGRATION_OLD)

    def _ask_for_the_app_update(self) -> None:
        """On Home Assistant OS, the Supervisor reloads its app store once, so
        the HA app's update shows now and not at its next check, when the
        service is below MIN_SERVICE (docs/spec/versions.md, Both updates
        offered together)."""
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
                and issue_id.startswith(("skill_", "agent_", "hook_", "ha_", "model_", "registry_", "config_"))
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
