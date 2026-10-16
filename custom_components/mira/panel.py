"""Mira's web UI inside Home Assistant: a sidebar panel that shows the
agent's page through Home Assistant's own server.

The page's files come through an open view (they hold no data, and the
agent serves them without a token as well); its API calls come through
an authenticated view that only admins may use and that adds the agent's
token. So the page works over HA's HTTPS and from away, with HA's login.
"""

from __future__ import annotations

import re

import aiohttp
from aiohttp import web

from homeassistant.components import frontend
from homeassistant.components.http import HomeAssistantView
from homeassistant.config_entries import ConfigEntryState
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import Unauthorized
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.http import KEY_HASS

from .const import DOMAIN

PANEL_URL_PATH = "mira"
PANEL_ICON = "mdi:face-woman-shimmer"
PAGE = "/api/mira/panel/ui"
# The agent's page and its files; nothing else goes through the open view.
PAGE_FILES = re.compile(r"ui/?|ui/icon\.png|ui/guide/(de|en)|ui/guide/img/[\w.-]+")
API = "v1/ui/"
# Headers passed on, both ways: videos come in ranges (Safari asks so).
REQUEST_PASSED = ("Range", "If-Range")
PASSED = ("Content-Type", "Cache-Control", "Accept-Ranges", "Content-Range")
TIMEOUT = aiohttp.ClientTimeout(total=60)


@callback
def async_register_views(hass: HomeAssistant) -> None:
    """Register the two views once; they find the loaded entry per request."""
    hass.http.register_view(MiraPanelView())
    hass.http.register_view(MiraProxyView())


@callback
def async_register_panel(hass: HomeAssistant, title: str) -> None:
    """Show Mira in the sidebar, for admins."""
    frontend.async_register_built_in_panel(
        hass,
        "iframe",
        sidebar_title=title,
        sidebar_icon=PANEL_ICON,
        frontend_url_path=PANEL_URL_PATH,
        config={"url": PAGE},
        require_admin=True,
        update=True,
    )


@callback
def async_remove_panel(hass: HomeAssistant) -> None:
    """Take Mira out of the sidebar."""
    frontend.async_remove_panel(hass, PANEL_URL_PATH, warn_if_unknown=False)


def _agent(hass: HomeAssistant) -> tuple[str, str] | None:
    """The loaded entry's agent URL and token."""
    for entry in hass.config_entries.async_entries(DOMAIN):
        if entry.state is ConfigEntryState.LOADED:
            return entry.data[CONF_URL].rstrip("/"), entry.data[CONF_TOKEN]
    return None


async def _forward(hass: HomeAssistant, request: web.Request, path: str, with_token: bool) -> web.Response:
    """Pass the request on to the agent and its answer back."""
    agent = _agent(hass)
    if agent is None:
        return web.json_response({"error": "Mira is not set up"}, status=503)
    base, token = agent
    headers = {"Content-Type": request.headers.get("Content-Type", "application/json")}
    headers.update({k: request.headers[k] for k in REQUEST_PASSED if k in request.headers})
    if with_token:
        headers["Authorization"] = f"Bearer {token}"
    body = await request.read() if request.can_read_body else None
    try:
        async with async_get_clientsession(hass).request(
            request.method,
            f"{base}/{path}",
            params=request.query,
            data=body,
            headers=headers,
            timeout=TIMEOUT,
        ) as resp:
            data = await resp.read()
            passed = {k: resp.headers[k] for k in PASSED if k in resp.headers}
            if with_token:
                passed.setdefault("Content-Type", "application/json")
            return web.Response(status=resp.status, body=data, headers=passed)
    except (aiohttp.ClientError, TimeoutError) as err:
        return web.json_response({"error": f"Mira agent not reachable: {err}"}, status=502)


class MiraPanelView(HomeAssistantView):
    """The agent's page and its files, without login like at the agent."""

    url = "/api/mira/panel/{path:.*}"
    name = "api:mira:panel"
    requires_auth = False

    async def get(self, request: web.Request, path: str) -> web.Response:
        if not PAGE_FILES.fullmatch(path):
            return web.Response(status=404)
        return await _forward(request.app[KEY_HASS], request, path, with_token=False)


class MiraProxyView(HomeAssistantView):
    """The page's API calls, for logged-in admins, with the agent's token."""

    url = "/api/mira/proxy/{path:.*}"
    name = "api:mira:proxy"
    requires_auth = True

    async def _handle(self, request: web.Request, path: str) -> web.Response:
        if not request["hass_user"].is_admin:
            raise Unauthorized()
        if not path.startswith(API):
            return web.Response(status=404)
        return await _forward(request.app[KEY_HASS], request, path, with_token=True)

    get = _handle
    put = _handle
    post = _handle
    delete = _handle
