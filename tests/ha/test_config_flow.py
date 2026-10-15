"""Config flow: the connection test decides between entry and error."""

from homeassistant import config_entries
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.data_entry_flow import FlowResultType

from custom_components.lares.const import CONF_TIMEOUT, DOMAIN

from .conftest import AGENT_URL


async def _start(hass):
    return await hass.config_entries.flow.async_init(DOMAIN, context={"source": config_entries.SOURCE_USER})


async def test_valid_agent_creates_entry(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/health", json={"status": "ok"})
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_URL: AGENT_URL + "/", CONF_TOKEN: "secret"}
    )
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert result["data"] == {CONF_URL: AGENT_URL, CONF_TOKEN: "secret"}
    assert result["result"].unique_id is None  # this agent sends no id
    assert aioclient_mock.mock_calls[0][3]["Authorization"] == "Bearer secret"


async def test_wrong_token(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/health", status=401)
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_URL: AGENT_URL, CONF_TOKEN: "wrong"}
    )
    assert result["type"] is FlowResultType.FORM
    assert result["errors"] == {"base": "invalid_auth"}


async def test_unreachable_agent(hass, aioclient_mock):
    aioclient_mock.get(f"{AGENT_URL}/v1/health", exc=TimeoutError())
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_URL: AGENT_URL, CONF_TOKEN: "secret"}
    )
    assert result["errors"] == {"base": "cannot_connect"}


async def test_same_agent_only_once(hass, aioclient_mock, entry):
    result = await _start(hass)
    result = await hass.config_entries.flow.async_configure(
        result["flow_id"], {CONF_URL: AGENT_URL, CONF_TOKEN: "secret"}
    )
    assert result["type"] is FlowResultType.ABORT
    assert result["reason"] == "already_configured"


async def test_options_set_timeout(hass, entry):
    result = await hass.config_entries.options.async_init(entry.entry_id)
    result = await hass.config_entries.options.async_configure(result["flow_id"], {CONF_TIMEOUT: 30})
    assert result["type"] is FlowResultType.CREATE_ENTRY
    assert entry.options[CONF_TIMEOUT] == 30
