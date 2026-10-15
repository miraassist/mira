"""Constants for the Mira integration."""

DOMAIN = "mira"
# The version of the code that runs: Home Assistant imports an integration
# once, at start, so after files are replaced only a restart changes it.
# Kept equal to manifest.json.
VERSION = "0.3.3"

CONF_TIMEOUT = "timeout"
# Set once the agent's watchers, persons and rooms were imported as subentries.
CONF_IMPORTED = "imported"
# The agent's Home Assistant user: only it may manage Mira's automations.
CONF_AGENT_USER = "agent_user_id"
DEFAULT_TIMEOUT = 15

# Spoken when the agent does not answer in time or fails. A satellite that
# says nothing at all looks broken; this at least tells the user why.
ERROR_TEXT = {
    "de": "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal.",
    "en": "I can't answer right now. Please try again in a moment.",
}
