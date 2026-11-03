"""Mira in Home Assistant's backups (docs/spec/backup.md, step 2): the
snapshot laid into the config folder before a backup and gone after it; a
backup without Mira never fails, a repair says why."""

import io
import json
import tarfile

from homeassistant.helpers import issue_registry as ir
from homeassistant.setup import async_setup_component

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


# --- restore (step 3) --------------------------------------------------------


def _snapshot(created="2026-10-31T21:00:00Z", version="0.15.0") -> bytes:
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        raw = json.dumps({"version": version, "created": created}).encode()
        info = tarfile.TarInfo("manifest.json")
        info.size = len(raw)
        tar.addfile(info, io.BytesIO(raw))
    return buf.getvalue()


async def _restored(hass, aioclient_mock, entry, tmp_path) -> bytes:
    """Home Assistant just restored: the snapshot is in the config folder. Returns the snapshot as written:
    gzip stamps the current second into it, so a second _snapshot() may differ."""
    hass.config.config_dir = str(tmp_path)
    (tmp_path / ".storage").mkdir(exist_ok=True)
    snapshot = _snapshot()
    backup.backup_path(hass).write_bytes(snapshot)
    await _setup(hass, aioclient_mock, entry)
    assert await async_setup_component(hass, "repairs", {})
    return snapshot


async def _fix(hass_client, step=None):
    client = await hass_client()
    resp = await client.post("/api/repairs/issues/fix", json={"handler": DOMAIN, "issue_id": backup.ISSUE_RESTORE})
    flow = await resp.json()
    if step is None:
        return client, flow
    resp = await client.post(f"/api/repairs/issues/fix/{flow['flow_id']}", json={"next_step_id": step})
    return client, await resp.json()


def _restore_calls(aioclient_mock):
    return [c for c in aioclient_mock.mock_calls if c[0] == "POST" and str(c[1]).endswith("/v1/restore")]


async def test_BK20_after_a_restore_of_home_assistant_the_repair_offers_miras_data(
    hass, aioclient_mock, entry, tmp_path
):
    await _restored(hass, aioclient_mock, entry, tmp_path)
    issue = ir.async_get(hass).async_get_issue(DOMAIN, backup.ISSUE_RESTORE)
    assert issue is not None and issue.is_fixable
    assert issue.translation_placeholders["version"] == "0.15.0"
    assert issue.translation_placeholders["date"].startswith("2026-1")


async def test_BK21_yes_restores_miras_data(hass, aioclient_mock, entry, tmp_path, hass_client):
    aioclient_mock.post(f"{AGENT_URL}/v1/restore", json={"config": "written", "created": "2026-10-31T21:00:00Z"})
    snapshot = await _restored(hass, aioclient_mock, entry, tmp_path)
    _, flow = await _fix(hass_client)
    assert flow["type"] == "menu" and flow["menu_options"] == ["restore", "discard"]
    _, result = await _fix(hass_client, "restore")
    assert result["type"] == "create_entry"
    assert len(_restore_calls(aioclient_mock)) == 1 and _restore_calls(aioclient_mock)[0][2] == snapshot
    assert not backup.backup_path(hass).exists()
    assert ir.async_get(hass).async_get_issue(DOMAIN, backup.ISSUE_RESTORE) is None


async def test_BK22_ignore_keeps_mira_as_she_is(hass, aioclient_mock, entry, tmp_path, hass_client):
    await _restored(hass, aioclient_mock, entry, tmp_path)
    _, result = await _fix(hass_client, "discard")
    assert result["type"] == "create_entry"
    assert _restore_calls(aioclient_mock) == []
    assert not backup.backup_path(hass).exists()


async def test_BK23_a_refused_restore_says_why(hass, aioclient_mock, entry, tmp_path, hass_client):
    reason = "the snapshot is of a newer Mira: update Mira first, then restore it"
    aioclient_mock.post(f"{AGENT_URL}/v1/restore", status=400, json={"error": reason})
    await _restored(hass, aioclient_mock, entry, tmp_path)
    _, result = await _fix(hass_client, "restore")
    assert result["type"] == "form" and result["step_id"] == "failed"
    assert result["description_placeholders"]["reason"] == reason
    assert backup.backup_path(hass).exists()
