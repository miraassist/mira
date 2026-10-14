"""Constants for the Lares integration."""

DOMAIN = "lares"

CONF_TIMEOUT = "timeout"
DEFAULT_TIMEOUT = 15

# Spoken when the agent does not answer in time or fails. A satellite that
# says nothing at all looks broken; this at least tells the user why.
ERROR_TEXT = {
    "de": "Ich kann gerade nicht antworten. Bitte versuch es gleich noch einmal.",
    "en": "I can't answer right now. Please try again in a moment.",
}
