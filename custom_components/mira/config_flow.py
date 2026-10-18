"""Config flow for Mira (docs/spec/einrichtung.md).

The agent announces itself by mDNS (_mira._tcp), so Home Assistant offers it
under "Discovered". A new agent pairs without a code, gets Home Assistant's
access and an AI model, and becomes the assistants' conversation agent. Mira's settings for a person
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
from homeassistant.core import HomeAssistant, callback
from homeassistant.data_entry_flow import section
from homeassistant.helpers import selector
from homeassistant.helpers.aiohttp_client import async_get_clientsession
from homeassistant.helpers.service_info.zeroconf import ZeroconfServiceInfo

from .access import assistants, ensure_agent_access, ensure_mcp_server, ha_url, revoke_agent_token
from .api import MiraAuthError, MiraBadCode, MiraClient, MiraError, MiraInvalidConfig
from .const import (
    CONF_AGENT_TOKEN,
    CONF_AGENT_USER,
    CONF_IMPORTED,
    CONF_PERSONA,
    CONF_PIPELINES,
    CONF_SIDEBAR,
    CONF_TIMEOUT,
    CONF_WEB_SEARCH,
    DEFAULT_TIMEOUT,
    DOMAIN,
)
from .models import CUSTOM, KIND_CLOUD, KIND_CUSTOM, PROVIDERS, ModelSource, ha_sources
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

# How long the agent may take to start anew after setup.
SETUP_WAIT = 60

# The guide's setup section, linked from the first form.
GUIDE = "https://github.com/miraassist/mira/blob/main/docs/guide/{}.md#{}"

# What the agent says went wrong with a model, as the form's error.
MODEL_ERRORS = {"unreachable", "bad_key", "no_model", "no_tools", "server_error"}

# The few words the model choice needs that name no product.
WORDS = {
    "de": {
        "custom": "Eigener Server (Adresse eingeben)",
        "from_ha": "Aus Home Assistant",
        "keeps": "bleibt {model}",
        "none": "keinem (später unter Einstellungen → Sprachassistenten)",
        "code": "Kopplungscode",
        "model": "KI-Modell",
        "answers": "Mira verwenden in",
        "missing": "fehlt noch",
        "bad_code": "Der Kopplungscode stimmt nicht oder ist abgelaufen. Gib ihn neu ein; einen neuen gibt es mit `docker exec mira /app/mira pair`.",
        "failed": "Das Koppeln hat nicht geklappt: {error}",
    },
    "en": {
        "custom": "Own server (enter its address)",
        "from_ha": "From Home Assistant",
        "keeps": "stays {model}",
        "none": "none (later under Settings → Voice assistants)",
        "code": "Pairing code",
        "model": "AI model",
        "answers": "Use Mira in",
        "missing": "still missing",
        "bad_code": "The pairing code is wrong or expired. Enter it again; get a new one with `docker exec mira /app/mira pair`.",
        "failed": "Pairing did not work: {error}",
    },
}


def _words(hass: HomeAssistant) -> dict[str, str]:
    return WORDS["de" if hass.config.language.startswith("de") else "en"]


def _model_error(err: MiraError, errors: dict[str, str], placeholders: dict[str, str]) -> None:
    """The agent's reason as the form's error, its detail below."""
    reason = getattr(err, "reason", "")
    errors["base"] = f"model_{reason}" if reason in MODEL_ERRORS else "setup_failed"
    placeholders["error"] = f"\n\n`{err}`"


class ModelSteps:
    """Choosing the AI model (LLM): where it comes from (an AI integration
    Home Assistant has, a cloud provider, or a server of one's own), then a
    model from what it offers. The agent tries it with a real request
    before it keeps it. Used by the setup and by the options; _use_model
    does the rest."""

    _source: ModelSource | None = None
    _models: list[str] = []

    def _model_client(self) -> MiraClient:
        raise NotImplementedError

    async def _use_model(self, llm: dict[str, Any]) -> ConfigFlowResult:
        raise NotImplementedError

    async def _reopened(self, step_id: str) -> ConfigFlowResult | None:
        """A dialog closed in this step and opened again; the setup goes
        back to its overview, the options stay."""
        return None

    async def async_step_model_source(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Where the model comes from; what Home Assistant has comes first."""
        if user_input is None and (back := await self._reopened("model_source")):
            return back
        from_ha = ha_sources(self.hass)
        words = _words(self.hass)
        if user_input is not None:
            choice = user_input["source"]
            if choice in from_ha:
                self._source = from_ha[choice]
                return await self._list_models("model_source")
            if choice in PROVIDERS:
                name, base_url = PROVIDERS[choice]
                self._source = ModelSource(name, base_url, "", KIND_CLOUD)
                return await self.async_step_model_key()
            self._source = ModelSource(words["custom"], "", "", KIND_CUSTOM)
            return await self.async_step_model_server()
        return self._source_form(None, {"error": ""})

    def _source_form(self, errors: dict[str, str] | None, placeholders: dict[str, str]) -> ConfigFlowResult:
        from_ha = ha_sources(self.hass)
        words = _words(self.hass)
        options = [selector.SelectOptionDict(value=k, label=f"{words['from_ha']}: {v.label}") for k, v in from_ha.items()]
        options += [selector.SelectOptionDict(value=k, label=name) for k, (name, _) in PROVIDERS.items()]
        options.append(selector.SelectOptionDict(value=CUSTOM, label=words["custom"]))
        return self.async_show_form(
            step_id="model_source",
            data_schema=vol.Schema(
                {
                    vol.Required("source", default=options[0]["value"]): selector.SelectSelector(
                        selector.SelectSelectorConfig(options=options, mode=selector.SelectSelectorMode.LIST)
                    )
                }
            ),
            errors=errors,
            description_placeholders=placeholders,
        )

    async def async_step_model_key(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The key for a cloud provider."""
        if user_input is None and (back := await self._reopened("model_key")):
            return back
        assert self._source
        if user_input is not None:
            self._source.api_key = user_input["api_key"].strip()
            return await self._list_models("model_key")
        return self._key_form({}, {"error": ""})

    def _key_form(self, errors: dict[str, str], placeholders: dict[str, str]) -> ConfigFlowResult:
        assert self._source
        return self.async_show_form(
            step_id="model_key",
            data_schema=vol.Schema(
                {
                    vol.Required("api_key"): selector.TextSelector(
                        selector.TextSelectorConfig(type=selector.TextSelectorType.PASSWORD)
                    )
                }
            ),
            errors=errors,
            description_placeholders={"provider": self._source.label, **placeholders},
        )

    async def async_step_model_server(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """A server of one's own: its address as the agent sees it, maybe a key."""
        if user_input is None and (back := await self._reopened("model_server")):
            return back
        assert self._source
        if user_input is not None:
            self._source.base_url = user_input["base_url"].strip().rstrip("/")
            self._source.label = self._source.base_url
            self._source.api_key = user_input.get("api_key", "").strip()
            return await self._list_models("model_server")
        return self._server_form({}, {"error": ""})

    def _server_form(self, errors: dict[str, str], placeholders: dict[str, str]) -> ConfigFlowResult:
        assert self._source
        return self.async_show_form(
            step_id="model_server",
            data_schema=self.add_suggested_values_to_schema(
                vol.Schema(
                    {
                        vol.Required("base_url"): str,
                        vol.Optional("api_key"): selector.TextSelector(
                            selector.TextSelectorConfig(type=selector.TextSelectorType.PASSWORD)
                        ),
                    }
                ),
                {"base_url": self._source.base_url},
            ),
            errors=errors,
            description_placeholders=placeholders,
        )

    async def _list_models(self, back: str) -> ConfigFlowResult:
        """Ask the server for its models; on failure, back to the form."""
        assert self._source
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        try:
            self._models = await self._model_client().models(self._source.base_url, self._source.api_key)
        except MiraError as err:
            _model_error(err, errors, placeholders)
        if not errors:
            return await self.async_step_model()
        if back == "model_key":
            return self._key_form(errors, placeholders)
        if back == "model_server":
            return self._server_form(errors, placeholders)
        return self._source_form(errors, placeholders)

    async def async_step_model(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The model from the list; the agent tries it before keeping it."""
        if user_input is None and (back := await self._reopened("model")):
            return back
        assert self._source
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            self._source.model = user_input["model"]
            llm = {
                "kind": self._source.kind,
                "base_url": self._source.base_url,
                "model": self._source.model,
                "api_key": self._source.api_key,
            }
            try:
                return await self._use_model(llm)
            except MiraError as err:
                _model_error(err, errors, placeholders)
            except (RuntimeError, TimeoutError) as err:
                errors["base"], placeholders["error"] = "setup_failed", f"\n\n`{err}`"
        models = sorted(self._models)
        default = self._source.model if self._source.model else vol.UNDEFINED
        return self.async_show_form(
            step_id="model",
            data_schema=vol.Schema(
                {
                    vol.Required("model", default=default): selector.SelectSelector(
                        selector.SelectSelectorConfig(options=models, custom_value=True)
                    )
                }
            ),
            errors=errors,
            description_placeholders={"server": self._source.label, **placeholders},
        )


class MiraConfigFlow(ModelSteps, ConfigFlow, domain=DOMAIN):
    """Find the agent, then an overview that says what is chosen and what
    is missing, with its branches: the AI model, the assistants Mira
    answers in, "Pair". Every branch comes back to it, also when its
    dialog was closed and opened again. Nothing is done at the agent or in
    Home Assistant before "Pair", so a setup broken off leaves nothing
    behind (docs/spec/einrichtung.md)."""

    VERSION = 1

    _url: str = ""
    # The agent's token: entered (mira.yaml) or handed out when pairing.
    _token: str = ""
    # A pairing code, for an agent that was paired before.
    _code: str | None = None
    # The model the agent has already, if any.
    _agent_model: str = ""
    _llm: dict[str, Any] | None = None
    _pipelines: list[str] | None = None
    _error: str = ""
    _data: dict[str, Any] = {}
    # The step last shown: a step run again without input while it is
    # shown means its dialog was opened again.
    _shown: str = ""

    @callback
    def async_show_form(self, *, step_id: str | None = None, **kwargs: Any) -> ConfigFlowResult:
        self._shown = step_id or ""
        return super().async_show_form(step_id=step_id, **kwargs)

    @callback
    def async_show_menu(self, *, step_id: str | None = None, **kwargs: Any) -> ConfigFlowResult:
        self._shown = step_id or ""
        return super().async_show_menu(step_id=step_id, **kwargs)

    async def _reopened(self, step_id: str) -> ConfigFlowResult | None:
        if self._shown == step_id:
            return await self.async_step_summary()
        return None

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
                return await self._step_auth(status, discovered=False)
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
        self.context["title_placeholders"] = {
            "name": discovery_info.name.split(".")[0],
            "url": url,
            "host": f"{host}:{discovery_info.port}",
        }
        try:
            status = await self._client().pairing()
        except MiraError:
            status = None
        return await self._step_auth(status, discovered=True)

    async def _step_auth(self, status: dict[str, Any] | None, discovered: bool) -> ConfigFlowResult:
        """An agent that pairs: the first time a click (for a discovered
        one), later a code; an agent whose token is in its mira.yaml, or
        from before pairing, wants that token."""
        if not status or not status.get("pairs"):
            return await self.async_step_token()
        self._agent_model = str(status.get("model") or "")
        if not status.get("open"):
            self._code = ""
        elif discovered:
            return await self.async_step_confirm()
        return await self.async_step_summary()

    async def async_step_confirm(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """A new agent was found; nothing happens yet."""
        if user_input is not None:
            return await self.async_step_summary()
        self._set_confirm_only()
        return self.async_show_form(step_id="confirm", description_placeholders={"url": self._url})

    async def async_step_pair(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The agent was paired before: a code from mira pair. It is used,
        and checked, only when pairing at the end."""
        if user_input is None and (back := await self._reopened("pair")):
            return back
        if user_input is not None:
            self._code = user_input["code"].strip()
            self._error = ""
            return await self.async_step_summary()
        return self.async_show_form(step_id="pair", data_schema=CODE_SCHEMA, description_placeholders={"url": self._url})

    async def async_step_token(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The token, for an agent whose token is set in its mira.yaml."""
        errors: dict[str, str] = {}
        if user_input is not None:
            self._token = user_input[CONF_TOKEN]
            try:
                health = await self._client().health()
            except MiraAuthError:
                errors["base"] = "invalid_auth"
            except MiraError:
                errors["base"] = "cannot_connect"
            if not errors:
                if health.get("id") and not self.unique_id:
                    await self.async_set_unique_id(health["id"])
                    self._abort_if_unique_id_configured(updates={CONF_URL: self._url})
                if health.get("status") != "setup":
                    # Set up by its mira.yaml: only the assistants.
                    return await self.async_step_assistants()
                self._agent_model = str(health.get("model") or "")
                return await self.async_step_summary()
        return self.async_show_form(
            step_id="token", data_schema=TOKEN_SCHEMA, errors=errors, description_placeholders={"url": self._url}
        )

    async def _use_model(self, llm: dict[str, Any]) -> ConfigFlowResult:
        await self._model_client().probe(llm)
        self._llm = llm
        self._error = ""
        return await self.async_step_summary()

    async def async_step_assistants(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Which assistants get Mira as their conversation agent; the
        preferred one is ticked. Done once Mira's entity exists."""
        if user_input is None and (back := await self._reopened("assistants")):
            return back
        pipelines = assistants(self.hass)
        if user_input is not None or not pipelines:
            self._pipelines = (user_input or {}).get(CONF_PIPELINES, [])
            if not self._pairs_or_sets_up():
                return self._create()
            return await self.async_step_summary()
        default = self._pipelines if self._pipelines is not None else [p for p, (_, pref) in pipelines.items() if pref]
        return self.async_show_form(
            step_id="assistants",
            data_schema=vol.Schema(
                {
                    vol.Optional(CONF_PIPELINES, default=default): selector.SelectSelector(
                        selector.SelectSelectorConfig(
                            options=[selector.SelectOptionDict(value=p, label=name) for p, (name, _) in pipelines.items()],
                            multiple=True,
                            mode=selector.SelectSelectorMode.LIST,
                        )
                    )
                }
            ),
        )

    def _pairs_or_sets_up(self) -> bool:
        """Whether "Pair" still has to happen: always, unless the agent is
        set up by its mira.yaml and was added with its token."""
        return not self._token or bool(self._agent_model) or self._llm is not None or self._code is not None

    async def async_step_summary(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """The overview: what is chosen, what is missing, and the branches;
        "Pair" does it all."""
        words = _words(self.hass)
        names = assistants(self.hass)
        if self._pipelines is None:
            self._pipelines = [p for p, (_, pref) in names.items() if pref]
        lines = [f"- **Mira:** {self._url}"]
        missing = []
        if self._code is not None:
            lines.append(f"- **{words['code']}:** " + ("✓" if self._code else words["missing"]))
            if not self._code:
                missing.append(words["code"])
        if self._llm:
            source = self._source.label if self._source else self._llm["base_url"]
            lines.append(f"- **{words['model']}:** ✓ {source}, {self._llm['model']}")
        elif self._agent_model:
            lines.append(f"- **{words['model']}:** ✓ " + words["keeps"].format(model=self._agent_model))
        else:
            lines.append(f"- **{words['model']}:** {words['missing']}")
            missing.append(words["model"])
        chosen = ", ".join(names[p][0] for p in self._pipelines if p in names) or words["none"]
        lines.append(f"- **{words['answers']}:** {chosen}")
        # "Start using Mira" shows only once nothing is missing.
        options = ["pair" if self._code is not None else "model_source"]
        if names:
            options.append("assistants")
        if not missing:
            options.append("finish")
        return self.async_show_menu(
            step_id="summary",
            menu_options=options,
            description_placeholders={
                "status": "\n".join(lines),
                "error": f"\n\n**{self._error}**" if self._error else "",
            },
        )

    async def async_step_finish(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Pair: Mira's own HA user and token, HA's MCP server, and the agent
        paired and set up in one call; it starts anew and is waited for."""
        words = _words(self.hass)
        self._error = ""
        refresh_id = None
        try:
            await ensure_mcp_server(self.hass)
            user_id, refresh_id, ha_token = await ensure_agent_access(self.hass)
            if self._token:
                await self._client().setup(ha_url(self.hass), ha_token, self._llm)
            else:
                self._token = await self._client().pair(self._code or "", ha_url(self.hass), ha_token, self._llm)
            await self._wait_ready()
        except MiraBadCode:
            self._error = words["bad_code"]
        except (MiraError, RuntimeError, TimeoutError) as err:
            self._error = words["failed"].format(error=err)
        if self._error:
            if refresh_id:
                revoke_agent_token(self.hass, refresh_id)
            return await self.async_step_summary()
        self._data = {CONF_IMPORTED: True, CONF_AGENT_USER: user_id, CONF_AGENT_TOKEN: refresh_id}
        return self._create()

    def _create(self) -> ConfigFlowResult:
        data = {CONF_URL: self._url, CONF_TOKEN: self._token, **self._data}
        return self.async_create_entry(title="Mira", data={**data, CONF_PIPELINES: self._pipelines or []})

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
    # The persona and the web search switch as the agent had them when the
    # form opened; None without the agent.
    _general: dict[str, Any] | None = None

    async def async_step_init(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        return self.async_show_menu(step_id="init", menu_options=["general", "model", "persons", "satellites"])

    def _model_client(self) -> MiraClient:
        return self.config_entry.runtime_data.client

    async def async_step_model(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        """Another AI model: where it comes from first, then the model."""
        if self._source is None:
            return await self.async_step_model_source()
        return await ModelSteps.async_step_model(self, user_input)

    async def _use_model(self, llm: dict[str, Any]) -> ConfigFlowResult:
        await self._model_client().setup(None, None, llm)
        return self.async_create_entry(data=dict(self.config_entry.options))

    async def async_step_general(self, user_input: dict[str, Any] | None = None) -> ConfigFlowResult:
        # The persona and the web search switch live at the agent, like
        # everything it is asked for per request; the entry keeps only what
        # the integration itself needs. Without the agent those fields are
        # left out, so nothing is overwritten blindly.
        runtime = getattr(self.config_entry, "runtime_data", None)
        client = runtime.client if runtime else None
        errors: dict[str, str] = {}
        placeholders = {"error": ""}
        if user_input is not None:
            data = dict(user_input)
            changes: dict[str, Any] = {}
            persona = data.pop(CONF_PERSONA, None)
            if self._general is not None and persona is not None and persona.strip() != self._general["persona"]:
                changes["persona"] = persona.strip()
            search = data.pop(CONF_WEB_SEARCH, None)
            if self._general is not None and search is not None and search != self._general["web_search"]:
                changes["web_search"] = search
            if client and changes:
                try:
                    await client.set_general(changes)
                except MiraInvalidConfig as err:
                    errors["base"], placeholders["error"] = "invalid_config", str(err)
                except MiraError:
                    errors["base"] = "cannot_connect"
            if not errors:
                return self.async_create_entry(data=dict(self.config_entry.options) | data)
        else:
            try:
                self._general = await client.general() if client else None
            except MiraError:
                self._general = None
        options = self.config_entry.options
        fields: dict[Any, Any] = {
            vol.Required(CONF_TIMEOUT, default=options.get(CONF_TIMEOUT, DEFAULT_TIMEOUT)): vol.All(
                int, vol.Range(min=3, max=120)
            ),
            vol.Required(CONF_SIDEBAR, default=options.get(CONF_SIDEBAR, True)): bool,
        }
        if self._general is not None:
            fields[vol.Required(CONF_WEB_SEARCH, default=self._general["web_search"])] = bool
            fields[vol.Optional(CONF_PERSONA, description={"suggested_value": self._general["persona"]})] = (
                selector.TextSelector(selector.TextSelectorConfig(multiline=True))
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
