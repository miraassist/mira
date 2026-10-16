"""Mira's web UI as a sidebar panel, through Home Assistant's server."""

from homeassistant.components import frontend

from custom_components.mira.const import CONF_SIDEBAR

from .conftest import AGENT_URL


async def _setup(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_notices": 0, "quiet_mode": False})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


async def test_panel_in_sidebar_for_admins(hass, aioclient_mock, entry):
    await _setup(hass, aioclient_mock, entry)
    panel = hass.data[frontend.DATA_PANELS]["mira"]
    assert panel.config == {"url": "/api/mira/panel/ui"}
    assert panel.require_admin
    await hass.config_entries.async_unload(entry.entry_id)
    assert "mira" not in hass.data[frontend.DATA_PANELS]


async def test_sidebar_can_be_switched_off(hass, aioclient_mock, entry):
    await _setup(hass, aioclient_mock, entry)
    result = await hass.config_entries.options.async_init(entry.entry_id)
    await hass.config_entries.options.async_configure(result["flow_id"], {CONF_SIDEBAR: False})
    await hass.async_block_till_done()
    assert "mira" not in hass.data[frontend.DATA_PANELS]


async def test_videos_come_in_ranges(hass, aioclient_mock, entry, hass_client_no_auth):
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.get(
        f"{AGENT_URL}/ui/guide/img/dialog.mp4",
        status=206,
        content=b"0123",
        headers={"Content-Type": "video/mp4", "Content-Range": "bytes 0-3/100", "Accept-Ranges": "bytes"},
    )
    client = await hass_client_no_auth()
    resp = await client.get("/api/mira/panel/ui/guide/img/dialog.mp4", headers={"Range": "bytes=0-3"})
    assert resp.status == 206
    assert resp.headers["Content-Range"] == "bytes 0-3/100"
    assert await resp.read() == b"0123"
    _, _, _, headers = aioclient_mock.mock_calls[-1]
    assert headers["Range"] == "bytes=0-3"


async def test_page_files_without_login_and_nothing_else(hass, aioclient_mock, entry, hass_client_no_auth):
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.get(f"{AGENT_URL}/ui", text="<html>Mira</html>", headers={"Content-Type": "text/html"})
    client = await hass_client_no_auth()
    resp = await client.get("/api/mira/panel/ui")
    assert resp.status == 200
    assert await resp.text() == "<html>Mira</html>"
    # The open view passes the page's files only, never the API.
    resp = await client.get("/api/mira/panel/v1/ui/memory")
    assert resp.status == 404


async def test_api_through_proxy_with_agent_token(hass, aioclient_mock, entry, hass_client):
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/memory/f1", json={"ok": True})
    client = await hass_client()
    resp = await client.put("/api/mira/proxy/v1/ui/memory/f1", json={"text": "Neu."})
    assert resp.status == 200
    assert await resp.json() == {"ok": True}
    method, url, data, headers = aioclient_mock.mock_calls[-1]
    assert (method, str(url)) == ("PUT", f"{AGENT_URL}/v1/ui/memory/f1")
    assert headers["Authorization"] == "Bearer secret"
    # Only the UI's API, not the rest of the agent's.
    resp = await client.get("/api/mira/proxy/v1/status")
    assert resp.status == 404


async def test_proxy_needs_login_and_admin(hass, aioclient_mock, entry, hass_client_no_auth, hass_admin_user, hass_client):
    await _setup(hass, aioclient_mock, entry)
    client = await hass_client_no_auth()
    assert (await client.get("/api/mira/proxy/v1/ui/memory")).status == 401
    hass_admin_user.groups = []
    client = await hass_client()
    assert (await client.get("/api/mira/proxy/v1/ui/memory")).status == 401
