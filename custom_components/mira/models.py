"""Where the agent's AI model (LLM) can come from in the setup form
(docs/spec/einrichtung.md): an AI integration Home Assistant already has,
a common cloud provider, or a server of one's own. A key taken over from
Home Assistant goes to the agent and is never shown."""

from __future__ import annotations

from dataclasses import dataclass

from homeassistant.config_entries import ConfigEntryState
from homeassistant.core import HomeAssistant

# The cloud providers (key → name, address) come from the agent, which
# keeps the one list of them (GET /v1/setup/providers).
Providers = dict[str, tuple[str, str]]

CUSTOM = "custom"
HA_PREFIX = "ha:"

KIND_CLOUD = "cloud"
KIND_CUSTOM = "custom"

# Home Assistant's AI integrations whose settings the agent can use: the
# provider's address (or the entry's own), and where the model is kept in
# the entry's conversation subentry.
_HA_SOURCES = {
    "openai_conversation": ("openai", "chat_model"),
    "google_generative_ai_conversation": ("gemini", "chat_model"),
    "open_router": ("openrouter", "model"),
    "anthropic": ("anthropic", "chat_model"),
    "ollama": (None, "model"),
}


@dataclass
class ModelSource:
    """A model server, ready to ask for its models."""

    label: str
    base_url: str
    api_key: str
    kind: str
    model: str = ""


def ha_sources(hass: HomeAssistant, providers: Providers) -> dict[str, ModelSource]:
    """The AI integrations set up in Home Assistant, by "ha:<entry id>";
    one whose provider the agent does not know is left out."""
    out: dict[str, ModelSource] = {}
    for domain, (provider, model_key) in _HA_SOURCES.items():
        for entry in hass.config_entries.async_entries(domain):
            if entry.state is not ConfigEntryState.LOADED:
                continue
            model = ""
            for sub in entry.subentries.values():
                if sub.subentry_type == "conversation" and sub.data.get(model_key):
                    model = str(sub.data[model_key]).removeprefix("models/")
                    break
            if provider:
                if provider not in providers:
                    continue
                name, base_url = providers[provider]
                key, kind = str(entry.data.get("api_key", "")), KIND_CLOUD
                if not key:
                    continue
            else:
                name, base_url = "Ollama", str(entry.data.get("url", "")).rstrip("/") + "/v1"
                key, kind = str(entry.data.get("api_key") or ""), KIND_CUSTOM
                if base_url == "/v1":
                    continue
            label = f"{name} ({model})" if model else name
            out[HA_PREFIX + entry.entry_id] = ModelSource(label, base_url, key, kind, model)
    return out
