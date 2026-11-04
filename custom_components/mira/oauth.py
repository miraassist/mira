"""Connections of skills with a login at a service (docs/spec/oauth.md).

Home Assistant does the login: Mira's config flow, started with the source
"connect" from the panel's Connections page, builds the login's address with
Home Assistant's OAuth helper (its fixed redirect over my.home-assistant.io,
the state it signs, PKCE), receives the redirect and exchanges the code. It
hands the tokens to the agent once and ends without a config entry; from then
on the agent keeps and renews them.

Sending the client secret as HTTP Basic, which X wants, needs the helper of
Home Assistant 2026.11 or newer (token_auth_method); before, Connect is not
offered.
"""

from __future__ import annotations

import logging
from typing import Any

from aiohttp import web
from homeassistant.components.http import HomeAssistantView
from homeassistant.config_entries import ConfigEntryState, ConfigFlowResult
from homeassistant.data_entry_flow import FlowResultType, UnknownFlow
from homeassistant.exceptions import Unauthorized
from homeassistant.helpers import config_entry_oauth2_flow
from homeassistant.helpers.http import KEY_HASS

from .api import MiraError, MiraInvalidConfig, MiraNotFound
from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

SOURCE_CONNECT = "connect"
# The helper can send the secret as HTTP Basic (Home Assistant 2026.11).
SUPPORTED = hasattr(config_entry_oauth2_flow.LocalOAuth2Implementation, "token_auth_method")


def implementation(hass, login: dict[str, Any]) -> config_entry_oauth2_flow.LocalOAuth2Implementation:
    """Home Assistant's OAuth implementation for a skill's login."""
    domain = f"{DOMAIN}_{login['skill']}"
    if login.get("pkce"):
        impl = config_entry_oauth2_flow.LocalOAuth2ImplementationWithPkce(
            hass, domain, login["client_id"], login["authorize_url"], login["token_url"], login["client_secret"]
        )
    else:
        impl = config_entry_oauth2_flow.LocalOAuth2Implementation(
            hass, domain, login["client_id"], login["client_secret"], login["authorize_url"], login["token_url"]
        )
    impl.token_auth_method = "client_secret_basic" if login.get("token_auth") == "basic" else "client_secret_post"
    return impl


class ConnectSteps:
    """The config flow's steps of a connection; the auth and creation steps
    are Home Assistant's (AbstractOAuth2FlowHandler)."""

    _login: dict[str, Any] | None = None
    _done: ConfigFlowResult | None = None

    @property
    def logger(self) -> logging.Logger:
        return _LOGGER

    @property
    def extra_authorize_data(self) -> dict:
        login = self._login or {}
        data = dict(login.get("authorize_params") or {})
        if scopes := login.get("scopes"):
            data["scope"] = " ".join(scopes)
        return data

    async def async_step_connect(self, data: dict[str, Any]) -> ConfigFlowResult:
        """Started by the Connections page with the skill's login."""
        self._login = data["login"]
        self.flow_impl = implementation(self.hass, self._login)
        return await self.async_step_auth()

    async def async_step_auth(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Home Assistant's step. On the redirect the code is exchanged and
        the tokens go to the agent before the redirect's page answers: that
        page closes the window, and the Connections page reads the state
        when it gets the focus back. Then the flow moves on by itself, since
        no dialog of the frontend does it (checked: a flow at external_done
        waits for the next configure)."""
        result = await super().async_step_auth(user_input)
        if result["type"] is FlowResultType.EXTERNAL_STEP_DONE:
            if "error" not in (user_input or {}):
                self._done = await super().async_step_creation()
            self.hass.async_create_task(self._continue(), eager_start=False)
        return result

    async def async_step_creation(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The outcome of the exchange already done on the redirect."""
        if self._done is not None:
            return self._done
        return await super().async_step_creation(user_input)

    async def _continue(self) -> None:
        try:
            await self.hass.config_entries.flow.async_configure(self.flow_id)
        except UnknownFlow:
            pass  # someone else moved it on

    async def async_oauth_create_entry(self, data: dict) -> ConfigFlowResult:
        """The tokens go to the agent; Home Assistant keeps nothing."""
        skill = (self._login or {}).get("skill", "")
        entry = _loaded(self.hass)
        if entry is None:
            _LOGGER.error("Mira is not set up; the connection of %s is not kept", skill)
            return self.async_abort(reason="connect_failed")
        try:
            await entry.runtime_data.client.put_oauth_token(skill, data["token"])
        except MiraError as err:
            _LOGGER.error("Mira did not take the connection of %s: %s", skill, err)
            return self.async_abort(reason="connect_failed")
        _LOGGER.info("Connected %s", skill)
        return self.async_abort(reason="connected")


class MiraOAuthView(HomeAssistantView):
    """For the Connections page: whether Connect works here. An older
    integration has no such view (404). Admins only."""

    url = "/api/mira/oauth"
    name = "api:mira:oauth"
    requires_auth = True

    async def get(self, request: web.Request) -> web.Response:
        if not request["hass_user"].is_admin:
            raise Unauthorized()
        return web.json_response({"supported": SUPPORTED})


class MiraOAuthStartView(HomeAssistantView):
    """Start a skill's login: the address of the service's login page, for
    the window the page opened. Admins only."""

    url = "/api/mira/oauth/{skill}/start"
    name = "api:mira:oauth:start"
    requires_auth = True

    async def post(self, request: web.Request, skill: str) -> web.Response:
        hass = request.app[KEY_HASS]
        if not request["hass_user"].is_admin:
            raise Unauthorized()
        if not SUPPORTED:
            return web.json_response({"error": "Home Assistant 2026.11 or newer is needed"}, status=422)
        entry = _loaded(hass)
        if entry is None:
            return web.json_response({"error": "Mira is not set up"}, status=503)
        try:
            login = await entry.runtime_data.client.oauth_login(skill)
        except MiraNotFound as err:
            return web.json_response({"error": str(err)}, status=404)
        except MiraInvalidConfig as err:
            return web.json_response({"error": str(err)}, status=422)
        except MiraError as err:
            return web.json_response({"error": f"Mira agent not reachable: {err}"}, status=502)
        result = await hass.config_entries.flow.async_init(
            DOMAIN, context={"source": SOURCE_CONNECT}, data={"login": login}
        )
        if result["type"] is not FlowResultType.EXTERNAL_STEP:
            return web.json_response({"error": result.get("reason", "the login did not start")}, status=422)
        return web.json_response({"url": result["url"]})


def _loaded(hass):
    for entry in hass.config_entries.async_entries(DOMAIN):
        if entry.state is ConfigEntryState.LOADED:
            return entry
    return None
