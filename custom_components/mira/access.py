"""What the agent needs from Home Assistant, set up by the integration
(docs/spec/setup.md, docs/spec/hacs-default.md): a long-lived token of Home
Assistant's owner, given with an admin's consent in the setup, and Home
Assistant's MCP server with the Assist API. Then nobody has to make a token
by hand, or know the MCP server integration."""

from __future__ import annotations

import logging
from datetime import timedelta

from homeassistant.auth.models import TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN, User
from homeassistant.config_entries import SOURCE_USER, ConfigEntry
from homeassistant.core import HomeAssistant
from homeassistant.helpers.network import NoURLAvailableError, get_url
from homeassistant.helpers.storage import Store

from .const import CONF_AGENT_TOKEN

_LOGGER = logging.getLogger(__name__)

# The owner's token for Mira, by this name in the owner's profile. Tokens of
# the user "Mira" that installs before 0.18 made are named OLD_TOKEN_NAME.
TOKEN_NAME = "Mira (Integration)"
OLD_TOKEN_NAME = "Mira agent"
MCP_DOMAIN = "mcp_server"
TOKEN_LIFETIME = timedelta(days=3650)
# Home Assistant's own conversation agent.
HA_AGENT = "conversation.home_assistant"
# The tokens of agents this Home Assistant removed, by agent id: it pairs
# with them again without a code (docs/spec/setup.md), by the key of
# the token after the agent let go, or with the token itself if the agent
# was away and never did. The store outlives the entry, which removing
# deletes.
REJOIN_STORE = "mira.rejoin"


async def owner(hass: HomeAssistant) -> User:
    """Home Assistant's owner: the admin who set it up, exactly one."""
    return next(u for u in await hass.auth.async_get_users() if u.is_owner)


async def make_owner_token(hass: HomeAssistant) -> tuple[str, str]:
    """A fresh long-lived token of the owner for Mira; an older one of Mira
    goes. Returns (refresh token id, access token)."""
    user = await owner(hass)
    for token in list(user.refresh_tokens.values()):
        if token.client_name == TOKEN_NAME:
            hass.auth.async_remove_refresh_token(token)
    refresh = await hass.auth.async_create_refresh_token(
        user,
        client_name=TOKEN_NAME,
        token_type=TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN,
        access_token_expiration=TOKEN_LIFETIME,
    )
    return refresh.id, hass.auth.async_create_access_token(refresh)


def token_state(hass: HomeAssistant, refresh_token_id: str | None) -> str:
    """What became of the token the integration made: "owner" (as it should
    be), "old" (the user "Mira"'s, before 0.18), "gone" (revoked), or ""
    (none made here: an agent with Home Assistant's token in its own file)."""
    if not refresh_token_id:
        return ""
    token = hass.auth.async_get_refresh_token(refresh_token_id)
    if token is None:
        return "gone"
    return "owner" if token.user.is_owner else "old"


async def renew_access(hass: HomeAssistant, entry: ConfigEntry, client) -> None:
    """A new owner's token for the agent, sent to it (it starts anew); the
    one before goes. Raises MiraError when the agent refuses it: then the new
    token is withdrawn again and the old one stays."""
    before = entry.data.get(CONF_AGENT_TOKEN)
    refresh_id, token = await make_owner_token(hass)
    try:
        await client.setup(ha_url(hass), token, None)
    except Exception:
        revoke_agent_token(hass, refresh_id)
        raise
    hass.config_entries.async_update_entry(entry, data={**entry.data, CONF_AGENT_TOKEN: refresh_id})
    if before != refresh_id:
        revoke_agent_token(hass, before)


def revoke_agent_token(hass: HomeAssistant, refresh_token_id: str | None) -> None:
    """Withdraw the token Mira had."""
    if not refresh_token_id:
        return
    token = hass.auth.async_get_refresh_token(refresh_token_id)
    if token is not None:
        hass.auth.async_remove_refresh_token(token)


async def ensure_mcp_server(hass: HomeAssistant) -> None:
    """Home Assistant's MCP server with the Assist API, set up if missing."""
    if hass.config_entries.async_entries(MCP_DOMAIN):
        return
    result = await hass.config_entries.flow.async_init(MCP_DOMAIN, context={"source": SOURCE_USER})
    if result.get("type") == "form":
        result = await hass.config_entries.flow.async_configure(result["flow_id"], {"llm_hass_api": ["assist"]})
    if result.get("type") != "create_entry":
        raise RuntimeError(f"setting up Home Assistant's MCP server ended with {result.get('type')}")
    _LOGGER.info("Set up Home Assistant's MCP server for the Mira agent")


def ha_url(hass: HomeAssistant) -> str:
    """The address the agent reaches Home Assistant at: the internal one.
    Never the external one: the agent's token would go out to it."""
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
    assistant changed, the agent it had before."""
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
    """Assistants that still use Mira get the agent they had before, or
    Home Assistant's own when that one is gone, so none points at an agent
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
    Assistant is the one the agent was paired with."""
    store = _rejoin_store(hass)
    tokens = await store.async_load() or {}
    await store.async_save({**tokens, agent_id: token})


async def rejoin_token(hass: HomeAssistant, agent_id: str) -> str | None:
    """The old token of an agent this Home Assistant let go, if any."""
    return (await _rejoin_store(hass).async_load() or {}).get(agent_id)


async def forget_rejoin(hass: HomeAssistant, agent_id: str) -> None:
    store = _rejoin_store(hass)
    tokens = await store.async_load() or {}
    if tokens.pop(agent_id, None) is not None:
        await store.async_save(tokens)
