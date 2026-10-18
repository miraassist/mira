"""Mira's labels in Home Assistant, known by their ids, never their names.

A label's id is made once from the name it is created with and never
changes; entities store only the id. So each label is created under a fixed
seed name, which makes the id, and then gets its name and description in
Home Assistant's language. The user may rename it freely.
"""

from __future__ import annotations

from homeassistant.core import HomeAssistant, callback
from homeassistant.helpers import label_registry as lr

# Mira's tasks: automations labelled with it (automations.py).
LABEL_TASKS = "mira"
# "Mira: nachfragen": Mira switches such an entity only after a yes; tasks
# never switch it. The agent reads it from the entity registry itself.
LABEL_CONFIRM = "mira_confirm"
# "Mira: einmalig": an automation that runs once and is then removed by the
# integration (once.py). Not a task, so it is not in the task list.
LABEL_ONCE = "mira_once"

# id: (seed name, icon, color, {language: (name, description)})
_LABELS: dict[str, tuple[str, str, str, dict[str, tuple[str, str]]]] = {
    LABEL_TASKS: (
        "Mira",
        "mdi:robot-happy-outline",
        "teal",
        {"de": ("Mira", "Aufgaben von Mira"), "en": ("Mira", "Mira's tasks")},
    ),
    LABEL_CONFIRM: (
        "mira_confirm",
        "mdi:hand-back-right-outline",
        "orange",
        {
            "de": ("Mira: nachfragen", "Mira schaltet das erst, nachdem du es bestätigt hast."),
            "en": ("Mira: confirm first", "Mira only switches this after you confirmed."),
        },
    ),
    LABEL_ONCE: (
        "mira_once",
        "mdi:timer-sand",
        "blue",
        {
            "de": ("Mira: einmalig", "Läuft einmal und räumt sich dann auf (Schlaf-Timer)."),
            "en": ("Mira: once", "Runs once and then removes itself (sleep timer)."),
        },
    ),
}


@callback
def ensure_label(hass: HomeAssistant, label_id: str) -> lr.LabelEntry:
    """The label with this id, created in Home Assistant's language if it
    is missing. An existing one is left as the user named it."""
    labels = lr.async_get(hass)
    if (label := labels.async_get_label(label_id)) is not None:
        return label
    seed, icon, color, texts = _LABELS[label_id]
    label = labels.async_create(seed, icon=icon, color=color)
    name, description = texts.get((hass.config.language or "en")[:2], texts["en"])
    return labels.async_update(label.label_id, name=name, description=description)


@callback
def ensure_labels(hass: HomeAssistant) -> None:
    for label_id in _LABELS:
        ensure_label(hass, label_id)
