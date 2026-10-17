"""Config flow: the connection test decides between entry and error."""

from homeassistant import config_entries
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.data_entry_flow import FlowResultType

from unittest.mock import AsyncMock, patch

from pytest_homeassistant_custom_component.common import MockConfigEntry

from custom_components.mira.const import (
    CONF_AGENT_TOKEN,
    CONF_AGENT_USER,
    CONF_IMPORTED,
    CONF_PIPELINES,
    CONF_TIMEOUT,
    DOMAIN,
)

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
    assert result["data"] == {CONF_URL: AGENT_URL, CONF_TOKEN: "secret", CONF_PIPELINES: []}
    assert aioclient_mock.mock_calls[0][3]["Authorization"] == "Bearer secret"


async def test_wrong_token(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairs": False})
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
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairs": True, "open": True})
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"


def _new_agent(aioclient_mock, models=("qwen3", "gemma3"), model=""):
    aioclient_mock.get(
        f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairs": True, "open": True, "setup_mode": True, "model": model}
    )
    aioclient_mock.post(f"{AGENT_URL}/v1/setup/models", json={"models": list(models)})
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok", "id": AGENT_ID})
    aioclient_mock.get(f"{AGENT_URL}/v1/status", json={"pending_messages": 0, "quiet_mode": False})
    aioclient_mock.put(f"{AGENT_URL}/v1/ui/ha", json={"managed": True})


def _calls(aioclient_mock, path):
    return [c[2] for c in aioclient_mock.mock_calls if c[0] == "POST" and str(c[1]).endswith(path)]


async def _to(hass, result, step):
    """A branch of the overview."""
    assert result["type"] is FlowResultType.MENU and result["step_id"] == "summary", result
    return await hass.config_entries.flow.async_configure(result["flow_id"], {"next_step_id": step})


async def _finish(hass, result):
    """The summary's "Pair"."""
    assert result["type"] is FlowResultType.MENU and result["step_id"] == "summary", result
    with patch("custom_components.mira.config_flow.ensure_mcp_server", AsyncMock()) as mcp:
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {"next_step_id": "finish"})
    return result, mcp


async def test_new_agent_is_paired_and_set_up_only_at_the_end(hass, aioclient_mock, hass_owner_user):
    """A new agent: the AI model from a server of one's own, tried at once;
    nothing is paired and no HA user made before the summary's "Pair",
    which then pairs and sets up in one call."""
    _new_agent(aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/setup/probe", json={"status": "ok"})
    aioclient_mock.post(f"{AGENT_URL}/v1/pair", json={"token": "agent-token"})
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    assert result["step_id"] == "summary"
    # "Start using Mira" only once nothing is missing.
    assert "finish" not in result["menu_options"]
    result = await _to(hass, result, "model_source")
    assert result["step_id"] == "model_source"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"source": "custom"})
    assert result["step_id"] == "model_server"
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {"base_url": "http://192.168.1.30:11434/v1/"}
    )
    assert result["step_id"] == "model"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"model": "qwen3"})
    assert result["step_id"] == "summary"
    assert "http://192.168.1.30:11434/v1, qwen3" in result["description_placeholders"]["status"]
    llm = {"kind": "custom", "base_url": "http://192.168.1.30:11434/v1", "model": "qwen3", "api_key": ""}
    assert _calls(aioclient_mock, "/v1/setup/probe") == [{"llm": llm}]
    assert not _calls(aioclient_mock, "/v1/pair")
    assert not [u for u in await hass.auth.async_get_users() if u.name == "Mira"]

    result, mcp = await _finish(hass, result)
    assert result["type"] is FlowResultType.CREATE_ENTRY, result
    mcp.assert_awaited_once()
    data = result["data"]
    assert data[CONF_TOKEN] == "agent-token" and data[CONF_IMPORTED]
    user = await hass.auth.async_get_user(data[CONF_AGENT_USER])
    assert user.name == "Mira" and not user.is_admin
    (pair,) = _calls(aioclient_mock, "/v1/pair")
    assert pair["code"] == "" and pair["llm"] == llm
    refresh = hass.auth.async_validate_access_token(pair["ha_token"])
    assert refresh is not None and refresh.user.id == user.id and refresh.id == data[CONF_AGENT_TOKEN]


async def test_provider_asks_only_for_the_key_and_says_what_failed(hass, aioclient_mock, hass_owner_user):
    """A cloud provider has its address built in; a model without tools is
    said plainly in the form, right where it can be changed."""
    _new_agent(aioclient_mock)
    aioclient_mock.post(f"{AGENT_URL}/v1/setup/probe", status=422, json={"error": "gemma3", "reason": "no_tools"})
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    result = await _to(hass, result, "model_source")
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"source": "openai"})
    assert result["step_id"] == "model_key"
    # A wrong click, the dialog closed and opened again: back at the overview.
    result = await hass.config_entries.flow.async_configure(result["flow_id"])
    assert result["step_id"] == "summary"
    result = await _to(hass, result, "model_source")
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"source": "gemini"})
    assert result["step_id"] == "model_key"
    assert result["description_placeholders"]["provider"] == "Google Gemini"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"api_key": " key "})
    assert result["step_id"] == "model"
    assert _calls(aioclient_mock, "/v1/setup/models") == [
        {"base_url": "https://generativelanguage.googleapis.com/v1beta/openai", "api_key": "key"}
    ]
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"model": "gemma3"})
    assert result["step_id"] == "model" and result["errors"] == {"base": "model_no_tools"}


async def test_model_from_home_assistant_needs_no_key(hass, aioclient_mock, hass_owner_user):
    """An AI integration Home Assistant has: its key goes to the agent
    without being asked for, and its model is ticked."""
    ai = MockConfigEntry(
        domain="google_generative_ai_conversation",
        title="Google Generative AI",
        data={"api_key": "ha-key"},
        state=config_entries.ConfigEntryState.LOADED,
        subentries_data=[
            {"subentry_type": "conversation", "data": {"chat_model": "models/gemini-flash-latest"}, "title": "Gemini", "unique_id": None}
        ],
    )
    ai.add_to_hass(hass)
    _new_agent(aioclient_mock, models=("gemini-flash-latest", "gemini-pro-latest"))
    aioclient_mock.post(f"{AGENT_URL}/v1/setup/probe", json={"status": "ok"})
    aioclient_mock.post(f"{AGENT_URL}/v1/pair", json={"token": "agent-token"})
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    result = await _to(hass, result, "model_source")
    source = f"ha:{ai.entry_id}"
    options = result["data_schema"].schema["source"].config["options"]
    assert options[0]["value"] == source and "gemini-flash-latest" in options[0]["label"]
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"source": source})
    assert result["step_id"] == "model"
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"model": "gemini-flash-latest"})
    result, _ = await _finish(hass, result)
    assert result["type"] is FlowResultType.CREATE_ENTRY, result
    (pair,) = _calls(aioclient_mock, "/v1/pair")
    assert pair["llm"]["api_key"] == "ha-key" and pair["llm"]["kind"] == "cloud"
    # A real Gemini entry is not loaded in tests; nothing to unload.
    ai.mock_state(hass, config_entries.ConfigEntryState.NOT_LOADED)


async def test_agent_paired_before_takes_a_code_at_the_end(hass, aioclient_mock, hass_owner_user):
    """An agent paired before keeps its model; the code is used when
    pairing, and a wrong one sends back to the summary, which offers to
    enter it again. The token made for it is withdrawn again."""
    aioclient_mock.get(f"{AGENT_URL}/v1/pair", json={"id": AGENT_ID, "pairs": True, "open": False, "model": "qwen3"})
    aioclient_mock.post(f"{AGENT_URL}/v1/pair", status=403)
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {CONF_URL: AGENT_URL})
    assert result["step_id"] == "summary" and "pair" in result["menu_options"]
    assert "qwen3" in result["description_placeholders"]["status"]
    assert "finish" not in result["menu_options"]
    result = await _to(hass, result, "pair")
    result = await hass.config_entries.flow.async_configure(result["flow_id"], {"code": "000000"})
    assert result["step_id"] == "summary"
    result, _ = await _finish(hass, result)
    assert result["step_id"] == "summary" and "pair" in result["menu_options"]
    assert result["description_placeholders"]["error"]
    user = next(u for u in await hass.auth.async_get_users() if u.name == "Mira")
    assert not [t for t in user.refresh_tokens.values() if t.client_name == "Mira agent"]


async def test_options_set_timeout(hass, entry):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    assert result["type"] is FlowResultType.MENU
    result = await hass.config_entries.options.async_configure(result["flow_id"], {"next_step_id": "general"})
    result = await hass.config_entries.options.async_configure(result["flow_id"], {CONF_TIMEOUT: 30})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert entry.options[CONF_TIMEOUT] == 30
