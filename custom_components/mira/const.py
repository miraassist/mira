"""Constants for the Mira integration."""

DOMAIN = "mira"
# The version of the code that runs: Home Assistant imports an integration
# once, at start, so after files are replaced only a restart changes it.
# Kept equal to manifest.json.
VERSION = "0.23.0"

CONF_PERSONA = "persona"
# Mira's own web search, switched at the service (docs/spec/web-search.md).
CONF_WEB_SEARCH = "web_search"
CONF_OWN_HOLD = "own_message_hold"
CONF_TIMEOUT = "timeout"
# Set once the service's persons and rooms were imported as settings on HA's persons and satellites.
CONF_IMPORTED = "imported"
# The id of the token an admin gave the service (access.py); never the token.
CONF_AGENT_TOKEN = "agent_token_id"
# Set when that token was pasted by an admin (since 0.23); entries without it
# may hold the owner's token the integration made itself before.
CONF_TOKEN_GIVEN = "agent_token_given"
# Assistants (Assist pipelines) to get Mira as their conversation agent
# once its entity exists, and that entity, to hand them back on removal.
CONF_PIPELINES = "pipelines"
CONF_PIPELINE_AGENT = "pipeline_agent"
# The service each of those assistants had before, given back on removal.
CONF_PIPELINES_BEFORE = "pipelines_before"
DEFAULT_TIMEOUT = 15
# Mira's web UI as a page in Home Assistant's sidebar (panel.py).
CONF_SIDEBAR = "sidebar"

# Spoken when the service does not answer in time or fails. A satellite that
# says nothing at all looks broken; this at least tells the user why.
ERROR_TEXT = {
    "de": "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal.",
    "en": "I can't answer right now. Please try again in a moment.",
}
# Ends a spoken answer whose connection to the service broke off after part of it.
BROKEN_TEXT = {
    "de": "Da ist etwas schiefgegangen.",
    "en": "Something went wrong.",
}

# Fired by the service once it is connected after its start: the integration
# sends its settings at once instead of at its next poll
# (docs/spec/editing.md, decision 10).
EVENT_STARTED = "mira_started"
