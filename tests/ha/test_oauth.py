"""A skill's connection: the login through Home Assistant, the tokens to the
agent (docs/spec/oauth.md, step 3)."""

import base64
from urllib.parse import parse_qs, urlsplit

import pytest
from homeassistant.helpers import config_entry_oauth2_flow

from custom_components.mira import oauth
from custom_components.mira.const import DOMAIN

from .conftest import AGENT_URL

LOGIN = {
    "skill": "x",
    "name": "X",
    "authorize_url": "https://service.example/authorize",
    "token_url": "https://service.example/token",
    "scopes": ["tweet.read", "offline.access"],
    "pkce": True,
    "token_auth": "basic",
    "authorize_params": {"prompt": "consent"},
    "client_id": "id-1",
    "client_secret": "geheim",
}
# Without "my" the redirect is the instance's own, from this header.
FRONTEND = {"HA-Frontend-Base": "https://ha.example"}
CALLBACK = "https://ha.example/auth/external/callback"
BASIC = hasattr(config_entry_oauth2_flow.LocalOAuth2Implementation, "token_auth_method")


@pytest.fixture
async def mira(hass, aioclient_mock, entry, monkeypatch):
    """Mira set up, with a skill x whose login it gives out."""
    monkeypatch.setattr(oauth, "SUPPORTED", True)
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False})
    aioclient_mock.get(f"{AGENT_URL}/v1/oauth/x", json=LOGIN)
    assert await hass.config_entries.async_setup(entry.entry_id)
    await hass.async_block_till_done()
    return entry


async def _start(client):
    resp = await client.post("/api/mira/oauth/x/start", headers=FRONTEND)
    assert resp.status == 200, await resp.text()
    return (await resp.json())["url"]


# CO-09 Connect opens the service's login with Home Assistant's redirect
async def test_co09_connect_opens_the_login_with_home_assistants_redirect(hass, mira, hass_client):
    # Given the page inside Home Assistant and a skill with client id and secret
    client = await hass_client()
    # When the user clicks Connect
    url = await _start(client)
    # Then a window opens at the skill's authorize_url
    parts = urlsplit(url)
    q = {k: v[0] for k, v in parse_qs(parts.query).items()}
    assert f"{parts.scheme}://{parts.netloc}{parts.path}" == LOGIN["authorize_url"]
    assert q["client_id"] == "id-1"
    assert q["scope"] == "tweet.read offline.access"
    assert q["prompt"] == "consent"
    assert q["code_challenge_method"] == "S256"
    assert "geheim" not in url
    # And its redirect address is Home Assistant's, with a state signed by Home Assistant
    assert q["redirect_uri"] == CALLBACK
    state = config_entry_oauth2_flow._decode_jwt(hass, q["state"])
    assert state["flow_id"]
    # The flow shows up nowhere: it is no discovery and no user flow.
    flows = hass.config_entries.flow.async_progress()
    assert [f["context"]["source"] for f in flows] == [oauth.SOURCE_CONNECT]


# CO-10 The redirect hands the tokens to Mira and creates no config entry
async def test_co10_the_redirect_hands_the_tokens_to_mira(hass, mira, aioclient_mock, hass_client, hass_client_no_auth):
    # Given a login started for a skill
    url = await _start(await hass_client())
    state = parse_qs(urlsplit(url).query)["state"][0]
    aioclient_mock.post(
        LOGIN["token_url"],
        json={"access_token": "at-1", "refresh_token": "rt-1", "token_type": "bearer", "expires_in": 7200},
    )
    aioclient_mock.put(f"{AGENT_URL}/v1/oauth/x/token", json={"connected": True})
    # When the service redirects with a code
    resp = await (await hass_client_no_auth()).get(f"/auth/external/callback?code=c-1&state={state}")
    assert resp.status == 200
    await hass.async_block_till_done()
    # Then the integration exchanges it at token_url and puts the tokens to the Mira service
    calls = {(m, str(u)): (d, h) for m, u, d, h in aioclient_mock.mock_calls}
    data, headers = calls[("POST", LOGIN["token_url"])]
    assert data["code"] == "c-1"
    assert data["code_verifier"]
    if BASIC:
        assert headers["Authorization"] == "Basic " + base64.b64encode(b"id-1:geheim").decode()
        assert "client_secret" not in data
    put, _ = calls[("PUT", f"{AGENT_URL}/v1/oauth/x/token")]
    assert put["access_token"] == "at-1"
    assert put["refresh_token"] == "rt-1"
    assert put["expires_at"] > 0
    # And Home Assistant has no new config entry and the page shows connected
    assert [e.entry_id for e in hass.config_entries.async_entries(DOMAIN)] == [mira.entry_id]
    assert hass.config_entries.flow.async_progress() == []


async def test_the_tokens_are_at_mira_when_the_window_closes(
    hass, mira, aioclient_mock, hass_client, hass_client_no_auth, monkeypatch
):
    """The redirect's page closes the window and the Connections page reads
    the state on getting the focus back: the tokens are at Mira by then,
    before the flow moves on."""
    url = await _start(await hass_client())
    state = parse_qs(urlsplit(url).query)["state"][0]
    aioclient_mock.post(LOGIN["token_url"], json={"access_token": "at-1", "refresh_token": "rt-1", "expires_in": 7200})
    aioclient_mock.put(f"{AGENT_URL}/v1/oauth/x/token", json={"connected": True})

    async def later(self):
        pass

    monkeypatch.setattr(oauth.ConnectSteps, "_continue", later)
    resp = await (await hass_client_no_auth()).get(f"/auth/external/callback?code=c-1&state={state}")
    assert resp.status == 200
    assert ("PUT", f"{AGENT_URL}/v1/oauth/x/token") in [(m, str(u)) for m, u, _, _ in aioclient_mock.mock_calls]


# CO-11 Refusing at the service stores nothing
async def test_co11_refusing_at_the_service_stores_nothing(
    hass, mira, aioclient_mock, hass_client, hass_client_no_auth
):
    # Given a login started for a skill
    url = await _start(await hass_client())
    state = parse_qs(urlsplit(url).query)["state"][0]
    # When the service redirects with an error
    await (await hass_client_no_auth()).get(f"/auth/external/callback?error=access_denied&state={state}")
    await hass.async_block_till_done()
    # Then nothing is stored and the page shows not connected
    assert not [u for m, u, _, _ in aioclient_mock.mock_calls if m in ("POST", "PUT") and "oauth" in str(u)]
    assert hass.config_entries.flow.async_progress() == []


# CO-14 An older integration or service: here, a Mira service without /v1/oauth
async def test_co14_an_older_service_or_home_assistant(hass, mira, aioclient_mock, hass_client, monkeypatch):
    client = await hass_client()
    assert (await (await client.get("/api/mira/oauth")).json()) == {"supported": True}
    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/oauth/x", status=404)
    resp = await client.post("/api/mira/oauth/x/start", headers=FRONTEND)
    assert resp.status == 404
    # Home Assistant older than 2026.11: the page says so, Connect does nothing.
    monkeypatch.setattr(oauth, "SUPPORTED", False)
    assert (await (await client.get("/api/mira/oauth")).json()) == {"supported": False}
    assert (await client.post("/api/mira/oauth/x/start", headers=FRONTEND)).status == 422


async def test_only_admins_connect(hass, mira, hass_admin_user, hass_client):
    hass_admin_user.groups = []
    client = await hass_client()
    assert (await client.get("/api/mira/oauth")).status == 401
    assert (await client.post("/api/mira/oauth/x/start", headers=FRONTEND)).status == 401
