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


async def ensure_agent_access(hass: HomeAssistant, user_id: str | None = None) -> tuple[str, str, str]:
    """Mira's own user (made once, kept by id) and a fresh long-lived token
    for it; an older token of the agent goes. Returns (user id, refresh
    token id, access token)."""
    user = await hass.auth.async_get_user(user_id) if user_id else None
    if user is None:
        for candidate in await hass.auth.async_get_users():
            if candidate.name == USER_NAME and not candidate.system_generated and not candidate.is_admin:
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
    """The address the agent reaches Home Assistant at: the internal one."""
    try:
        return get_url(hass, allow_external=False, allow_cloud=False, prefer_external=False)
    except NoURLAvailableError:
        return get_url(hass, allow_cloud=False)
