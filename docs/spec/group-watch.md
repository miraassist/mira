# Group watch – Spec

Status 2026-11-13: specified (decisions 1 to 20); steps 1 to 5 built 2026-11-13 (decisions 21 and on taken while building,
to be confirmed by the maintainer), not released. The battery watch and the availability watch (decisions 36 to 47,
taken without the maintainer, to be confirmed) specified 2026-11-13, steps 6 to 8 not built yet. It builds on the
[Device watch](device-watch.md). Scenarios: [group-watch.feature](group-watch.feature).

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

Two more group watches look after the house's devices themselves (decision 19, specified as decisions 36 to 47): the
**battery watch** tells the batteries that run low, the **availability watch** the devices Home Assistant cannot
reach. Both look once a day and name every member that breaks the rule then (decision 37); neither is an alarm.

- "Sag mir, wenn eine Batterie leer wird." – "Mach ich: Ich schau jeden Morgen um 9 nach den 12 Batterien, die Handys
  und das Auto nicht, und sag dir, welche unter 20 Prozent sind. Schon jetzt schwach: Rauchmelder Flur (12 %). Passt
  das?"
- "Sag Bescheid, wenn ein Gerät nicht mehr erreichbar ist." – "Mach ich: Jeden Morgen um 9 sag ich dir, welche der 40
  Geräte seit mindestens einer Stunde nicht erreichbar sind. Schon jetzt nicht erreichbar: Steckdose Garage."

The availability watch is the deliberate counterpart of decision 23: in every other group watch `unavailable` and
`unknown` never break a rule; here `unavailable` is the rule.

Not here: cameras (for now), alarms (smoke, gas, leak: the devices or a coupled system do that). "Wie geht's dem
Haus?" is a later spec of its own that builds on the watches; "Ist alles zu?" (C) takes neither new kind (decision 46).

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
| 19 | Later | A **battery watch** (a battery low) and an **availability watch** (a sensor not reachable, a radio problem, a value unchanged for long) are further group watches, specified later as an extension of this spec; never as an alarm. "Wie geht's dem Haus?" comes after them, a spec of its own that builds on the watches |
| 20 | How an entry reaches every person while nobody is home (decision 12) | A new receiver value **`persons`** of `mira.send_message`: every person with a channel gets the entry privately. New at the interface: an older service refuses the value with its error (the automation's trace shows it), an older integration passes the text on unchanged |
| 21 | One macro for the selection (The selection) | **One template text**, `internal/watch/blueprints/selection.jinja`, copied word for word into every group blueprint twice (its trigger and the variable `breaking`, the members that break the rule, which the entry names) and rendered with the same variables by the check; a Go test keeps the copies identical. Reason: Home Assistant's own way to share a macro (`custom_templates/`) cannot be written over its WebSocket API, and a blueprint's `trigger_variables` take no template over states. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 22 | Areas and floors together; who is a member | Areas and floors together are a **union**: everything in the areas and on the floors (an entity's area is its own or its device's). A member is any entity Home Assistant has of the domain and class, not only the ones exposed to Assist (decision 4: a selection over what Home Assistant knows). Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 23 | What breaks a rule | Only `on` (lights, openings, motion) and a number (temperatures): `unavailable`, `unknown` never do (Errors, last row). The entry names the members that break the rule when the automation runs (the variable `breaking`, computed again); a condition ends the run without an entry when none is left (a window closed between the trigger and the run). Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 24 | Receiver of every group blueprint | `persons` while `zone.home` is 0 (decision 20), else the house: for all seven blueprints, as "The blueprints" says; `zone.home` not a number counts as somebody home. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 25 | The inputs' bounds | `hours` 0.05 to 72 (step 0.05; the lower bound lets the test HA run it in minutes), `delay` 0 to 120 minutes, `minutes` 1 to 1440, `below` and `above` −40 to 60 °C, both optional (empty), at least one given and `below` under `above` (the code's check); `night_start`, `night_end` times without default. `group` is an entity of the blueprint's domain; the code checks it is a group helper (an entity with the attribute `entity_id`) of that domain, and not given with areas or floors. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 26 | Indicator lights | The satellites' indicator lights (an LED ring, a screen) are lights too and in "all lights" (found on the test HA: `light.*_led_ring`, `light.show_screen`); the planner proposes them as exceptions as it does the outside lights (a ring lit for hours while messages wait would otherwise be told as left on). Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 27 | Home Assistant and an unknown label | `config/entity_registry/update` takes a label id that does not exist without a word (checked on the test HA, 2026-11-13): before it sets the label, the code reads the label registry, and a missing label (an older integration) is the error of A3. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 28 | When an exception gets its label (A3) | **On the yes to the proposal**, not before it: the planner names the exceptions in its draft (`exclude`, the ids; for lights it proposes the outside lights and the indicators when the user says nothing about them), the proposal names them ("Ausgenommen: …; bekommt das Label „Mira: nicht überwachen“ und ist dann in keinem Wächter mehr") and the members left today, and the yes sets each label (A3's command) before the watch is saved; one label that cannot be set ends it: nothing is saved, the user hears which exception and Home Assistant's answer. Reason: one yes for the watch and its exceptions, and the planner, which sees the members, picks the entities, not the conversation. Outside a drafting ("Überwach die Terrasse nicht mehr", "… wieder", D) the tool **`watch_exception`** (skill `aufgabe`, entity by id or by the words of its name among the members of every scope, labelled ones included) sets or removes the label. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 29 | How the conversation hands a watch group to the drafting | The field `watch` of `aufgabe` holds it in words, written "Gruppe: …" ("Gruppe: Fenster und Türen im Dachgeschoss"); the field check `entities` takes such a value as it is, and the drafting checks the selection against Home Assistant (A2). With such a value (or for a change of a group watch) the planner gets the house's floors with their areas, the group helpers and the members of each scope over the whole house (one render each, A4's costs); otherwise only the catalog of the blueprints. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 30 | A selection without members (A4) | Ends the drafting at once with the reason (no second draft: another draft cannot add a sensor); the thread ends as one the house cannot do, the user hears it, nothing is saved, a log line names the selection. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 31 | The receiver `persons` in the service (decision 20) | `mira.send_message` with `receiver: persons` is stored as a **private message of the house**, which goes out over the channels of every person that has one (a person without a channel is passed over, not a failure); no person with a channel: refused (400, "receiver persons: no person has a channel", a log line with the text), so the automation's trace shows it. Each call is sent: the dedupe key is not used for it (a key would only find an entry already sent). The integration passes the value on unchanged. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 32 | A member on when the watch is loaded | A template trigger fires when its template **turns** true: a member already on (or a streak running) when the watch is saved or Home Assistant starts is not told, the next streak is (as the device watch's decision 31). Found on the test HA: the demo's `binary_sensor.movement_backyard` is on for good, so "motion while nobody is home" over the whole test house never turned true; a sensor stuck on keeps such a watch quiet until it goes off. Accepted; the label takes such a sensor out. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 33 | The tool `house_check` | Offered in **every turn**, without a skill ("Ist alles zu?" is asked at any time); read only. Scopes `openings` (default), `lights` ("Brennt irgendwo noch Licht?") and `motion`; areas and floors by name or id. Temperatures have no "open" and are no scope of a check. Its answer names what is open with its area, how many were checked, and what does not report, which the model is told to say too (Flash Lite left it out at first). Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 34 | The check's entry (C2) | Kind `simple`, importance 1, for the receiver (house or a person; another value is the caller's error), key `check:<scope>` (a second check while it is unheard replaces it), expiry 12 hours, text "Noch offen: Fenster Bad (Bad)." and, when some do not report, "Meldet sich nicht: …". Nothing open: no entry, also when some do not report. Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 35 | Errors of `/v1/check` | A scope, area, floor or receiver that does not exist: 400 with what there is; Home Assistant failing to render: 502 with its answer and a log line; the integration's action raises them as `check_failed` with the service's reason, a 404 (an older service) as `check_needs_update` ("Mira kann das erst nach einem Update."). Not tried again (Retries). Taken while building, 2026-11-13, to be confirmed by the maintainer |
| 36 | What a battery is (battery watch) | A `sensor` of device class `battery` (Home Assistant's percentage) below `below` (default 20 %), or a `binary_sensor` of device class `battery` that is `on` (Home Assistant: "low"). Both domains form one watch group, the scope **batteries**. Rechargeable batteries report the same class (phones, tablets, cars, robot vacuums, e-bikes); they are left out by the label, which the planner proposes for them as for the outside lights (decision 45). Read on the maintainer's house (read only, 2026-11-13): 46 percentage sensors and 12 low-battery sensors of radio devices, 7 phones, a car and a robot vacuum; none below 37 % that day. (taken while building, to be confirmed by the maintainer) |
| 37 | Once a day, not once per streak | Both watches **look once a day at `at`** (default 09:00, a time trigger) and name every member that breaks the rule then; while it lasts, it is named again the next day (the dedupe key replaces an entry not yet heard), until the battery is changed, the device is back, or the member carries the label. Reasons: a low battery and a lost device are lasting states of single members, not events; a template trigger fires once per streak of the whole group (decision 3), and in a real house some battery is low or some device is unreachable for weeks, so a second one would never be told; a watch cannot remember whom it told, since `this` is empty in a trigger after the automation is saved or reloaded (checks, row 13), and a per-member streak by `last_changed` would name a battery again at every percent it loses. The time trigger also fires nothing at a restart or a reload (decision 39), and costs one render a day instead of a template over every sensor rendered up to once a second. A departure from "push, not polling" for this reason; Home Assistant's own community blueprints for batteries look once a day the same way. (taken while building, to be confirmed by the maintainer) |
| 38 | When a device is unreachable (availability watch) | An entity counts as **unreachable** when its state is `unavailable` for at least `hours` (default 1), by its `last_changed`. `unknown` does not count (a value not there yet is no lost device). A state with the attribute `restored` does not count: no integration provides the entity now (an entity left over from a removed device or integration, or an integration that did not load); Home Assistant shows both on its own pages, and the left-overs would otherwise fill the entry every day (the test HA has seven). Known gap: a device that is already off when its integration starts can leave its integration unloaded and its entities `restored`. Members are only entities that belong to a **device** (Home Assistant's device registry), of the domains `sensor`, `binary_sensor`, `switch`, `light`, `cover`, `climate`, `lock`, `fan`, `valve`: the scope **devices**; media players (off by design), helpers and templates (no device) are none. (taken while building, to be confirmed by the maintainer) |
| 39 | A restart or a reload | A restart gives every entity a new `last_changed` (checks, row 15), a reload of an integration keeps it for an entity that stays `unavailable` (row 16). Nothing fires at either (decision 37); entities that are unavailable for a few seconds or minutes while Home Assistant starts or an integration reloads are available again by the check, and a restart within `hours` before the check hides a lost device for that day only (named the next day). No flood, no extra entry. (taken while building, to be confirmed by the maintainer) |
| 40 | How one entry names many | **By device**: the battery watch names a member's device (its name given by the user, else its name), each device once, with the percentage where it has one ("Rauchmelder Flur (12 %), Wassermelder Keller"); an entity without a device by its own name. The availability watch names a device whose member entities are all `unavailable` by the device's name, otherwise (only some of its entities are lost: a broken probe) each unreachable entity by its own name. Never an entity id. (taken while building, to be confirmed by the maintainer) |
| 41 | The selection of the two | The whole house by default, narrowed by areas and floors as every watch group (decision 22), minus the label. **No group helper**: a fixed list is not how one thinks of batteries or devices, and the label covers the exceptions. (taken while building, to be confirmed by the maintainer) |
| 42 | One selection over several domains | The shared selection (decision 21) takes `domain` as one domain or a list (the batteries: `sensor` and `binary_sensor`; the devices: nine). The macro's text changes, so every group blueprint gets `mira-version: 2` and is replaced as in the device watch (its step D). (taken while building, to be confirmed by the maintainer) |
| 43 | Receiver and entry | As every group blueprint (decision 24): `persons` while `zone.home` is 0, else the house; kind `simple`, importance 1, key `watch:<automation id>`, expiry 12 hours. (taken while building, to be confirmed by the maintainer) |
| 44 | The inputs' bounds | Battery: `below` 1 to 90 % (default 20), `at` (default 09:00). Availability: `hours` 0.05 to 72 (default 1; the lower bound lets the test HA run it in minutes), `at` (default 09:00). Both: `areas`, `floors`. (taken while building, to be confirmed by the maintainer) |
| 45 | The proposal and the planner | The proposal names how many batteries or devices the watch holds today (by device, decision 40) and those that already break the rule ("Schon jetzt schwach: …", "Schon jetzt nicht erreichbar: …"). The planner sees every battery with its value and proposes as exceptions what is charged rather than changed (phones, tablets, cars, robot vacuums, e-bikes, watches); for the devices it sees only the entities unreachable today (in a real house the whole list is thousands of entities) and proposes nothing by itself. (taken while building, to be confirmed by the maintainer) |
| 46 | "Ist alles zu?" | `house_check` and `mira.check` keep their scopes (openings, lights, motion): batteries and devices are refused there as the temperatures are. "Wie geht's dem Haus?" builds on these watches later, in a spec of its own. (taken while building, to be confirmed by the maintainer) |
| 47 | Exceptions | The label on an entity, as everywhere (decision 5). A device is out of the availability watch when each of its member entities carries the label: the planner's `exclude` names them all (the ones it sees unreachable), `watch_exception` sets it on one entity at a time and finds the members of the two new scopes too. (taken while building, to be confirmed by the maintainer) |
| 48 | Made in conversation | As every group watch (decision 29): the field `watch` of `aufgabe` holds "Gruppe: Batterien" or "Gruppe: Geräte, die nicht erreichbar sind"; the time is not asked (09:00 unless the user says one), nor the percentage or the hours. (taken while building, to be confirmed by the maintainer) |

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
| Mira's blueprints (`internal/watch`, from the [Device watch](device-watch.md)) | The nine rules as blueprints; made ready, versioned and replaced as there | Nine more blueprints, one shared selection (over one domain or several, decision 42) | The same building block; a group watch is only another kind of blueprint |
| Tasks, drafting (skill `aufgabe`, from the device watch) | A group watch is a task made from a blueprint | The catalog names the selection's inputs; the skill's prose says how a selection is said | One way to make a task |
| Saving (`automation.Save`, `own_automations`) | Saves the automation from the blueprint | None | As in the device watch |
| `mira/automation/validate` (integration) | Checks the rendered automation | None | As in the device watch |
| `mira.send_message` (integration) and the notifier (service) | The blueprints' action | The receiver value `persons` (decision 20): the service delivers the entry privately to every person with a channel | Already there for tasks; one value more, no new field |
| Messages, kind `simple` | A watch's entry | None | As in the device watch |
| Mira's labels (`custom_components/mira/labels.py`) | The label "Mira: nicht überwachen", created by the integration under a fixed id (`mira_dont_watch`) in Home Assistant's language, as Mira's other labels | One more label | Labels are created there, known by their ids, never their names |
| Home Assistant's entity registry (`config/entity_registry/update`, admin) | Setting the label on an entity on the user's word | None (an existing command) | Home Assistant's own place for a label |
| Home Assistant's templates (`render_template`, one subscription answered once) | "Ist alles zu?": the same selection, rendered by Home Assistant | None | The check and the watch compute the group the same way; nothing fetches every state to filter here |
| **New: the action `mira.check`** (integration) and the route `POST /v1/check` (service) | Triggering a check from an automation or a script (decision 16) | New action, new route; the fields optional both ways | Home Assistant's way to call Mira; an older service answers 404 |
| **New: the tool `house_check`** (service) | "Ist alles zu?" in a conversation, on the same code as `/v1/check` | New tool | The model must not compute the group itself (decision 13) |
| Fake devices (test HA) | Floors; windows, doors, motion and temperature sensors in areas with floors; persons home and away; batteries and devices that can be lost (step 6) | Extended | The test HA has windows and motion sensors, but no floors, no temperature sensors in rooms and no way to empty `zone.home` on purpose; its batteries (the demo's, the fake Zigbee2MQTT's) cannot be set, and no device can be lost on purpose |

## The blueprints

All are `domain: automation` under `mira/` of Home Assistant, made ready and replaced as in the device watch (its
section D). Each entry names the members that break the rule (their names), goes to the house, or while nobody is home
to every person (decision 20), with `dedupe_key: watch:<automation id>` and `expires_in: 12h`.

### The selection (shared by all)

- **Inputs:** `areas` (areas, optional), `floors` (floors, optional), `group` (a group helper, optional; not for the
  battery and the availability watch, decision 41). The domain (one or a list, decision 42) and the device classes are
  fixed by each blueprint. The label id is fixed (`mira_dont_watch`).
- **Members:** with `group`: its members (`expand`). Otherwise the entities of the blueprint's domains whose
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

Once per streak for these seven (decision 3): the trigger fires again only after no member broke the rule.

### 8. Batteries low (`battery_low`, the battery watch)

- **Inputs:** the selection (areas, floors) over the scope batteries (`sensor` and `binary_sensor` of `battery`),
  `below` (%, default 20), `at` (time, default 09:00).
- **Trigger:** the time `at`, every day (decision 37).
- **Rule:** a `sensor` member whose state is a number below `below`, or a `binary_sensor` member `on`; `unavailable`
  and `unknown` do not count (that is the availability watch's).
- **Entry:** "Batterie fast leer: {devices}." with the percentage where there is one (decision 40). Importance 1. No
  member low: no entry.

### 9. Devices not reachable (`unreachable`, the availability watch)

- **Inputs:** the selection (areas, floors) over the scope devices (decision 38), `hours` (default 1), `at` (time,
  default 09:00).
- **Trigger:** the time `at`, every day (decision 37).
- **Rule:** a member that belongs to a device, is `unavailable` and not `restored`, with its `last_changed` at least
  `hours` ago (decision 38).
- **Entry:** "Nicht erreichbar: {devices or entities}." (decision 40). Importance 1. None: no entry.

Both: once a day while it lasts (decision 37), not once per streak.

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
2. The entry goes to the house, or while `zone.home` is at 0 to every person privately (decision 20).
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

### E. The battery and the availability watch

1. **Making** (as A): "Sag mir, wenn eine Batterie leer wird" fills `watch` with "Gruppe: Batterien", "Sag Bescheid,
   wenn ein Gerät nicht mehr erreichbar ist" with "Gruppe: Geräte, die nicht erreichbar sind" (decision 48). The
   planner gets the catalog with the two blueprints, the batteries with their values, the devices unreachable today
   (decision 45), chooses `battery_low` or `unreachable`, and for the batteries proposes the rechargeable ones in
   `exclude`.
2. **Check and proposal** (A4): the code renders the members once, counts them by device and names those that break
   the rule today; the proposal: "Heute sind das 12: … Schon jetzt schwach: Rauchmelder Flur (12 %). Ausgenommen: …".
3. **On yes:** the exceptions' labels, then saved as a task (A5).
4. **Telling:** at `at` Home Assistant's time trigger runs the automation; the variable `breaking` renders the
   selection and the rule once; with none, a condition ends the run without an entry; else `mira.send_message` with
   the names (decision 40) for the receiver (decision 43). Read out, the model words it.
5. **Changing, switching off, deleting:** as D. "Die Handys nicht" is an exception (decision 47).

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
- **The battery and the availability watch in Home Assistant:** one time trigger each; nothing between the checks.
  At `at`, one render of the selection and the rule over the domains (the batteries: every `sensor` and
  `binary_sensor` is read for its class; the devices: every entity of nine domains, and for each one in a device its
  device), once a day. Grows with the number of entities of those domains; on the test HA the devices' render over
  407 entities took about 20 ms (checks, row 17).
- **Making one** (the turn waits, as A): one render of the house, one of the members of the scope, one for the planner's
  catalog per scope; for the devices the members' render returns every entity of a device in the nine domains (in a
  house of thousands of entities some hundred kilobytes, one render), the catalog's only the unreachable ones.
  `watch_exception` renders every scope's members once, the devices' included.

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
| — | A member is `unavailable` or `unknown` | It does not count as breaking a rule; "Ist alles zu?" names it as not readable ("Das Fenster im Bad meldet sich nicht"). Except in the availability watch, whose rule `unavailable` is (decision 38) |
| E2 | The house has no battery or no device in the selection | As A4: the proposal says so, nothing is saved, a log line names the selection |
| E2 | Rendering the members fails | As A4: the planner's draft ends with Home Assistant's answer; a log line with the scope and the template's start |
| E4 | Home Assistant is down at `at` | No check that day; the next day's names it (Home Assistant's time trigger does not catch up). Nobody learns of the missed one: the next check is the answer |
| E4 | The template fails to render at `at` (a broken Home Assistant) | The automation's trace and Home Assistant's log show the error; no entry |
| E4 | `mira.send_message` fails | As B1 |
| C1, C2 | `house_check` or `mira.check` asked for batteries or devices | Refused as an unknown scope (decision 46), with the scopes there are |

## Checks on the test HA

| # | Transition | Trigger | Signal | Checked |
|---|---|---|---|---|
| 1 | The selection as a template | `label_entities()`, `floors()`, `floor_entities()`, a filter by `attributes.device_class` | the fakes' windows and motion sensors | 2026-11-13, 2026.11.1 (no floors yet) |
| 2 | A floor and an area narrow the selection | fake windows on two floors | only the floor's | 2026-11-13, 2026.11.1: the floor's two windows; floor and area together their union; a group helper its two windows; a light group is no member of "all lights" |
| 3 | The label takes a member out, at once | `config/entity_registry/update` with the label | the template's result without it; a running watch does not fire for it | 2026-11-13, 2026.11.1: the next render is without it (the group helper's members keep it); refused for a token without admin (`unauthorized`); an unknown label id is taken without a word (decision 27). The running watch: step 3 |
| 4 | `zone.home` goes to 0 and back | the fake persons leave and come | the zone's state 0, then 1 | 2026-11-13, 2026.11.1: "Sim zu Hause Anna" off, `zone.home` 1 → 0 at once; on, 0 → 1 |
| 5 | Open while nobody is home | a fake window open, the persons leave | the trace fires after `delay` | 2026-11-13, 2026.11.1: 5 minutes after the house became empty, to the second; again 5 minutes after the window was closed and opened (a new streak) |
| 6 | Open at night, motion at night | a fake window open or motion in the (shortened) night window | the trace fires | 2026-11-13, 2026.11.1: a night window around now; the window's entry 1 minute (`delay`) after it opened, to the second; the garden's motion at once |
| 7 | Open too long, lights left on | a fake window or light on for the (shortened) time | the trace fires once per streak | 2026-11-13, 2026.11.1: open 1 minute, light 0.05 h: each at the first full minute after the time (the template's render each minute, as the device watch's row 6); not again while they stayed on; a labelled light on for the time: no trace (row 3) |
| 8 | Motion while nobody is home | fake motion with nobody home | the trace fires | 2026-11-13, 2026.11.1: at once, the entry at both fake phones about 6 seconds later (the notifier's tick); over the whole house only once the demo's motion sensor, on for good, was left out (decision 32) |
| 9 | Too cold | a fake temperature below the value for the (shortened) time; how long the template takes over all sensors | the trace fires; the render time | 2026-11-13, 2026.11.1: the entry 1 minute after 4 °C, to the second; the template over the test HA's 179 sensors rendered in 3 to 11 ms (`render_template`, the round trip included); Home Assistant renders a template over a whole domain at most once a second |
| 10 | `render_template` once | the check's template | the first answer, then the subscription ends | 2026-11-13, 2026.11.1: the first answer after 2 ms, `unsubscribe_events` answered with success, no event after it; `house_check` at the test Mira 3 to 4 ms |
| 11 | `mira.check` from a script | a script with `response_variable` | the list in the response, the entry | 2026-11-13, 2026.11.1: the script's response `{"scope": "openings", "open": [Fenster Bad], "checked": 9, "message_id": …}` in 25 ms, the entry for the house; all closed: `open` empty, no entry; with the service of step 3 (no route): "Mira can do this only after an update of the Mira service" |

| 12 | The selection over two domains (batteries) | the shared selection with `domain` a list, `sensor` and `binary_sensor` of `battery`; `device_attr` for the names | the batteries with their devices' names | 2026-11-13, 2026.11.1: the test HA's five battery sensors (the demo's, the fake Zigbee2MQTT's), each with its device's name ("Outside Temperature", "Klimasensor Bad"), rendered in 8 ms; the openings with `domain` a string as before (9) |
| 13 | A watch remembering whom it told | a template trigger reading `this` | `this` in the trigger after the automation is saved | 2026-11-13, 2026.11.1: an automation saved over the config API with a template trigger `{{ this is not none and … }}` did not fire when the window opened (`this` is empty: the triggers are attached before the automation's state is written); switched off and on, it fired. Hence decision 37 |
| 14 | The daily check | a watch with `at` a minute ahead, a fake battery low, a fake device lost | the trace at `at`, the entry naming the device | 2026-11-13, 2026.11.1: `battery_low` at 23:13 to the second, `breaking` "Outside Temperature (12 %)" (the demo's), "Fake Rauchmelder Flur (12 %)", "Fake Wassermelder Keller", the call `mira.send_message` for the house with "Batterie fast leer: …"; `unreachable` (0.05 h) at 23:16 named "Fake Steckdose Garage" and the Matter plug "Steckdose Wohnzimmer" (an entity's name: its device has entities that are there) |
| 15 | A restart | Home Assistant restarted with an entity `unavailable` before and after | the entity's `last_changed` | 2026-11-13, 2026.11.1: the test HA's Matter plug (offline) had `last_changed` 3 s after the container started; the fake plug lost, Home Assistant restarted with `homeassistant.restart` at 22:19:38 UTC: both its entities `unavailable` again, not `restored`, `last_changed` 22:19:58 (the start); no watch fired at the start |
| 16 | A reload of an integration | the Matter entry reloaded, its plug `unavailable` before and after | the plug's `last_changed` | 2026-11-13, 2026.11.1: kept (21:36:47 before and after the reload at 21:59) |
| 17 | Who is a device, and the cost | the devices' selection over the nine domains | members with a device, the `unavailable` ones, `restored`, the render time | 2026-11-13, 2026.11.1: 407 entities, 371 with a device; 8 `unavailable` among them, 7 of them `restored` (left-overs of the found-devices fake and a duplicate of the Show's switch), the Matter plug not; 20 to 22 ms |
| 18 | A device lost, and one of its entities lost | the fake device's entities all `unavailable`, then only one | the entry names the device, then the entity | 2026-11-13, 2026.11.1: the whole fake plug lost: "Fake Steckdose Garage" (row 14); the plug back and only its signal lost: "Fake Steckdose Garage Signal" at 23:24 |

Each row is checked before the step that builds on it.

## Steps

1. **The selection and the blueprints:** the shared macro, the seven blueprints embedded with the device watch's;
   rows 1 to 3. Built 2026-11-13 (decisions 21 to 25): `internal/watch/blueprints/selection.jinja` and the seven
   blueprints, their texts in German and English, the selectors area, floor, time, boolean and the group helper read
   by the code, the code's check of the selection (`CheckSelection`), the members and the house's floors, areas and
   group helpers rendered by Home Assistant (`render_template` once, `ws.RenderTemplate`); the fake house of
   `watch_fake` (floors, openings, motion, temperatures, lights, phones; [Fake devices](fake-devices.md)) came with
   it, as rows 2 and 3 need it. GW-01 to GW-03 in pytest (`tests/ha/test_group_watch.py`, with every group
   blueprint loading as an automation); rows 1 to 3 checked.
2. **Drafting a group watch:** the catalog's selection inputs, the label tool, the proposal with today's members; the
   skill's prose; the label in `labels.py`. Built 2026-11-13 (decisions 26 to 30): the planner's group catalog (the
   house's floors, areas, group helpers and members), the code's check of the selection and the exceptions, the
   proposal with today's members and the exceptions, the labels set on the yes (`watch.SetLabel`), the tool
   `watch_exception`, the field `watch` with "Gruppe: …", the skill's prose. GW-04 to GW-07 in Go. At the test Mira by
   voice at the fake Show: "Gib mir Bescheid, wenn ein Fenster offen ist und keiner da ist" – the proposal "Heute sind
   das 9: Fenster Bad, …" – "Ja, passt" saved it from `open_while_away`; "Sag Bescheid, wenn irgendwo Licht ewig an ist,
   aber die Terrasse nicht" proposed all lights but the terrace (the planner then did not yet see the members it
   did not know, so the indicator lights stayed in; since then the planner gets them); on "nimm auch den Garten und
   die Leuchtringe … raus" all but the terrace, the garden and the indicators; saved, with their labels (see the
   open point below); "Überwach den Garten wieder" took the label off (`watch_exception`); "Sag Bescheid,
   wenn im Keller Bewegung ist, während keiner zu Hause ist" – "Im Keller gibt es gar keinen Bewegungsmelder" (the
   planner, seeing the members), nothing saved; with the label deleted in Home Assistant "Überwach die Terrasse nicht
   mehr" – "… das passende Label fehlt. Die Mira-Integration braucht dafür ein Update" (the log names the entity).
   GW-07's refusal (a token without admin) in Go only: the test Mira works with the owner's token. Open for the
   maintainer (the test Mira's voice model Flash Lite): a broken front matter of `aufgabe` (fixed while building)
   first sent the wish to the skills `regel` and `aufgabe-aendern`; after a changed proposal it once confirmed in the
   same turn without the user's yes, which set labels on two indicator lights of the real devices' entries in the
   test HA (taken off again at once); "Überwach die Terrasse nicht mehr" once deleted the lights' watch
   (`task_delete`) and once went to `aufgabe-aendern`; with `smart` it asked and called `watch_exception`.
3. **The fakes and the runs:** the receiver `persons` (decision 20); floors, openings, motion, temperatures, persons home and away; rows 4 to 9; each
   blueprint end to end (entry, read out, follow-up). Built 2026-11-13 (decisions 31 and 32): the receiver `persons`
   in the service (`/v1/messages`, the notifier's private delivery of the house to every person with a channel); the
   fakes came with step 1 (persons home and away are the test HA's "Sim zu Hause" switches; Anna's and Ben's channels
   the fake phones, set on Mira's page in the test HA). GW-08 to GW-16 in pytest (`tests/ha/test_group_watch.py`, the
   time moved on); rows 3 to 9 checked. End to end at the test HA and the test Mira: the watch "Fenster bei
   Abwesenheit" made by voice in step 2 told "Niemand ist zu Hause, aber Fake Fenster Küche ist offen." to both fake
   phones 5 minutes after Anna left; motion while nobody is home in the garden reached both phones; the night,
   too-long, left-on and too-cold watches put their entries for the house, and "Hab ich neue Nachrichten?" at the fake
   Show said "Vorhin um 22:28 Uhr gab es Bewegung im Garten, und im Keller ist die Temperatur mit 4 Grad zu niedrig.
   Außerdem ist hier im Schlafzimmer das Fenster noch offen und das Licht brennt auch noch."; "Mach's aus" switched the
   bedroom's fake light off (`HassTurnOff`); a watch switched off (GW-16) told nothing. The test HA's Telegram chat of
   the person Admin got the first private entry before the test persons' phones were set; it was taken off Admin for
   the runs and set back after.
4. **"Ist alles zu?":** the tool `house_check`, the route `/v1/check`, the action `mira.check`; rows 10 and 11. Built
   2026-11-13 (decisions 33 to 35): `watch.HouseCheck`, `tasks.Check` and the tool `house_check`, the route
   `/v1/check`, the action `mira.check` with its texts in German and English. GW-17 to GW-20 in Go, GW-19 (the
   integration's side) and GW-21 in pytest (`tests/ha/test_check.py`); rows 10 and 11 checked. At the test Mira by
   voice at the fake Show (Flash Lite): "Ist alles zu?" with the bathroom window open – "Im Bad steht noch ein Fenster
   offen."; with the office's fake window unavailable – "Das Fenster im Bad ist noch offen, und das Bürofenster meldet
   sich nicht."; a script with `mira.check` and `response_variable` as in row 11, GW-21 against the service of step 3
   at the test HA.
5. **The guide**, German and English: group watches, the label, "Ist alles zu?", `mira.check` in an automation. Built
   2026-11-13 (`docs/guide/de.md`, `en.md`, "Wächter für Gruppen" after the device watches).
6. **The battery and the availability watch in Home Assistant:** the shared selection over a list of domains
   (decision 42, every group blueprint to version 2), the blueprints `battery_low` and `unreachable`, the fakes'
   batteries and a device that can be lost (with a device, [Fake devices](fake-devices.md)); rows 14, 15 (with the
   fake) and 18. Built 2026-11-13: the selection's `domain` one or a list (`Scope.Domains`, `Blueprint.Domains`), every
   group blueprint at version 2 (replaced at the test Mira's start, the log names each), `battery_low` and
   `unreachable` with their texts in German and English, `house_check` keeping its three scopes; the fakes' devices
   with the config entry "Fake Geräte" and `watch_fake.reachable` (a lost device keeps its loss over a restart in
   its restore data: Home Assistant keeps no `unavailable` to restore). GW-22 to GW-26 in pytest
   (`tests/ha/test_group_watch.py`, the restart of GW-26 as states written anew, the real one row 15); rows 14,
   15 and 18 checked.
7. **Drafting them:** the scopes batteries and devices in the code, the catalog, the proposal's counts by device and
   "Schon jetzt …", the exceptions, `house_check` refusing them, the skill's prose; GW-27 to GW-31 in Go; each made
   by voice at the test Mira, triggered, read out.
8. **The guide**, German and English: the two watches in "Wächter für Gruppen".
