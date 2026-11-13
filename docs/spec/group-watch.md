# Group watch – Spec

Status 2026-11-13: specified (decisions 1 to 19), not built; it builds on the [Device watch](device-watch.md) (its
steps 1 and 2 first). Scenarios: [group-watch.feature](group-watch.feature).

## Goal

A **group watch** watches a **watch group** of sensors against a rule for a situation the user says: lights left on,
a window open while nobody is home, motion at night in the garden, the cellar too cold. A member that does not follow
the rule makes an entry of a message. "Ist alles zu?" checks a watch group once, on request or called from an
automation of the household.

- "Sag Bescheid, wenn irgendwo Licht ewig an ist." – "Mach ich, ab drei Stunden, alle Lampen außer Terrasse und
  Garten. Passt das?"
- "Gib mir Bescheid, wenn ein Fenster offen ist und keiner da ist." – "Mach ich: alle Fenster und Türen. Ich schick's
  euch aufs Handy."
- "Melde Bewegung im Garten nachts." – "Von wann bis wann ist nachts?" – "23 bis 6." – "Mach ich."
- "Ist alles zu?" – "Bis auf das Fenster im Bad ist alles zu."

The difference to the device watch: there one device is held against its own normal, learned from its history; here
a group is held against a rule the user says. The ground is the same: Mira's blueprints, drafting through the skill
`aufgabe`, a watch is a task, its entry a message's.

Not here: cameras (for now), alarms (smoke, gas, leak: the devices or a coupled system do that), a battery or
availability watch (later, decision 19).

## Decisions (2026-11-13)

| # | Question | Decision |
|---|---|---|
| 1 | Which lights for "left on" | **All lights, with exceptions.** Mira proposes all lights except those she takes for outside lighting by their area or name; the user corrects by voice ("die Terrasse nicht"). An exception is the label (decision 5), not an input of the blueprint |
| 2 | Pausing, e.g. on holiday | No input "pause while": see decision 11 |
| 3 | How "all lights" becomes a trigger | A **template trigger over the watch group**: a new member is watched at once. It fires once per streak; a second member crossing the rule while the first still does is not said on its own (the entry names every member that breaks the rule when it fires). A member with the label never makes or joins an entry |
| 4 | Where the group comes from | A **watch group** is a selection over what Home Assistant knows, held as the blueprint's inputs: by **device class** (domain and device class), by **area**, by **floor**, the three combinable ("alle Fenster im Dachgeschoss"); or a **group helper** with its fixed members. A device of a fitting class added later is in at once. Group helpers do not count as members of a selection (their members are in already). Checked on the maintainer's house (read only, 2026-11-13): 22 windows, 17 doors, 4 openings, 11 motion, 7 occupancy, 54 lights; the 62 binary sensors without a device class are none of these (inverter relays, Echo states, thermostats) |
| 5 | Exceptions | The label **"Mira: nicht überwachen"** ("Mira: don't watch"; in the house's language, as Mira's other labels) on an entity takes it out of every selection; the user sets it in Home Assistant, or Mira sets it on the user's word. Not for a group helper, whose members the user chooses |
| 6 | A declined offer and the label | Kept apart: a declined offer of the device watch stays in Mira's database ([Device watch](device-watch.md), decision 21); the label is a deliberate exception, visible in Home Assistant |
| 7 | Exceptions per watch | None for now: the label is the only exception. "Die Terrasse nicht" sets the label on the terrace light, which then is in no group watch |
| 8 | Watch groups at the start | Lights (`light`); openings: windows, doors, openings, garage doors (`binary_sensor` of `window`, `door`, `opening`, `garage_door`); motion: `binary_sensor` of `motion`, `occupancy`, `presence`; temperatures: `sensor` of `temperature` (decision 18) |
| 9 | Nobody home | The zone `zone.home` counts 0 persons: Home Assistant's own signal |
| 10 | Night | A window of time per watch, said when it is made ("nachts, also von 23 bis 6 Uhr") |
| 11 | Pausing | No automatism: a watch is switched off and on as every task (by voice, in the companion app, or by the household's own script, e.g. "Wir sind im Urlaub") |
| 12 | Receiver while nobody is home | Every person, privately over their channels ([Channels](channels.md)); a satellite helps nobody then. How: see Open |
| 13 | "Ist alles zu?" on request | Computed over the same watch group as the watches (the label included), so a watch and the answer never disagree |
| 14 | Checks at occasions | None by themselves for now (not on leaving, not at bedtime). The check is easy to trigger from Home Assistant (decision 16) |
| 15 | Rules at the start | Each a blueprint: **lights left on**, **open while nobody is home**, **open at night**, **open too long**, **motion while nobody is home**, **motion at night**, and **too cold or too warm** (decision 18) |
| 16 | Triggering a check from Home Assistant | An action of the integration, **`mira.check`**: an automation or a script calls it, the service computes the check and puts an entry for the receiver; the result also comes back as the action's response. "Ist alles zu?" in a conversation takes the same way in the code. A new action and route, optional at the interface: an older service answers 404, and the action says so |
| 17 | What "Ist alles zu?" checks | The openings (decision 8), the label included |
| 18 | Temperatures | A group watch too: `sensor` of `temperature`, by area or floor; the rule "below or above a value for minutes" ("Keller unter 5 Grad"). Sensors that do not measure a room (a freezer's, a radiator's) are left out by the label or by choosing areas |
| 19 | Later | A battery and availability watch (a smoke detector not reachable, a battery low) as a later extension, never as an alarm. Not here |

## Who does what

| Step | Blueprint / code (fixed, checked) | Model (at run time) |
|---|---|---|
| Which members | The selection in the blueprint (device class, area, floor or group helper, minus the label), computed by Home Assistant's templates | turns the user's words into the selection ("alle Fenster im Dachgeschoss"), proposes the outside lights as exceptions by area and name |
| An exception | Mira's tool sets the label on the entity (Home Assistant's entity registry) after the user's yes | hears "die Terrasse nicht", names the entity |
| The rule's numbers | The blueprint's inputs, bounded by their selectors | hours, minutes, the night window, a temperature from the user's words |
| Creating, changing, deleting | The task's way as in the device watch | proposes in words, takes the yes |
| Telling | The automation calls `mira.send_message` with the members that break the rule | words the entry when it is read out; "mach's aus", "mach's zu" is the next turn |
| "Ist alles zu?" | The code has Home Assistant render the same selection and answers with the open members | says it ("Bis auf das Fenster im Bad ist alles zu") |

## Building blocks

| Building block | Used for | Change | Reason |
|---|---|---|---|
| Mira's blueprints (`internal/watch`, from the [Device watch](device-watch.md)) | The seven rules as blueprints; made ready, versioned and replaced as there | Seven more blueprints, one shared selection | The same building block; a group watch is only another kind of blueprint |
| Tasks, drafting (skill `aufgabe`, from the device watch) | A group watch is a task made from a blueprint | The catalog names the selection's inputs; the skill's prose says how a selection is said | One way to make a task |
| Saving (`automation.Save`, `own_automations`) | Saves the automation from the blueprint | None | As in the device watch |
| `mira/automation/validate` (integration) | Checks the rendered automation | None | As in the device watch |
| `mira.send_message` (integration) | The blueprints' action | None, or one receiver value (Open 1) | Already there for tasks |
| Messages, kind `simple` | A watch's entry | None | As in the device watch |
| Mira's labels (`custom_components/mira/labels.py`) | The label "Mira: nicht überwachen", created by the integration under a fixed id (`mira_dont_watch`) in Home Assistant's language, as Mira's other labels | One more label | Labels are created there, known by their ids, never their names |
| Home Assistant's entity registry (`config/entity_registry/update`, admin) | Setting the label on an entity on the user's word | None (an existing command) | Home Assistant's own place for a label |
| Home Assistant's templates (`render_template`, one subscription answered once) | "Ist alles zu?": the same selection, rendered by Home Assistant | None | The check and the watch compute the group the same way; nothing fetches every state to filter here |
| **New: the action `mira.check`** (integration) and the route `POST /v1/check` (service) | Triggering a check from an automation or a script (decision 16) | New action, new route; the fields optional both ways | Home Assistant's way to call Mira; an older service answers 404 |
| **New: the tool `house_check`** (service) | "Ist alles zu?" in a conversation, on the same code as `/v1/check` | New tool | The model must not compute the group itself (decision 13) |
| Fake devices (test HA) | Floors; windows, doors, motion and temperature sensors in areas with floors; persons home and away | Extended | The test HA has windows and motion sensors, but no floors, no temperature sensors in rooms and no way to empty `zone.home` on purpose |

## The blueprints

All are `domain: automation` under `mira/` of Home Assistant, made ready and replaced as in the device watch (its
section D). Each entry names the members that break the rule (their names), goes to the house, or while nobody is home
to every person (Open 1), with `dedupe_key: watch:<automation id>` and `expires_in: 12h`.

### The selection (shared by all)

- **Inputs:** `areas` (areas, optional), `floors` (floors, optional), `group` (a group helper, optional). The device
  classes are fixed by each blueprint. The label id is fixed (`mira_dont_watch`).
- **Members:** with `group`: its members (`expand`). Otherwise the entities of the blueprint's domain whose
  `device_class` is one of its classes, in one of `areas` or on one of `floors` if given, without the entities carrying
  the label (`label_entities('mira_dont_watch')`) and without group helpers (an entity with the attribute `entity_id`).
- **Template:** one macro used by every blueprint's trigger and by the check. Home Assistant renders a trigger's
  template on every change of an entity of the domain it reads, and once a minute where it uses `now()`.

### 1. Lights left on (`left_on`)

- **Inputs:** the selection over `light`, `hours` (default 3), `only_away` (default off).
- **Trigger:** a member `on` for `hours` (by `last_changed`), and with `only_away` `zone.home` at 0.
- **Entry:** "{names} ist/sind seit {hours} Stunden an." Importance 1.

### 2. Open while nobody is home (`open_while_away`)

- **Inputs:** the selection over the openings, `delay` (minutes, default 5).
- **Trigger:** `zone.home` at 0 and a member `on`, held for `delay`: the house becomes empty with a window open, or a
  window opens while it is empty.
- **Entry:** "Niemand ist zu Hause, aber {names} ist/sind offen." Importance 2.

### 3. Open at night (`open_at_night`)

- **Inputs:** the selection over the openings, `night_start`, `night_end` (times), `delay` (minutes, default 10).
- **Trigger:** within the night window and a member `on`, held for `delay`.
- **Entry:** "Es ist {time} und {names} ist/sind noch offen." Importance 1.

### 4. Open too long (`open_too_long`)

- **Inputs:** the selection over the openings, `minutes` (default 30).
- **Trigger:** a member `on` for `minutes`.
- **Entry:** "{names} ist/sind seit {minutes} Minuten offen." Importance 1.

### 5. Motion while nobody is home (`motion_while_away`)

- **Inputs:** the selection over the motion classes.
- **Trigger:** `zone.home` at 0 and a member `on`.
- **Entry:** "Bewegung bei {names}, obwohl niemand zu Hause ist." Importance 2. A pet or a robot vacuum: the label on
  the sensor, or the watch switched off while it runs (decision 11).

### 6. Motion at night (`motion_at_night`)

- **Inputs:** the selection over the motion classes, `night_start`, `night_end`.
- **Trigger:** within the night window and a member `on`.
- **Entry:** "Bewegung bei {names} um {time}." Importance 1.

### 7. Too cold or too warm (`temperature_out_of_range`)

- **Inputs:** the selection over `sensor` of `temperature`, `below` and/or `above` (°C), `minutes` (default 30).
- **Trigger:** a member below `below` or above `above`, held for `minutes`; a member `unavailable` or `unknown` does
  not count.
- **Entry:** "{names}: {value} °C, {unter/über} {limit} °C." Importance 1.

Once per streak for all (decision 3): the trigger fires again only after no member broke the rule.

## What it does, step by step

### A. Making a group watch (in conversation)

1. The skill `aufgabe` collects the wish as today; the drafting gets the catalog with the group blueprints (from the
   device watch, its step A2 makes them ready).
2. **The selection:** the model says the device classes' blueprint, the areas, floors or the group helper from the
   user's words. For lights it proposes the outside lights (by area and name) as exceptions.
3. **An exception:** on the user's word ("die Terrasse nicht") and yes, the tool sets the label on the entity
   (`config/entity_registry/update` with the label added to its labels). It then is in no group watch (decision 7);
   the proposal says so.
4. **Check and proposal:** `blueprint/substitute`, `mira/automation/validate`, the proposal in words from the rendered
   automation, with the members the selection gives today (rendered once by Home Assistant): "alle 22 Fenster und 17
   Türen außer Gartentür".
5. On yes: saved as a task, as in the device watch.

### B. Telling

1. Home Assistant's trigger fires; the automation calls `mira.send_message` with the members' names.
2. The entry goes to the house, or while `zone.home` is at 0 to every person privately (Open 1).
3. Read out, the model words it; "mach's aus", "mach's zu" (where a member can be switched) is the next turn.

### C. "Ist alles zu?" and `mira.check`

1. **In a conversation:** the tool `house_check` (scope: the openings; optionally areas or floors) has Home Assistant
   render the selection once (`render_template`, its first answer, then the subscription ends) and returns the open
   members with their areas. The model says them.
2. **From Home Assistant:** an automation calls `mira.check` (fields: `scope`, default the openings; `areas`, `floors`
   optional; `receiver` optional; `notify`, default true). The integration posts `/v1/check`; the service computes as
   in 1, puts an entry for the receiver when something is open and `notify` is on, and answers the list, which the
   action returns as its response (`response_variable`).
3. Nothing open: no entry; the response's list is empty.

### D. Changing, switching off, deleting

As every task, as in the device watch. The label stays when a watch is deleted: it is the user's exception in Home
Assistant, removed there or on the user's word ("überwach die Terrasse wieder").

## What it costs

- **Per watch in Home Assistant:** one automation with a template trigger. Home Assistant renders it on every change
  of an entity of the domain it reads (`light`, `binary_sensor`, `sensor`), and once a minute where it uses `now()`.
  Grows with the number of entities of that domain in the house (the maintainer's: about 60 lights, about 250 binary
  sensors, many more sensors; assumption: a template over the sensors' states is cheap enough, measured on the test HA
  before the temperature blueprint is built, row 9 of the checks).
- **Making a watch:** as in the device watch; one more render of the selection for the proposal (milliseconds); one
  `config/entity_registry/update` per exception.
- **"Ist alles zu?" and `mira.check`** (the turn waits): one `render_template` subscription, answered at once and
  ended; milliseconds. For `mira.check` one HTTP request from the integration to the service, and an entry if
  something is open.
- **An entry:** one `mira.send_message`, as every task's.

## Retries

As in the device watch (its decision 17): a lost connection, a timeout, a 429 and a 5xx are tried again, at most 3
times, with an exponential wait from 1 s; anything else ends at once with its trace. This holds for the label's
update, `render_template` and the blueprints' calls. `mira.check` is not retried by the integration: the automation's
trace shows the error.

## Errors

| Step | Fails | Who learns of it |
|---|---|---|
| A2 | The user names something no selection gives ("die Fenster im Turm", no such floor) | The model says what there is ("Es gibt Keller-, Erd- und Dachgeschoss"); nothing is drafted |
| A3 | Setting the label is refused (not an admin's token) or the label does not exist (an older integration) | The tool's error with Home Assistant's answer; the model says the exception could not be set and the watch is not proposed with it; a log line with the entity |
| A4 | The selection gives no member | The proposal says so ("Im Dachgeschoss gibt es keine Fenster mit Sensor"); nothing is saved |
| A4, A5 | As in the device watch | As there |
| B1 | `mira.send_message` fails | Home Assistant's automation trace; the integration logs it, as today |
| C1 | `render_template` fails or does not answer within 5 s | The tool's error with Home Assistant's answer; the model says it could not check; a log line with the template's start |
| C2 | `/v1/check` is missing (an older service) | The action raises an error: "Mira kann das erst nach einem Update"; the automation's trace shows it |
| C2 | The service cannot render | The action's error with the service's answer; the trace shows it |
| — | A member is `unavailable` or `unknown` | It does not count as breaking a rule; "Ist alles zu?" names it as not readable ("Das Fenster im Bad meldet sich nicht") |

## Checks on the test HA

| # | Transition | Trigger | Signal | Checked |
|---|---|---|---|---|
| 1 | The selection as a template | `label_entities()`, `floors()`, `floor_entities()`, a filter by `attributes.device_class` | the fakes' windows and motion sensors | 2026-11-13, 2026.11.1 (no floors yet) |
| 2 | A floor and an area narrow the selection | fake windows on two floors | only the floor's | open |
| 3 | The label takes a member out, at once | `config/entity_registry/update` with the label | the template's result without it; a running watch does not fire for it | open |
| 4 | `zone.home` goes to 0 and back | the fake persons leave and come | the zone's state 0, then 1 | open |
| 5 | Open while nobody is home | a fake window open, the persons leave | the trace fires after `delay` | open |
| 6 | Open at night, motion at night | a fake window open or motion in the (shortened) night window | the trace fires | open |
| 7 | Open too long, lights left on | a fake window or light on for the (shortened) time | the trace fires once per streak | open |
| 8 | Motion while nobody is home | fake motion with nobody home | the trace fires | open |
| 9 | Too cold | a fake temperature below the value for the (shortened) time; how long the template takes over all sensors | the trace fires; the render time | open |
| 10 | `render_template` once | the check's template | the first answer, then the subscription ends | open |
| 11 | `mira.check` from a script | a script with `response_variable` | the list in the response, the entry | open |

Each row is checked before the step that builds on it.

## Steps

1. **The selection and the blueprints:** the shared macro, the seven blueprints embedded with the device watch's;
   rows 1 to 3.
2. **Drafting a group watch:** the catalog's selection inputs, the label tool, the proposal with today's members; the
   skill's prose; the label in `labels.py`.
3. **The fakes and the runs:** floors, openings, motion, temperatures, persons home and away; rows 4 to 9; each
   blueprint end to end (entry, read out, follow-up).
4. **"Ist alles zu?":** the tool `house_check`, the route `/v1/check`, the action `mira.check`; rows 10 and 11.
5. **The guide**, German and English: group watches, the label, "Ist alles zu?", `mira.check` in an automation.

## Open

1. **How an entry reaches every person while nobody is home** (decision 12): `mira.send_message` has one receiver
   (the house or a person), and the blueprint does not know the persons.
   - a new receiver value `persons` (every person with a channel, privately); a new value at the interface, an
     older service refuses it with its error;
   - or a rule of the notifier: an entry for the house while `zone.home` is at 0 goes privately to every person
     (for every message of the house, not only a watch's).
