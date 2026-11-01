"""Mira in Home Assistant's backups (docs/spec/backup.md): before a backup the
integration lays Mira's snapshot into Home Assistant's config folder, which
the backup takes along, and deletes it afterwards. It never stops the
backup: without Mira, a repair says why."""

from __future__ import annotations

import logging
from pathlib import Path

from homeassistant.config_entries import ConfigEntryState
from homeassistant.core import HomeAssistant
from homeassistant.helpers import issue_registry as ir

from .api import MiraError, MiraNotFound
from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

# The snapshot's place in the config folder: only ever there inside a backup,
# and after a restore.
BACKUP_FILE = "mira_backup.tar.gz"
ISSUE_BACKUP = "backup_missing"


def backup_path(hass: HomeAssistant) -> Path:
    return Path(hass.config.path(".storage", BACKUP_FILE))


async def async_pre_backup(hass: HomeAssistant) -> None:
    """Fetches Mira's snapshot into the config folder. Any failure is caught:
    an error here would stop Home Assistant's whole backup."""
    entries = hass.config_entries.async_entries(DOMAIN)
    if not entries:
        return
    loaded = [e for e in entries if e.state is ConfigEntryState.LOADED]
    try:
        if not loaded:
            _missing(hass, "backup_missing_unreachable", "Mira is not connected")
            return
        data = await loaded[0].runtime_data.client.backup()
        await hass.async_add_executor_job(_write, backup_path(hass), data)
    except MiraNotFound:
        _missing(hass, "backup_missing_old", "Mira has no GET /v1/backup")
    except MiraError as err:
        _missing(hass, "backup_missing_unreachable", str(err))
    except Exception as err:  # noqa: BLE001 - never fail Home Assistant's backup
        _missing(hass, "backup_missing_error", str(err), error=str(err))
    else:
        ir.async_delete_issue(hass, DOMAIN, ISSUE_BACKUP)
        _LOGGER.info("Mira's snapshot is in the backup (%d bytes)", len(data))


async def async_post_backup(hass: HomeAssistant) -> None:
    """Deletes the snapshot from the config folder."""
    try:
        await hass.async_add_executor_job(backup_path(hass).unlink, True)
    except OSError as err:
        _LOGGER.warning("Could not delete Mira's snapshot after the backup: %s", err)


def _write(path: Path, data: bytes) -> None:
    tmp = path.with_suffix(".part")
    tmp.write_bytes(data)
    tmp.replace(path)


def _missing(hass: HomeAssistant, key: str, why: str, error: str = "") -> None:
    _LOGGER.warning("Mira is not in this backup: %s", why)
    ir.async_create_issue(
        hass,
        DOMAIN,
        ISSUE_BACKUP,
        is_fixable=False,
        severity=ir.IssueSeverity.WARNING,
        translation_key=key,
        translation_placeholders={"error": error},
    )
