"""What the service needs from Home Assistant, set up by the integration
(docs/spec/setup.md, docs/spec/hacs-default.md): a long-lived token of an
admin that the admin pastes at the setup, and Home Assistant's MCP server
with the Assist API. The integration never makes a token itself."""

from __future__ import annotations

import logging

import voluptuous as vol
from homeassistant.auth.models import TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN, RefreshToken
from homeassistant.config_entries import SOURCE_USER, ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers import selector
from homeassistant.helpers.network import NoURLAvailableError, get_url
from homeassistant.helpers.storage import Store

from .const import CONF_AGENT_TOKEN, CONF_TOKEN_GIVEN

_LOGGER = logging.getLogger(__name__)

# The owner's token the integration made itself before 0.23, by this name in
# the owner's profile: revoked at the first start after the update
# (docs/spec/hacs-default.md, 4).
MADE_TOKEN_NAME = "Mira (Integration)"
MCP_DOMAIN = "mcp_server"
# The token field of the setup and of the repair, hidden as a password.
HA_TOKEN_SCHEMA = vol.Schema(
    {vol.Required("token"): selector.TextSelector(selector.TextSelectorConfig(type=selector.TextSelectorType.PASSWORD))}
)
# Home Assistant's own conversation agent.
HA_AGENT = "conversation.home_assistant"
# The tokens of agents this Home Assistant removed, by agent id: it pairs
# with them again without a code (docs/spec/setup.md), by the key of
# the token after the service let go, or with the token itself if the service
# was away and never did. The store outlives the entry, which removing
# deletes.
REJOIN_STORE = "mira.rejoin"


def check_token(hass: HomeAssistant, token: str) -> tuple[RefreshToken | None, str]:
    """A token an admin pasted: its refresh token, or why it cannot be
    Mira's (the form's error key). Looked up in Home Assistant's memory."""
    refresh = hass.auth.async_validate_access_token(token.strip())
    if refresh is None:
        return None, "invalid_token"
    if refresh.token_type != TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN:
        return None, "token_not_long_lived"
    if not refresh.user.is_admin:
        return None, "token_not_admin"
    return refresh, ""


def token_works(hass: HomeAssistant, refresh_token_id: str) -> bool:
    """Whether the token Mira was given still works: it exists, its user is
    active and an admin."""
    refresh = hass.auth.async_get_refresh_token(refresh_token_id)
    return refresh is not None and refresh.user.is_active and refresh.user.is_admin


def revoke_made_token(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    """The owner's token the integration made itself before 0.23 goes: no
    token made by the integration stays. True when one was revoked."""
    refresh_id = entry.data.get(CONF_AGENT_TOKEN)
    if not refresh_id or entry.data.get(CONF_TOKEN_GIVEN):
        return False
    refresh = hass.auth.async_get_refresh_token(refresh_id)
    if refresh is None or refresh.client_name != MADE_TOKEN_NAME:
        return False
    hass.auth.async_remove_refresh_token(refresh)
    _LOGGER.info(
        "Revoked the token %r the integration made for Mira before; an admin gives Mira a token under Repairs",
        MADE_TOKEN_NAME,
    )
    return True


async def give_token(hass: HomeAssistant, entry: ConfigEntry, client, token: str, refresh: RefreshToken) -> None:
    """The token an admin gave, sent to the service (it starts anew) and its
    id kept. Raises MiraError when the service refuses it."""
    await client.setup(ha_url(hass), token.strip(), None)
    hass.config_entries.async_update_entry(
        entry, data={**entry.data, CONF_AGENT_TOKEN: refresh.id, CONF_TOKEN_GIVEN: True}
    )


async def ensure_mcp_server(hass: HomeAssistant) -> None:
    """Home Assistant's MCP server with the Assist API, set up if missing."""
    if hass.config_entries.async_entries(MCP_DOMAIN):
        return
    result = await hass.config_entries.flow.async_init(MCP_DOMAIN, context={"source": SOURCE_USER})
    if result.get("type") == "form":
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {"llm_hass_api": ["assist"]})
    if result.get("type") != "create_entry":
        raise RuntimeError(f"setting up Home Assistant's MCP server ended with {result.get('type')}")
    _LOGGER.info("Set up Home Assistant's MCP server for the Mira service")


def ha_url(hass: HomeAssistant) -> str:
    """The address the service reaches Home Assistant at: the internal one.
    Never the external one: the service's token would go out to it."""
    try:
        return get_url(hass, allow_external=False, allow_cloud=False, prefer_external=False)
    except NoURLAvailableError as err:
        raise RuntimeError(
            "Home Assistant has no internal address; set one under Settings → System → Network (Home Assistant URL, Local network)"
        ) from err


def assistants(hass: HomeAssistant) -> dict[str, tuple[str, bool]]:
    """Home Assistant's assistants (Assist pipelines): id -> (name, preferred)."""
    if "assist_pipeline" not in hass.config.components:
        return {}
    from homeassistant.components.assist_pipeline import async_get_pipeline, async_get_pipelines

    preferred = async_get_pipeline(hass).id
    return {p.id: (p.name, p.id == preferred) for p in async_get_pipelines(hass)}


async def use_mira_in(hass: HomeAssistant, pipeline_ids: list[str], entity_id: str) -> dict[str, str]:
    """Make Mira the conversation agent of these assistants; returns, per
    assistant changed, the service it had before."""
    if not pipeline_ids or "assist_pipeline" not in hass.config.components:
        return {}
    from homeassistant.components.assist_pipeline import async_get_pipelines, async_update_pipeline

    before = {}
    for pipeline in async_get_pipelines(hass):
        if pipeline.id in pipeline_ids and pipeline.conversation_engine != entity_id:
            before[pipeline.id] = pipeline.conversation_engine
            await async_update_pipeline(hass, pipeline, conversation_engine=entity_id)
            _LOGGER.info("Mira is now the conversation agent of the assistant %s", pipeline.name)
    return before


async def hand_back(hass: HomeAssistant, entity_id: str | None, before: dict[str, str]) -> None:
    """Assistants that still use Mira get the service they had before, or
    Home Assistant's own when that one is gone, so none points at a service
    that no longer exists."""
    if not entity_id or "assist_pipeline" not in hass.config.components:
        return
    from homeassistant.components.assist_pipeline import async_get_pipelines, async_update_pipeline

    for pipeline in async_get_pipelines(hass):
        if pipeline.conversation_engine != entity_id:
            continue
        agent = before.get(pipeline.id) or HA_AGENT
        if agent != HA_AGENT and hass.states.get(agent) is None:
            agent = HA_AGENT
        await async_update_pipeline(hass, pipeline, conversation_engine=agent)
        _LOGGER.info("The assistant %s uses %s again", pipeline.name, agent)


def _rejoin_store(hass: HomeAssistant) -> Store[dict[str, str]]:
    return Store(hass, 1, REJOIN_STORE, private=True)


async def keep_rejoin(hass: HomeAssistant, agent_id: str, token: str) -> None:
    """The entry is removed: its token proves later that this Home
    Assistant is the one the service was paired with."""
    store = _rejoin_store(hass)
    tokens = await store.async_load() or {}
    await store.async_save({**tokens, agent_id: token})


async def rejoin_token(hass: HomeAssistant, agent_id: str) -> str | None:
    """The old token of a service this Home Assistant let go, if any."""
    return (await _rejoin_store(hass).async_load() or {}).get(agent_id)


async def forget_rejoin(hass: HomeAssistant, agent_id: str) -> None:
    store = _rejoin_store(hass)
    tokens = await store.async_load() or {}
    if tokens.pop(agent_id, None) is not None:
        await store.async_save(tokens)
