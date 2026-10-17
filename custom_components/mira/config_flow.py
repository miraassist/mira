"""Config flow for Mira: agent URL and token, checked against /v1/health.

The agent announces itself by mDNS (_mira._tcp), so Home Assistant offers it
under "Discovered" and only asks for the token. Mira's settings for a person
or a satellite are set under "Configure" and stored on that person or
satellite; see managed.py.
"""

from __future__ import annotations

import asyncio
from typing import Any

import voluptuous as vol

from homeassistant.config_entries import (
    ConfigEntry,
    ConfigEntryState,
    ConfigFlow,
    ConfigFlowResult,
    OptionsFlow,
)
from homeassistant.const import CONF_TOKEN, CONF_URL
from homeassistant.core import callback
from homeassistant.data_entry_flow import section
from homeassistant.helpers import selector
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .access import ensure_agent_access, ensure_mcp_server, ha_url
from .api import MiraAuthError, MiraBadCode, MiraClient, MiraError, MiraInvalidConfig
from .const import (
    CONF_AGENT_TOKEN,
    CONF_AGENT_USER,
    CONF_IMPORTED,
    CONF_PERSONA,
    CONF_SIDEBAR,
    CONF_TIMEOUT,
    DEFAULT_TIMEOUT,
    DOMAIN,
)
from .managed import (
    ACCEPT,
    all_persons,
    all_satellites,
    entity_name,
    notify_targets,
    person_settings,
    satellite_settings,
    save_settings,
)

USER_SCHEMA = vol.Schema({vol.Required(CONF_URL, default="http://localhost:8787"): str})
TOKEN_SCHEMA = vol.Schema({vol.Required(CONF_TOKEN): str})
CODE_SCHEMA = vol.Schema({vol.Required("code"): str})

# Model servers the setup offers, with the address they usually have as
# seen from the agent (docs/spec/einrichtung.md).
MODEL_KINDS = {
    "lmstudio": "http://localhost:1234/v1",
    "ollama": "http://localhost:11434/v1",
    "llamacpp": "http://localhost:8080/v1",
    "cloud": "https://api.openai.com/v1",
}
# How long the agent may take to start anew after setup.
SETUP_WAIT = 60

# The guide's setup section, linked from the first form.
GUIDE = "https://github.com/miraassist/mira/blob/main/docs/guide/{}.md#{}"


class ModelSteps:
    """Choosing the language model: the kind of server, its address (as the
    agent sees it) and key, then a model from what it offers. Used by the
    setup and by the options; _model_chosen does the rest."""

    _kind: str = "lmstudio"
    _base_url: str = ""
    _api_key: str = ""
    _models: list[str] = []

    async def async_step_model_kind(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Which kind of model server."""
        if user_input is not None:
            self._kind = user_input["kind"]
            return await self.async_step_model_server()
        return self.async_show_form(
            step_id="model_kind",
            data_schema=vol.Schema(
                {
                    vol.Required("kind", default=self._kind): selector.SelectSelector(
                        selector.SelectSelectorConfig(options=list(MODEL_KINDS), translation_key="model_kind")
                    )
                }
            ),
        )

    async def async_step_model_server(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Its address as the agent sees it, and a key for a cloud."""
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            self._base_url = user_input["base_url"].rstrip("/")
            self._api_key = user_input.get("api_key", "")
            try:
                self._models = await self._model_client().models(self._base_url, self._api_key)
            except MiraInvalidConfig as err:
                errors["base"], placeholders["error"] = "model_server", str(err)
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                return await self.async_step_model()
        return self.async_show_form(
            step_id="model_server",
            data_schema=vol.Schema(
                {
                    vol.Required("base_url", default=self._base_url or MODEL_KINDS[self._kind]): str,
                    vol.Optional("api_key", default=self._api_key): selector.TextSelector(
                        selector.TextSelectorConfig(type=selector.TextSelectorType.PASSWORD)
                    ),
                }
            ),
            errors=errors,
            description_placeholders=placeholders,
        )




class MiraConfigFlow(ModelSteps, ConfigFlow, domain=DOMAIN):
    """Find the agent, pair with it, and, if it is new, set it up: Home
    Assistant's access and the language model (docs/spec/einrichtung.md)."""

    VERSION = 1

    _url: str = ""
    _token: str = ""

    def _client(self, token: str = "") -> MiraClient:
        return MiraClient(async_get_clientsession(self.hass), self._url, token or self._token)

    def _model_client(self) -> MiraClient:
        return self._client()

    async def async_step_user(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The agent's address, when it was not found on the network."""
        errors: dict[str, str] = {}
        if user_input is not None:
            self._url = user_input[CONF_URL].rstrip("/")
            self._async_abort_entries_match({CONF_URL: self._url})
            try:
                status = await self._client().pairing()
            except MiraError:
                status = None
                try:
                    await self._client().health()
                except MiraAuthError:
                    pass  # an agent from before pairing: it wants its token
                except MiraError:
                    errors["base"] = "cannot_connect"
            if not errors:
                if status and status.get("id"):
                    await self.async_set_unique_id(status["id"])
                    self._abort_if_unique_id_configured(updates={CONF_URL: self._url})
                return await self._step_auth(status)
        return self.async_show_form(
            step_id="user",
            data_schema=self.add_suggested_values_to_schema(USER_SCHEMA, user_input),
            errors=errors,
            description_placeholders={"guide": self._guide()},
        )

    def _guide(self) -> str:
        if self.hass.config.language.startswith("de"):
            return GUIDE.format("de", "einrichtung")
        return GUIDE.format("en", "setup")

    async def async_step_zeroconf(self, discovery_info: ZeroconfServiceInfo) -> ConfigFlowResult:
        """An agent announced itself on the network."""
        agent_id = discovery_info.properties.get("id")
        if not agent_id:
            return self.async_abort(reason="no_id")
        host = discovery_info.host
        if ":" in host:
            host = f"[{host}]"
        url = f"http://{host}:{discovery_info.port}"
        await self.async_set_unique_id(agent_id)
        self._abort_if_unique_id_configured(updates={CONF_URL: url})
        # An entry from before agents had ids: same address, same agent.
        for entry in self._async_current_entries(include_ignore=False):
            if entry.unique_id is None and entry.data.get(CONF_URL) == url:
                self.hass.config_entries.async_update_entry(entry, unique_id=agent_id)
                return self.async_abort(reason="already_configured")
        self._url = url
        self.context["title_placeholders"] = {"name": discovery_info.name.split(".")[0], "url": url}
        try:
            status = await self._client().pairing()
        except MiraError:
            status = None
        return await self._step_auth(status)

    async def _step_auth(self, status: dict[str, Any] | None) -> ConfigFlowResult:
        """A code when the agent offers pairing, else its token."""
        if status and status.get("pairing"):
            return await self.async_step_pair()
        return await self.async_step_token()

    async def async_step_pair(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The pairing code the agent shows in its log and on its page."""
        errors: dict[str, str] = {}
        if user_input is not None:
            try:
                self._token = await self._client().pair(user_input["code"])
            except MiraBadCode:
                errors["base"] = "bad_code"
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                return await self._after_auth()
        return self.async_show_form(
            step_id="pair", data_schema=CODE_SCHEMA, errors=errors, description_placeholders={"url": self._url}
        )

    async def async_step_token(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The token, for an agent whose token is set in its mira.yaml."""
        errors: dict[str, str] = {}
        if user_input is not None:
            self._token = user_input[CONF_TOKEN]
            try:
                await self._client().health()
            except MiraAuthError:
                errors["base"] = "invalid_auth"
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                return await self._after_auth()
        return self.async_show_form(
            step_id="token", data_schema=TOKEN_SCHEMA, errors=errors, description_placeholders={"url": self._url}
        )

    async def _after_auth(self) -> ConfigFlowResult:
        """A set-up agent is done; a new one gets Home Assistant and a model."""
        try:
            health = await self._client().health()
        except MiraError:
            return self.async_abort(reason="cannot_connect")
        if health.get("id") and not self.unique_id:
            await self.async_set_unique_id(health["id"])
            self._abort_if_unique_id_configured(updates={CONF_URL: self._url})
        if health.get("status") != "setup":
            return self.async_create_entry(title="Mira", data={CONF_URL: self._url, CONF_TOKEN: self._token})
        return await self.async_step_model_kind()

    async def async_step_model(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The model, from what the server offers; then everything is set up."""
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            llm = {"kind": self._kind, "base_url": self._base_url, "model": user_input["model"], "api_key": self._api_key}
            try:
                await ensure_mcp_server(self.hass)
                user_id, refresh_id, ha_token = await ensure_agent_access(self.hass)
                await self._client().setup(ha_url(self.hass), ha_token, llm)
                await self._wait_ready()
            except MiraInvalidConfig as err:
                errors["base"], placeholders["error"] = "model_server", str(err)
            except (MiraError, RuntimeError, TimeoutError) as err:
                errors["base"], placeholders["error"] = "setup_failed", str(err)
            if not errors:
                return self.async_create_entry(
                    title="Mira",
                    data={
                        CONF_URL: self._url,
                        CONF_TOKEN: self._token,
                        CONF_IMPORTED: True,
                        CONF_AGENT_USER: user_id,
                        CONF_AGENT_TOKEN: refresh_id,
                    },
                )
        return self.async_show_form(
            step_id="model",
            data_schema=vol.Schema(
                {
                    vol.Required("model"): selector.SelectSelector(
                        selector.SelectSelectorConfig(options=self._models, custom_value=True)
                    )
                }
            ),
            errors=errors,
            description_placeholders=placeholders,
        )

    async def _wait_ready(self) -> None:
        """The agent starts anew with what it got; wait until it runs."""
        for _ in range(SETUP_WAIT):
            await asyncio.sleep(1)
            try:
                health = await self._client().health()
            except MiraError:
                continue
            if health.get("status") != "setup":
                return
        raise TimeoutError("the agent did not start within a minute")

    @staticmethod
    @callback
    def async_get_options_flow(config_entry: ConfigEntry) -> OptionsFlow:
        return MiraOptionsFlow()



# Field names inside the per-person and per-satellite sections. Home
# Assistant translates a section's fields by the section's key, and those
# keys are names of persons and satellites, so the fields carry readable
# names themselves, in Home Assistant's language.
FIELD_NAMES = {
    "de": {
        "channels": "Kanäle (notify-Ziele, z. B. Telegram-Chat, Companion-App)",
        "accept_from": "Nachrichten annehmen von",
        "alone_is_me": "Allein zu Hause sind die Satelliten ich",
        "presence": "Bewegungsmelder",
        "morning": "Beim Hereinkommen ansagen",
        "indicator": "Anzeige (z. B. LED-Ring)",
    },
    "en": {
        "channels": "Channels (notify targets, e.g. Telegram chat, companion app)",
        "accept_from": "Take messages from",
        "alone_is_me": "Alone at home, the satellites are me",
        "presence": "Motion sensors",
        "morning": "Announce on entry",
        "indicator": "Indicator (e.g. LED ring)",
    },
}


class MiraOptionsFlow(ModelSteps, OptionsFlow):
    """General options, and Mira's settings on Home Assistant's persons and
    satellites: one form each, with a collapsible section per person or
    satellite, filled with what is set now. The settings are stored on the
    persons and satellites themselves (managed.py)."""

    _keys: dict[str, str] = {}
    # The persona as the agent had it when the form opened; None without
    # the agent.
    _persona: str | None = None

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        return self.async_show_menu(step_id="init", menu_options=["general", "model", "persons", "satellites"])

    def _model_client(self) -> MiraClient:
        return self.config_entry.runtime_data.client

    async def async_step_model(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Another language model: the kind first, then server and model."""
        if user_input is None and not self._models:
            return await self.async_step_model_kind()
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            llm = {"kind": self._kind, "base_url": self._base_url, "model": user_input["model"], "api_key": self._api_key}
            try:
                await self._model_client().setup(None, None, llm)
            except MiraInvalidConfig as err:
                errors["base"], placeholders["error"] = "model_server", str(err)
            except MiraError as err:
                errors["base"], placeholders["error"] = "setup_failed", str(err)
            if not errors:
                return self.async_create_entry(data=dict(self.config_entry.options))
        return self.async_show_form(
            step_id="model",
            data_schema=vol.Schema(
                {
                    vol.Required("model"): selector.SelectSelector(
                        selector.SelectSelectorConfig(options=self._models, custom_value=True)
                    )
                }
            ),
            errors=errors,
            description_placeholders=placeholders,
        )

    async def async_step_general(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        # The persona lives at the agent, like everything it is asked for
        # per request; the entry keeps only what the integration itself
        # needs. Without the agent the field is left out, so nothing is
        # overwritten blindly.
        runtime = getattr(self.config_entry, "runtime_data", None)
        client = runtime.client if runtime else None
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            data = dict(user_input)
            persona = data.pop(CONF_PERSONA, None)
            if client and persona is not None and persona.strip() != self._persona:
                try:
                    await client.set_persona(persona.strip())
                except MiraInvalidConfig as err:
                    errors["base"], placeholders["error"] = "invalid_config", str(err)
                except MiraError:
                    errors["base"] = "cannot_connect"
            if not errors:
                return self.async_create_entry(data=dict(self.config_entry.options) | data)
        else:
            try:
                self._persona = await client.persona() if client else None
            except MiraError:
                self._persona = None
        options = self.config_entry.options
        fields: dict[Any, Any] = {
            vol.Required(CONF_TIMEOUT, default=options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)): vol.All(
                int, vol.Range(min=3, max=120)
            ),
            vol.Required(CONF_SIDEBAR, default=options.get(CONF_SIDEBAR, True)): bool,
        }
        if self._persona is not None:
            fields[vol.Optional(CONF_PERSONA, description={"suggested_value": self._persona})] = selector.TextSelector(
                selector.TextSelectorConfig(multiline=True)
            )
        return self.async_show_form(
            step_id="general", data_schema=vol.Schema(fields), errors=errors, description_placeholders=placeholders
        )

    async def async_step_persons(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        fields = self._named(
            {
                vol.Optional("channels"): selector.SelectSelector(
                    selector.SelectSelectorConfig(
                        options=notify_targets(self.hass), custom_value=True, multiple=True,
                        mode=selector.SelectSelectorMode.DROPDOWN,
                    )
                ),
                vol.Optional("accept_from", default="all"): selector.SelectSelector(
                    selector.SelectSelectorConfig(options=list(ACCEPT), translation_key="accept_from")
                ),
                vol.Optional("alone_is_me", default=False): selector.BooleanSelector(),
            }
        )
        return await self._sections("persons", all_persons(self.hass), person_settings(self.hass), fields, user_input)

    async def async_step_satellites(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        fields = self._named(
            {
                vol.Optional("presence"): selector.EntitySelector(
                    selector.EntitySelectorConfig(domain="binary_sensor", multiple=True)
                ),
                vol.Optional("morning", default=False): selector.BooleanSelector(),
                vol.Optional("indicator"): selector.EntitySelector(selector.EntitySelectorConfig(domain="light")),
            }
        )
        return await self._sections(
            "satellites", all_satellites(self.hass), satellite_settings(self.hass), fields, user_input
        )

    def _names(self) -> dict[str, str]:
        return FIELD_NAMES.get((self.hass.config.language or "en")[:2], FIELD_NAMES["en"])

    def _named(self, fields: dict[Any, Any]) -> vol.Schema:
        """The fields under their readable names."""
        names = self._names()
        return vol.Schema({type(k)(names[str(k)], **_marker_args(k)): v for k, v in fields.items()})

    async def _sections(
        self,
        step_id: str,
        entities: list[str],
        current: dict[str, dict[str, Any]],
        fields: vol.Schema,
        user_input: dict[str, Any] | None,
    ) -> ConfigFlowResult:
        entry = self.config_entry
        if entry.state is not ConfigEntryState.LOADED:
            return self.async_abort(reason="not_loaded")
        if not entities:
            return self.async_abort(reason=f"no_{step_id}")
        # A section is named by what it is for; its key is the name, which
        # the form shows as the section's title.
        keys: dict[str, str] = {}
        for entity in entities:
            name = entity_name(self.hass, entity)
            keys[name if name not in keys else f"{name} ({entity})"] = entity
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            back = {v: k for k, v in self._names().items()}
            changes = {keys[k]: {back.get(f, f): x for f, x in v.items()} for k, v in user_input.items() if k in keys}
            try:
                if step_id == "persons":
                    await save_settings(self.hass, entry, persons=changes)
                else:
                    await save_settings(self.hass, entry, satellites=changes)
            except (ValueError, MiraInvalidConfig) as err:
                errors["base"], placeholders["error"] = "invalid_config", str(err)
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                return self.async_create_entry(data=dict(entry.options))
        schema = vol.Schema(
            {
                vol.Required(key): section(
                    self.add_suggested_values_to_schema(
                        fields,
                        (user_input or {}).get(key)
                        or {self._names().get(f, f): x for f, x in current.get(entity, {}).items()},
                    ),
                    {"collapsed": not current.get(entity)},
                )
                for key, entity in keys.items()
            }
        )
        return self.async_show_form(
            step_id=step_id,
            data_schema=schema,
            errors=errors,
            description_placeholders=placeholders,
        )


def _marker_args(marker: vol.Marker) -> dict[str, Any]:
    """A marker's default and description, to rename it."""
    out: dict[str, Any] = {}
    if marker.default is not vol.UNDEFINED:
        out["default"] = marker.default
    if marker.description is not None:
        out["description"] = marker.description
    return out
