"""What the agent needs from Home Assistant, set up by the integration
(docs/spec/einrichtung.md): a user of its own without admin rights, with a
long-lived token, and Home Assistant's MCP server with the Assist API. Then
nobody has to log in as another user for a token, or know the MCP server
integration."""

from __future__ import annotations

from datetime import timedelta
import logging

from homeassistant.auth.const import GROUP_ID_USER
from homeassistant.auth.models import TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN
from homeassistant.config_entries import SOURCE_USER
from homeassistant.core import HomeAssistant
from homeassistant.helpers.network import NoURLAvailableError, get_url

_LOGGER = logging.getLogger(__name__)

USER_NAME = "Mira"
TOKEN_NAME = "Mira agent"
MCP_DOMAIN = "mcp_server"
TOKEN_LIFETIME = timedelta(days=3650)
# Home Assistant's own conversation agent.
HA_AGENT = "conversation.home_assistant"


async def ensure_agent_access(hass: HomeAssistant, user_id: str | None = None) -> tuple[str, str, str]:
    """Mira's own user (made once, kept by id) and a fresh long-lived token
    for it; an older token of the agent goes. Returns (user id, refresh
    token id, access token)."""
    user = await hass.auth.async_get_user(user_id) if user_id else None
    if user is None:
        # An earlier Mira user, after the integration was set up anew; never
        # one that belongs to a person in the house, who may be called Mira.
        people = _person_users(hass)
        for candidate in await hass.auth.async_get_users():
            if (
                candidate.name == USER_NAME
                and not candidate.system_generated
                and not candidate.is_admin
                and candidate.id not in people
            ):
                user = candidate
                break
    if user is None:
        user = await hass.auth.async_create_user(USER_NAME, group_ids=[GROUP_ID_USER])
        _LOGGER.info("Created the Home Assistant user %s for the Mira agent (no admin rights)", USER_NAME)
    if user.is_admin:
        # The first user of an empty Home Assistant becomes its owner; the
        # agent must never run with admin rights.
        raise RuntimeError("Mira's Home Assistant user would have admin rights; set Home Assistant up first")
    for token in list(user.refresh_tokens.values()):
        if token.client_name == TOKEN_NAME:
            hass.auth.async_remove_refresh_token(token)
    refresh = await hass.auth.async_create_refresh_token(
        user, client_name=TOKEN_NAME, token_type=TOKEN_TYPE_LONG_LIVED_ACCESS_TOKEN, access_token_expiration=TOKEN_LIFETIME
    )
    return user.id, refresh.id, hass.auth.async_create_access_token(refresh)


def _person_users(hass: HomeAssistant) -> set[str]:
    """The users linked to a person in Home Assistant: people's own accounts."""
    return {uid for state in hass.states.async_all("person") if (uid := state.attributes.get("user_id"))}


def revoke_agent_token(hass: HomeAssistant, refresh_token_id: str | None) -> None:
    """Withdraw the agent's token; the user stays, Mira's tasks hang on it."""
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
    from homeassistant.components.assist_pipeline import async_get_pipeline, async_get_pipelines  # noqa: PLC0415

    preferred = async_get_pipeline(hass).id
    return {p.id: (p.name, p.id == preferred) for p in async_get_pipelines(hass)}


async def use_mira_in(hass: HomeAssistant, pipeline_ids: list[str], entity_id: str) -> dict[str, str]:
    """Make Mira the conversation agent of these assistants; returns, per
    assistant changed, the agent it had before."""
    if not pipeline_ids or "assist_pipeline" not in hass.config.components:
        return {}
    from homeassistant.components.assist_pipeline import async_get_pipelines, async_update_pipeline  # noqa: PLC0415

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
    from homeassistant.components.assist_pipeline import async_get_pipelines, async_update_pipeline  # noqa: PLC0415

    for pipeline in async_get_pipelines(hass):
        if pipeline.conversation_engine != entity_id:
            continue
        agent = before.get(pipeline.id) or HA_AGENT
        if agent != HA_AGENT and hass.states.get(agent) is None:
            agent = HA_AGENT
        await async_update_pipeline(hass, pipeline, conversation_engine=agent)
        _LOGGER.info("The assistant %s uses %s again", pipeline.name, agent)
