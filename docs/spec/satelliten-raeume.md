# Satellites and rooms – spec

Status 2026-10-31: steps 1–4 built, not released. Scenarios: [satelliten-raeume.feature](satelliten-raeume.feature).
Builds on [Mira in Home Assistant](ha-anbindung.md), "Rooms are satellites"; it replaces the rule "one satellite per
area" there.

## Goal

Where a satellite stands is Home Assistant's: its area. Two setups fall through today without a word:

- **A satellite without an area** can be talked to, but everything bound to a room does nothing there: no
  announcements, no morning announcement, no ring, no screen hint, no sleep timer or radio without naming a room,
  and a message left there has the sender "?". It is missing in the integration's options and on Mira's page, and
  nothing says why. Only the model is told "the room is unknown".
- **A second satellite in an area** (a large room with two on purpose) is left out: it answers, but Mira never
  speaks there on her own, its ring shows nothing, and its settings are refused.

Both become visible, and a room may have several satellites.

## Decided (2026-10-31)

| # | Question | Decision |
|---|---|---|
| 1 | How a satellite without an area is reported | A repair in Home Assistant per satellite, and Mira's page "Satellites" lists it greyed with the reason |
| 2 | Several satellites in an area | Built: a room has one or more satellites |
| 3 | Which satellite speaks where it is not clear | The room's **primary satellite** |
| 4 | How a room gets its primary | The integration marks one as soon as a room has two, stored on the satellite; a switch "Main satellite in the room" changes it. There is always exactly one, visible |
| 5 | Who speaks | By the occasion: what was asked for at a satellite comes there; the morning announcement where the motion sensor fired; everything else at the primary |
| 6 | Rings and screens | All of the room's show what waits |
| 7 | Urgent announcements "everywhere" | One satellite per room, the primary |
| 8 | The satellite's area in Mira | As in the integration: the entity's area, else its device's (before, Mira took only the device's) |
| 9 | Mira without the integration | Not a case any more; `satellites` in `mira.yaml` is not extended |

## What it does

### A satellite without an area

- **A repair** in Home Assistant (Settings → Repairs), one per Assist satellite that is on and has no area:
  "Mira does not know where ‹name› stands". The text says what is missing there (announcements, the ring, timers
  and radio without naming a room) and the next click: give the device an area. It links to the device's page.
  It goes away by itself once the satellite has an area, is switched off or removed.
- **Mira's page "Satellites"** (with the integration) lists it greyed, with "No area: give it an area in Home
  Assistant". It cannot be opened; there is nothing to set before it has a room.
- **Raised and cleared by the integration**, which has the registries and already listens to their changes
  (`entity_registry_updated`, `device_registry_updated`, debounced): on every change it compares the satellites
  without an area with the repairs it has open. Cost: one pass over the entity registry in memory per burst of
  changes, no call to Home Assistant or to the Mira service.
- **The service's area of a turn** is the satellite entity's area, else its device's, like the integration's
  (decision 8). Before, a satellite whose entity had an area and its device none got announcements, but its turns
  had no room.

### Several satellites in a room

- **A room** is an area with one or more satellites (Assist satellites, and speakers with settings, as before).
  Every satellite keeps its own settings: motion sensors, morning announcement, indicator light.
- **The primary satellite.** In a room with one satellite it is that one. When a room has several and none of
  them is marked (a second came into the area, or the primary was switched off, removed or moved away), the
  integration marks the one that was the room's satellite before (it remembers the last one per area while Home
  Assistant runs; after a restart, the oldest in the entity registry, `created_at`), as a setting on it
  (`primary: <area>`: the mark holds only in that area, so a primary moved to another room does not bring it
  along), so it shows in its options. The switch "Main satellite in the room" in a
  satellite's options makes it the primary and takes the mark from the other one. Mira never guesses.
- **Who speaks** (decision 5):

  | Occasion | Satellite |
  |---|---|
  | something asked for at a satellite and delivered later: a reminder "here", the answer of a thread or of a helper that worked in the background | the satellite it was asked at, if it is still in that room; else the primary |
  | the morning announcement, someone comes in | the satellite whose motion sensor fired; a sensor set on two satellites: the primary |
  | an answer and its afterword in a turn | the satellite of the turn (as before, Home Assistant speaks it) |
  | everything else: a message to the room, from Telegram, a task, an urgent announcement | the primary |

- **Rings and screens:** each satellite's indicator (its ring, found, or the light set) and each screen in the room
  shows what waits for the room, and glows while Mira speaks in it. They act as one: the room's state, shown on
  every one of them. A satellite whose own ring is one of the room's indicators gets no screen hint on top (as
  before: a TECHO5 Echo offers the overlay action without a screen); one with a screen and without its own ring
  gets the hint, also in a room whose other satellite has a ring.
- **Urgent announcements** go to every room once, at its primary.
- **Settings of a satellite that is not primary** count as before: its motion sensors trigger the morning
  announcement of the room, at it; its indicator shows the room's state.

### The interface between the integration and the service

New fields are optional both ways:

- A room as the integration sends it (`PUT /v1/ui/ha`) keeps `satellite`, `presence`, `morning` and `indicator`
  for the **primary**, so an older service sees exactly the room it saw before: one satellite, the primary.
- New: `others`, the room's other satellites, each `{satellite, presence, indicator, morning}`. An older service
  ignores it. An older integration does not send it: one satellite per room, as before.
- The service turns a room into its config: `satellites: {area: [primary, others…]}` (one entity or a list, the
  first is the primary), `notifier.indicators: {area: [lights…]}` (one or a list), and for the motion sensors of
  the others `notifier.morning_trigger.speak_at: {sensor: satellite}`. The morning trigger's `presence` per area
  holds all sensors of the room; the room announces on entry if one of its satellites has "morning".
- `GET /v1/ui/ha` gives the rooms back with `others`, for the page.

### What a delivery remembers

To speak where something was asked, Mira keeps the satellite's device with what she delivers later:

- a scheduled message (a reminder) and a message left at a satellite: `origin_device`, beside `origin_area`;
- a thread: the device of the turn it began in, beside its area;
- a helper's delivery (`Delivery`): the device beside the area.

A device that is no longer a satellite in that room (moved, switched off) falls back to the room's primary; that
is the rule of the table above, not a guess.

## Costs

- **Integration:** on registry changes, one pass over the entity registry in memory (as today for the rooms), and
  creating or deleting a repair where the set changed. Setting the primary mark writes one entity option, only
  when a room has none.
- **Service:** nothing new is read from Home Assistant. Choosing a satellite is a lookup in the room's list; each
  ring in a room is one `light.turn_on`/`turn_off` call, as each room's ring is today, so a room with two rings
  costs two calls per change.

## Steps

1. **Integration: a satellite without an area is visible.** The repair, raised and cleared on registry changes;
   the page lists it greyed. Scenarios SR-01 to SR-04, tested in pytest.
2. **Service: the turn's area as the integration finds it** (decision 8). Scenario SR-05.
3. **Integration: several satellites per room.** The primary mark and its switch, `others` in the rooms, the
   options and the page without "both are set up for area". Scenarios SR-10 to SR-14, tested in pytest.
4. **Service: several satellites per room.** The rooms with `others`, the choice of the satellite per occasion,
   every ring and screen of a room, the device kept with deliveries. Scenarios SR-20 to SR-29.
