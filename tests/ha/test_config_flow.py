"""Config flow: the connection test decides between entry and error."""

from homeassistant import config_entries
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.data_entry_flow import FlowResultType

from unittest.mock import AsyncMock, patch

from custom_components.mira.const import CONF_AGENT_TOKEN, CONF_AGENT_USER, CONF_IMPORTED, CONF_TIMEOUT, DOMAIN

from .conftest import AGENT_ID, AGENT_URL


async def _start(hass):
    return await hass.config_entries.flow.async_init(DOMAIN, context={"source": config_entries.SOURCE_USER})


async def test_agent_with_token_in_its_config(hass, aioclient_mock):
    """An agent whose token is in its mira.yaml (or one from before
    pairing): the address, then the token."""
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", status=404)
    aioclient_mock.get(f"{AGENT_URL}/v1/health", status=401)
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL + "/"})
    assert result["step_id"] == "token"
    aioclient_mock.clear_requests()
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok"})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_TOKEN: "secret"})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["data"] == {CONF_URL: AGENT_URL, CONF_TOKEN: "secret"}
    assert aioclient_mock.mock_calls[0][3]["Authorization"] == "Bearer secret"


async def test_wrong_token(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairing": False})
    aioclient_mock.get(f"{AGENT_URL}/v1/health", status=401)
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_TOKEN: "wrong"})
    assert result["errors"] == {"base": "invalid_auth"}


async def test_unreachable_agent(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", exc=TimeoutError())
    aioclient_mock.get(f"{AGENT_URL}/v1/health", exc=TimeoutError())
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    assert result["errors"] == {"base": "cannot_connect"}


async def test_same_agent_only_once(hass, aioclient_mock, entry):
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairing": True})
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"


async def test_new_agent_pairs_and_is_set_up(hass, aioclient_mock, hass_owner_user):
    """A new agent: the code, the language model, and the integration sets
    up Home Assistant for it: its own user without admin rights, a token,
    and the agent gets both."""
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairing": True, "setup_mode": True})
    aioclient_mock.post(f"{AGENT_URL}/v1/pair", status=403)
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    assert result["step_id"] == "pair"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"code": "000000"})
    assert result["errors"] == {"base": "bad_code"}

    aioclient_mock.clear_requests()
    aioclient_mock.post(f"{AGENT_URL}/v1/pair", json={"token": "agent-token"})
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "setup", "id": AGENT_ID})
    aioclient_mock.post(f"{AGENT_URL}/v1/setup/models", json={"models": ["qwen3", "gemma3"]})
    aioclient_mock.post(f"{AGENT_URL}/v1/setup", json={"status": "restarting"})
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False})
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"code": "123 456"})
    assert result["step_id"] == "model_kind"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"kind": "ollama"})
    assert result["step_id"] == "model_server"
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {"base_url": "http://localhost:11434/v1", "api_key": ""}
    )
    assert result["step_id"] == "model"
    with (
        patch("custom_components.mira.config_flow.ensure_mcp_server", AsyncMock()) as mcp,
        patch("custom_components.mira.config_flow.MiraConfigFlow._wait_ready", AsyncMock()),
    ):
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {"model": "qwen3"})
    assert result["type"] is FlowResultType.CREATE_ENTRY, result
    mcp.assert_awaited_once()
    data = result["data"]
    assert data[CONF_TOKEN] == "agent-token" and data[CONF_IMPORTED]
    user = await hass.auth.async_get_user(data[CONF_AGENT_USER])
    assert user.name == "Mira" and not user.is_admin
    setup = next(c[2] for c in aioclient_mock.mock_calls if str(c[1]).endswith("/v1/setup"))
    assert setup["llm"] == {"kind": "ollama", "base_url": "http://localhost:11434/v1", "model": "qwen3", "api_key": ""}
    refresh = hass.auth.async_validate_access_token(setup["ha_token"])
    assert refresh is not None and refresh.user.id == user.id and refresh.id == data[CONF_AGENT_TOKEN]


async def test_options_set_timeout(hass, entry):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    assert result["type"] is FlowResultType.MENU
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"next_step_id": "general"})
    result = await hass.config_entries.options.async_configure(result["flow_id"], {CONF_TIMEOUT: 30})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert entry.options[CONF_TIMEOUT] == 30
