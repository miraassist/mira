"""The fake house of the group watch (docs/spec/fake-devices.md, "The watched group").

Floors and areas, and the entities a group watch watches, each in its area: windows and doors,
motion, temperatures and lights, plus a phone per person (a notify entity that keeps what it was sent).
The floors and areas are made when missing; an area that has a floor keeps it, an entity that has an
area keeps it (the user may move them in the test HA).
"""

from __future__ import annotations

from homeassistant.core import HomeAssistant
from homeassistant.helpers import area_registry as ar
from homeassistant.helpers import entity_registry as er
from homeassistant.helpers import floor_registry as fr

# Floors with their level, and the areas on them; areas without a floor are outside.
FLOORS = {
    "Kellergeschoss": (-1, ["Keller"]),
    "Erdgeschoss": (0, ["Wohnzimmer", "Küche", "Flur", "Bad", "Garage"]),
    "Dachgeschoss": (1, ["Schlafzimmer", "Büro"]),
}
OUTSIDE = ["Garten", "Terrasse"]

# (name, unique id, device class, area)
OPENINGS = [
    ("Fake Fenster Wohnzimmer", "watch_fake_window_living", "window", "Wohnzimmer"),
    ("Fake Fenster Küche", "watch_fake_window_kitchen", "window", "Küche"),
    ("Fake Fenster Schlafzimmer", "watch_fake_window_bedroom", "window", "Schlafzimmer"),
    ("Fake Fenster Büro", "watch_fake_window_office", "window", "Büro"),
    ("Fake Kellerfenster", "watch_fake_window_cellar", "window", "Keller"),
    ("Fake Haustür", "watch_fake_door_front", "door", "Flur"),
    ("Fake Gartentür", "watch_fake_door_garden", "door", "Wohnzimmer"),
    ("Fake Garagentor", "watch_fake_garage_door", "garage_door", "Garage"),
]
MOTION = [
    ("Fake Bewegung Garten", "watch_fake_motion_garden", "motion", "Garten"),
    ("Fake Präsenz Büro", "watch_fake_presence_office", "presence", "Büro"),
]
# (name, unique id, area, °C)
TEMPERATURES = [
    ("Fake Temperatur Keller", "watch_fake_temperature_cellar", "Keller", 12.0),
    ("Fake Temperatur Wohnzimmer", "watch_fake_temperature_living", "Wohnzimmer", 21.0),
]
# (name, unique id, area)
LIGHTS = [
    ("Fake Licht Küche", "watch_fake_light_kitchen", "Küche"),
    ("Fake Licht Schlafzimmer", "watch_fake_light_bedroom", "Schlafzimmer"),
    ("Fake Licht Terrasse", "watch_fake_light_terrace", "Terrasse"),
    ("Fake Licht Garten", "watch_fake_light_garden", "Garten"),
]
# (name, unique id): sensor.fake_handy_anna keeps what notify.fake_handy_anna was sent.
PHONES = [("Fake Handy Anna", "fake_handy_anna"), ("Fake Handy Ben", "fake_handy_ben")]


def ensure_structure(hass: HomeAssistant) -> None:
    """The floors and areas, made when missing; an area without a floor gets its floor."""
    floors, areas = fr.async_get(hass), ar.async_get(hass)
    for name, (level, on_it) in FLOORS.items():
        floor = floors.async_get_floor_by_name(name) or floors.async_create(name, level=level)
        for area_name in on_it:
            area = areas.async_get_area_by_name(area_name) or areas.async_create(area_name)
            if area.floor_id is None:
                areas.async_update(area.id, floor_id=floor.floor_id)
    for area_name in OUTSIDE:
        if areas.async_get_area_by_name(area_name) is None:
            areas.async_create(area_name)


def place(hass: HomeAssistant, entity_id: str, area_name: str) -> None:
    """Put an entity into its area unless it has one."""
    entities = er.async_get(hass)
    entry = entities.async_get(entity_id)
    if entry is None or entry.area_id is not None:
        return
    area = ar.async_get(hass).async_get_area_by_name(area_name)
    if area is not None:
        entities.async_update_entity(entity_id, area_id=area.id)
