"""Mira's web UI inside Home Assistant: a sidebar panel that shows the
agent's page through Home Assistant's own server.

The page's files come through an open view (they hold no data, and the
agent serves them without a token as well); its API calls come through
an authenticated view that adds the agent's token and says who is signed
in. So the page works over HA's HTTPS and from away, with HA's login.

Everyone sees the house's memory and their own; the rest of the page
(notices, tasks, settings) is for admins. The view enforces that, and the
agent once more from the user it is told.
"""

from __future__ import annotations

import asyncio
import logging
import re

import aiohttp
from aiohttp import web

from homeassistant.components import frontend
from homeassistant.components.http import HomeAssistantView
from homeassistant.config_entries import ConfigEntryState
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.core import HomeAssistant, callback
from homeassistant.exceptions import Unauthorized
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.http import KEY_HASS

from .api import MiraError, MiraInvalidConfig
from .const import DOMAIN
from .managed import (
    SATELLITE_DOMAINS,
    all_persons,
    all_satellites,
    entity_name,
    person_settings,
    satellite_area,
    satellite_settings,
    save_settings,
)

PANEL_URL_PATH = "mira"
PANEL_ICON = "mdi:face-woman-shimmer"
PAGE = "/api/mira/panel/ui"
# The agent's page and its files; nothing else goes through the open view.
PAGE_FILES = re.compile(r"ui/?|ui/icon\.png|ui/guide/(de|en)|ui/guide/img/[\w.-]+")
API = "v1/ui/"
# What a user who is no admin may call: their memory, and changing a fact.
NON_ADMIN_LIST = "v1/ui/memory"
NON_ADMIN_FACT = re.compile(r"v1/ui/memory/(?!compact$)[\w-]+")
# Who is signed in, for the agent (internal/ui: HeaderHAUser, HeaderHAAdmin).
HEADER_USER = "X-Mira-HA-User"
HEADER_ADMIN = "X-Mira-HA-Admin"
# Headers passed on, both ways: videos come in ranges (Safari asks so).
REQUEST_PASSED = ("Range", "If-Range")
PASSED = ("Content-Type", "Cache-Control", "Accept-Ranges", "Content-Range")
TIMEOUT = aiohttp.ClientTimeout(total=60)
# How often and how long to wait for an agent that is restarting.
CONNECT_TRIES = 4
CONNECT_WAIT = 1.0

_LOGGER = logging.getLogger(__name__)

# Shown in the panel while the agent cannot be reached; reloads itself.
WAITING_PAGE = """<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="refresh" content="5"><meta name="viewport" content="width=device-width">
<style>body{font-family:Roboto,system-ui,sans-serif;color:#555;display:flex;align-items:center;
justify-content:center;height:90vh;margin:0;text-align:center}@media (prefers-color-scheme:dark)
{body{color:#bbb;background:#111}}</style></head><body><p>Mira ist gerade nicht erreichbar, gleich noch mal …<br>
<small>Mira can't be reached right now, trying again …</small></p></body></html>"""


@callback
def async_register_views(hass: HomeAssistant) -> None:
    """Register the views once; they find the loaded entry per request."""
    hass.http.register_view(MiraPanelView())
    hass.http.register_view(MiraProxyView())
    hass.http.register_view(MiraSettingsView())


@callback
def async_register_panel(hass: HomeAssistant, title: str) -> None:
    """Show Mira in the sidebar, for everyone."""
    frontend.async_register_built_in_panel(
        hass,
        "iframe",
        sidebar_title=title,
        sidebar_icon=PANEL_ICON,
        frontend_url_path=PANEL_URL_PATH,
        config={"url": PAGE},
        require_admin=False,
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


async def _forward(
    hass: HomeAssistant, request: web.Request, path: str, with_token: bool, extra: dict[str, str] | None = None
) -> web.Response:
    """Pass the request on to the agent and its answer back."""
    agent = _agent(hass)
    if agent is None:
        return web.json_response({"error": "Mira is not set up"}, status=503)
    base, token = agent
    headers = {"Content-Type": request.headers.get("Content-Type", "application/json")}
    headers.update({k: request.headers[k] for k in REQUEST_PASSED if k in request.headers})
    if with_token:
        headers["Authorization"] = f"Bearer {token}"
    headers.update(extra or {})
    body = await request.read() if request.can_read_body else None
    # The agent may be restarting just now (an update, a container restart):
    # wait a moment for it rather than showing a connection error. Only a
    # connection that was never made is tried again, so nothing runs twice.
    last: Exception | None = None
    for attempt in range(CONNECT_TRIES):
        if attempt:
            await asyncio.sleep(CONNECT_WAIT)
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
        except aiohttp.ClientConnectorError as err:
            last = err
        except (aiohttp.ClientError, TimeoutError) as err:
            last = err
            break
    _LOGGER.debug("Mira agent not reachable: %s", last)
    # The page says this in Mira's words and tries again by itself.
    return web.json_response({"error": "unreachable", "unreachable": True}, status=503)


def _allowed(method: str, path: str) -> bool:
    """Whether a user who is no admin may make this call."""
    if method == "GET":
        return path == NON_ADMIN_LIST
    return method in ("PUT", "DELETE") and NON_ADMIN_FACT.fullmatch(path) is not None


class MiraPanelView(HomeAssistantView):
    """The agent's page and its files, without login like at the agent."""

    url = "/api/mira/panel/{path:.*}"
    name = "api:mira:panel"
    requires_auth = False

    async def get(self, request: web.Request, path: str) -> web.Response:
        if not PAGE_FILES.fullmatch(path):
            return web.Response(status=404)
        resp = await _forward(request.app[KEY_HASS], request, path, with_token=False)
        if resp.status == 503 and path in ("ui", "ui/"):
            # The page itself: a short note that loads Mira again by itself.
            return web.Response(status=503, text=WAITING_PAGE, content_type="text/html")
        return resp


class MiraProxyView(HomeAssistantView):
    """The page's API calls, for signed-in users, with the agent's token."""

    url = "/api/mira/proxy/{path:.*}"
    name = "api:mira:proxy"
    requires_auth = True

    async def _handle(self, request: web.Request, path: str) -> web.Response:
        user = request["hass_user"]
        if not path.startswith(API):
            return web.Response(status=404)
        if not user.is_admin and not _allowed(request.method, path):
            raise Unauthorized()
        who = {HEADER_USER: user.id, HEADER_ADMIN: "1" if user.is_admin else "0"}
        return await _forward(request.app[KEY_HASS], request, path, with_token=True, extra=who)

    get = _handle
    put = _handle
    post = _handle
    delete = _handle


class MiraSettingsView(HomeAssistantView):
    """Mira's settings on Home Assistant's persons and satellites, for the
    panel's forms: read them, and save them after the agent checked them.
    They are stored on the persons and satellites (managed.py), the same as
    the integration's options do. Admins only."""

    url = "/api/mira/settings/{kind}"
    name = "api:mira:settings"
    requires_auth = True

    async def get(self, request: web.Request, kind: str) -> web.Response:
        hass = request.app[KEY_HASS]
        if not request["hass_user"].is_admin:
            raise Unauthorized()
        if kind == "persons":
            current = person_settings(hass)
            items = [{"entity": e, "name": entity_name(hass, e), "settings": current.get(e, {})} for e in all_persons(hass)]
            notify = sorted(f"notify.{n}" for n in hass.services.async_services_for_domain("notify"))
            return web.json_response({"items": items, "notify": notify})
        if kind == "satellites":
            current = satellite_settings(hass)
            areas = ar.async_get(hass)
            items = []
            for e in all_satellites(hass):
                area = satellite_area(hass, e)
                area_entry = areas.async_get_area(area) if area else None
                items.append(
                    {
                        "entity": e,
                        "name": entity_name(hass, e),
                        "area": area_entry.name if area_entry else area,
                        "settings": current.get(e, {}),
                    }
                )
            return web.json_response(
                {"items": items, "binary_sensors": _choices(hass, "binary_sensor"), "lights": _choices(hass, "light")}
            )
        return web.Response(status=404)

    async def put(self, request: web.Request, kind: str) -> web.Response:
        hass = request.app[KEY_HASS]
        if not request["hass_user"].is_admin:
            raise Unauthorized()
        if kind not in ("persons", "satellites"):
            return web.Response(status=404)
        entry = _loaded(hass)
        if entry is None:
            return web.json_response({"error": "Mira is not set up"}, status=503)
        try:
            changes = await request.json()
            if not isinstance(changes, dict):
                raise ValueError("body must be {entity: settings}")
            known = set(all_persons(hass) if kind == "persons" else all_satellites(hass))
            if kind == "satellites":
                known |= {e for e in changes if e.split(".", 1)[0] in SATELLITE_DOMAINS}
            if unknown := set(changes) - known:
                raise ValueError(f"unknown: {', '.join(sorted(unknown))}")
            await save_settings(hass, entry, **{kind: changes})
        except (ValueError, MiraInvalidConfig) as err:
            return web.json_response({"error": str(err)}, status=422)
        except MiraError as err:
            return web.json_response({"error": f"Mira agent not reachable: {err}"}, status=502)
        return web.json_response({"saved": True})


def _choices(hass: HomeAssistant, domain: str) -> list[dict[str, str]]:
    return sorted(
        ({"id": s.entity_id, "name": s.name} for s in hass.states.async_all(domain)), key=lambda c: c["name"]
    )


def _loaded(hass: HomeAssistant):
    for entry in hass.config_entries.async_entries(DOMAIN):
        if entry.state is ConfigEntryState.LOADED:
            return entry
    return None
