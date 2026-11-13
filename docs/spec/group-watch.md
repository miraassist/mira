# Group watch – Spec

Status 2026-11-13: not specified yet; the next spec after the [Device watch](device-watch.md). What is decided already
is kept here.

## Idea

A **group watch** watches a group of sensors against a rule for a situation the user says: all lights but the
garden's, nobody home, at night, when leaving. A member that does not follow the rule makes an entry of a message.
The checks at occasions ("Ist alles zu?" when leaving or going to bed) are the same kind, checked once instead of all
the time. It shares the ground of the device watch: blueprints as blueprints, drafting through the skill `aufgabe`, a
watch is a task, its entry a message's. Not part of it for now: cameras (decided 2026-11-13).

## Decided already (2026-11-13, in the device watch's spec)

| # | Question | Decision |
|---|---|---|
| 1 | Which lights for "left on" | **All lights, with exceptions.** Mira proposes all lights except those she takes for outside lighting by their area or name; the user corrects by voice ("die Terrasse nicht"); the exceptions are an input of the blueprint. A light added later is watched without asking |
| 2 | Pausing, e.g. on holiday | A presence simulation switches lights on on purpose. The blueprint "left on" gets an input "pause while": an entity of the house and its state (a holiday helper, the simulation's switch, an alarm panel in `armed_vacation`). When Mira finds such an entity while making the watch, she asks whether to use it; if not, the watch is switched off and on by voice or in the companion app, as every task (`automation.turn_off/on`, [Tasks](tasks.md)). The water watches and the daily routine do not pause: on holiday they matter most |
| 3 | How "all lights" becomes a trigger | A **template trigger over all lights** minus the exceptions and groups: a new light is watched at once. It fires once per streak; a second light crossing the hours while the first is still on is not said on its own (the entry names every light on that long). The exceptions must hold: an excluded light (the garden light) never makes or joins an entry |

## The blueprint "lights left on", as far as it was specified

### Lights left on (`left_on`)

- **Inputs:** `exclude` (lights, several allowed; the outside lighting), `hours` (default 3), `only_away` (default
  off: only when nobody is home, by the zone `zone.home`'s count 0), `pause_entity` (optional, any entity) and
  `pause_state` (text, default `on`).
- **Trigger:** a template trigger over `states.light` without `exclude` and without groups: true while one of them
  is `on` for `hours` (decision 3). Rendered on every change of a light and once a minute.
- **Condition:** `pause_entity` is not in `pause_state`. A paused watch says nothing, also not afterwards for what
  was on during the pause. A `pause_entity` that is gone counts as not paused (Home Assistant's state condition is
  false for an unknown entity): the watch speaks again rather than staying silent unnoticed.
- **Entry:** "{names} ist/sind seit {hours} Stunden an." The follow-up "mach's aus" is a normal turn.
- Light groups (a light with the attribute `entity_id`) are not counted: their members are, and the group would say
  the same light twice.
- Switches and fans are not part of it for now; a wish for them is drafted freely as today (decision 2).


## Rules to become scenarios

- All lights except the outside lighting are proposed; "die Küche auch nicht" adds an exception.
- A holiday or presence-simulation entity found: Mira asks whether the watch pauses while it is on.
- A paused watch says nothing.
- An excluded light (the garden light) never makes or joins an entry.
- A light left on, and "mach's aus" in the same conversation switches it off.
