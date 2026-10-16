"""Mira's web UI as a sidebar panel, through Home Assistant's server."""

from homeassistant.components import frontend

from custom_components.mira.const import CONF_SIDEBAR

from .conftest import AGENT_URL


async def _setup(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_notices": 0, "quiet_mode": False})
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()


async def test_panel_in_sidebar_for_everyone(hass, aioclient_mock, entry):
    await _setup(hass, aioclient_mock, entry)
    panel = hass.data[frontend.DATA_PANELS]["mira"]
    assert panel.config == {"url": "/api/mira/panel/ui"}
    assert not panel.require_admin
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
    assert headers["X-Mira-HA-Admin"] == "1"
    assert headers["X-Mira-HA-User"]
    # Only the UI's API, not the rest of the agent's.
    resp = await client.get("/api/mira/proxy/v1/status")
    assert resp.status == 404


async def test_proxy_needs_login(hass, aioclient_mock, entry, hass_client_no_auth):
    await _setup(hass, aioclient_mock, entry)
    client = await hass_client_no_auth()
    assert (await client.get("/api/mira/proxy/v1/ui/memory")).status == 401


async def test_non_admin_gets_their_memory_only(hass, aioclient_mock, entry, hass_admin_user, hass_client):
    await _setup(hass, aioclient_mock, entry)
    aioclient_mock.get(f"{AGENT_URL}/v1/ui/memory", json={"memories": []})
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/memory/f1", json={"saved": True})
    hass_admin_user.groups = []
    client = await hass_client()
    assert (await client.get("/api/mira/proxy/v1/ui/memory")).status == 200
    _, _, _, headers = aioclient_mock.mock_calls[-1]
    # The agent filters by this user; it trusts the header because the
    # integration's token comes with it.
    assert headers["X-Mira-HA-User"] == hass_admin_user.id
    assert headers["X-Mira-HA-Admin"] == "0"
    assert (await client.put("/api/mira/proxy/v1/ui/memory/f1", json={"text": "x"})).status == 200
    for method, path in [
        ("GET", "/api/mira/proxy/v1/ui/config"),
        ("GET", "/api/mira/proxy/v1/ui/notices"),
        ("POST", "/api/mira/proxy/v1/ui/memory/compact"),
        ("PUT", "/api/mira/proxy/v1/ui/config"),
    ]:
        assert (await client.request(method, path)).status == 401, path


async def test_agent_restarting_is_waited_for(hass, aioclient_mock, entry, hass_client, hass_client_no_auth, monkeypatch):
    import aiohttp

    from custom_components.mira import panel

    monkeypatch.setattr(panel, "CONNECT_WAIT", 0)
    await _setup(hass, aioclient_mock, entry)
    down = aiohttp.ClientConnectorError(None, OSError("connection refused"))
    aioclient_mock.get(f"{AGENT_URL}/v1/ui/memory", exc=down)
    aioclient_mock.get(f"{AGENT_URL}/ui", exc=down)
    client = await hass_client()
    resp = await client.get("/api/mira/proxy/v1/ui/memory")
    # No raw connection error: the page says it in Mira's words and retries.
    assert resp.status == 503
    assert await resp.json() == {"error": "unreachable", "unreachable": True}
    calls = [c for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/ui/memory")]
    assert len(calls) == panel.CONNECT_TRIES
    resp = await (await hass_client_no_auth()).get("/api/mira/panel/ui")
    assert resp.status == 503
    assert "nicht erreichbar" in await resp.text()
