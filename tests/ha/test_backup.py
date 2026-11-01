"""Mira in Home Assistant's backups (docs/spec/backup.md, step 2): the
snapshot laid into the config folder before a backup and gone after it; a
backup without Mira never fails, a repair says why."""

from homeassistant.helpers import issue_registry as ir

from custom_components.mira import backup
from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

STATUS = {"pending_messages": 0, "quiet_mode": False}
SNAPSHOT = b"\x1f\x8b fake snapshot"


def _repair(hass):
    return ir.async_get(hass).async_get_issue(DOMAIN, backup.ISSUE_BACKUP)


async def _setup(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


async def test_BK10_before_a_backup_the_snapshot_is_laid_into_the_config_folder(hass, aioclient_mock, entry, tmp_path):
    hass.config.config_dir = str(tmp_path)
    (tmp_path / ".storage").mkdir(exist_ok=True)
    aioclient_mock.get(f"{AGENT_URL}/v1/backup", content=SNAPSHOT)
    await _setup(hass, aioclient_mock, entry)
    await backup.async_pre_backup(hass)
    assert backup.backup_path(hass).read_bytes() == SNAPSHOT
    assert _repair(hass) is None
    await backup.async_post_backup(hass)
    assert not backup.backup_path(hass).exists()
    # Home Assistant's backup finds the platform
    from homeassistant.components.backup.const import DATA_MANAGER
    from homeassistant.setup import async_setup_component

    assert await async_setup_component(hass, "backup", {})
    await hass.async_block_till_done()
    assert DOMAIN in hass.data[DATA_MANAGER].platforms


async def test_BK11_mira_not_reachable_does_not_stop_the_backup(hass, aioclient_mock, entry, tmp_path):
    hass.config.config_dir = str(tmp_path)
    (tmp_path / ".storage").mkdir(exist_ok=True)
    aioclient_mock.get(f"{AGENT_URL}/v1/backup", exc=TimeoutError())
    await _setup(hass, aioclient_mock, entry)
    await backup.async_pre_backup(hass)  # does not raise
    assert not backup.backup_path(hass).exists()
    issue = _repair(hass)
    assert issue is not None and issue.translation_key == "backup_missing_unreachable"
    # not connected at all: the same
    await hass.config_entries.async_unload(entry.entry_id)
    ir.async_delete_issue(hass, DOMAIN, backup.ISSUE_BACKUP)
    await backup.async_pre_backup(hass)
    assert _repair(hass).translation_key == "backup_missing_unreachable"


async def test_BK12_an_older_mira_does_not_stop_the_backup(hass, aioclient_mock, entry, tmp_path):
    hass.config.config_dir = str(tmp_path)
    (tmp_path / ".storage").mkdir(exist_ok=True)
    aioclient_mock.get(f"{AGENT_URL}/v1/backup", status=404)
    await _setup(hass, aioclient_mock, entry)
    await backup.async_pre_backup(hass)
    assert not backup.backup_path(hass).exists()
    assert _repair(hass).translation_key == "backup_missing_old"


async def test_BK13_the_repair_goes_with_the_next_backup_that_has_mira(hass, aioclient_mock, entry, tmp_path):
    hass.config.config_dir = str(tmp_path)
    (tmp_path / ".storage").mkdir(exist_ok=True)
    aioclient_mock.get(f"{AGENT_URL}/v1/backup", status=404)
    await _setup(hass, aioclient_mock, entry)
    await backup.async_pre_backup(hass)
    assert _repair(hass) is not None
    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json=STATUS)
    aioclient_mock.get(f"{AGENT_URL}/v1/backup", content=SNAPSHOT)
    await backup.async_pre_backup(hass)
    assert _repair(hass) is None
    await backup.async_post_backup(hass)
