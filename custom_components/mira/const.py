"""Constants for the Mira integration."""

DOMAIN = "mira"
# The version of the code that runs: Home Assistant imports an integration
# once, at start, so after files are replaced only a restart changes it.
# Kept equal to manifest.json.
VERSION = "0.6.44"

CONF_PERSONA = "persona"
# Mira's own web search, switched at the agent (docs/spec/websuche.md).
CONF_WEB_SEARCH = "web_search"
CONF_OWN_HOLD = "own_message_hold"
CONF_TIMEOUT = "timeout"
# Set once the agent's persons and rooms were imported as settings on HA's persons and satellites.
CONF_IMPORTED = "imported"
# The agent's Home Assistant user: only it may manage Mira's automations.
CONF_AGENT_USER = "agent_user_id"
# The refresh token the integration made for the agent (access.py).
CONF_AGENT_TOKEN = "agent_token_id"
# Assistants (Assist pipelines) to get Mira as their conversation agent
# once its entity exists, and that entity, to hand them back on removal.
CONF_PIPELINES = "pipelines"
CONF_PIPELINE_AGENT = "pipeline_agent"
# The agent each of those assistants had before, given back on removal.
CONF_PIPELINES_BEFORE = "pipelines_before"
DEFAULT_TIMEOUT = 15
# Mira's web UI as a page in Home Assistant's sidebar (panel.py).
CONF_SIDEBAR = "sidebar"

# Spoken when the agent does not answer in time or fails. A satellite that
# says nothing at all looks broken; this at least tells the user why.
ERROR_TEXT = {
    "de": "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal.",
    "en": "I can't answer right now. Please try again in a moment.",
}
