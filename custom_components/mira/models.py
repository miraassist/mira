"""Where the agent's AI model (LLM) can come from in the setup form
(docs/spec/setup.md): a common cloud provider with its key, or a server of
one's own. Another integration's key is never read (docs/spec/hacs-default.md)."""

from __future__ import annotations

from dataclasses import dataclass

# The cloud providers (key → name, address) come from the agent, which
# keeps the one list of them (GET /v1/setup/providers).
Providers = dict[str, tuple[str, str]]

CUSTOM = "custom"

KIND_CLOUD = "cloud"
KIND_CUSTOM = "custom"


@dataclass
class ModelSource:
    """A model server, ready to ask for its models."""

    label: str
    base_url: str
    api_key: str
    kind: str
    model: str = ""
