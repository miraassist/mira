# Device watch – Spec

Status 2026-11-13: specified (decisions 1 to 21), not built. Scenarios: [device-watch.feature](device-watch.feature). First part
of "Mira watches over the house": a **device watch** watches one device against its own normal, learned from its
history. A **group watch** watches a group of sensors against a rule for a situation (lights left on, windows open,
motion while nobody is home, the checks at occasions): [Group watch](group-watch.md), specified later. "Wie geht's
dem Haus?" gets a spec of its own.

## Goal

Mira watches over the house's devices without the user building automations: water that runs on, a meter that
never rests, a device that no longer follows its daily routine, a fridge that runs without a pause, a dishwasher that
runs too long. The watching itself is a fixed
**blueprint**, written once and checked; Mira applies it to the house in conversation.

- "Pass auf, ob bei uns Wasser dauernd läuft." – "Mach ich: Läuft der Wasserzähler eine halbe Stunde ohne Pause, sag
  ich Bescheid. Passt das?"
- "Achte aufs Aquarium." – "Das Aquariumlicht läuft jeden Tag von etwa 7:40 bis 11:40 und von 14:20 bis 21:00, mit
  rund 50 Watt. Soll ich mich melden, wenn es davon abweicht?"
- Later, at a satellite: "Seit einer halben Stunde läuft Wasser, der Zähler steht bei 81.240 Litern." – "Ben ist
  in der Badewanne." – "Alles klar."

Not here: alarms (smoke, fire, gas, leak sensors: covered by the devices themselves or a coupled system), security
watching (cameras, network; that is what tools like Home Generative Agent do).

## Decisions (2026-11-13)

| # | Question | Decision |
|---|---|---|
| 1 | How a watch is written | As a **Home Assistant blueprint** (automation): Home Assistant's own way to share an automation with named inputs. Nothing rebuilt. Mira's blueprints are the ones under `mira/`; the word is Home Assistant's, in German too ("Blueprint") |
| 2 | How free the model is | It only fills a blueprint's named inputs; it never writes or changes the blueprint. A wish that fits no input is said, not built around. To start with; revisited when blueprints are in use |
| 3 | First blueprints | The water meter that runs on, the water meter that never rests (both after the maintainer's own automations), a device that follows its daily routine (an aquarium's light and pump); then a cycle and a device run (decision 12). "A light left on" moved to the group watch (decision 20) |
| 4 | Offering | **On request**: "Gibt's Geräte, die du überwachen könntest?" lists the candidates of the house. Mira does not offer by herself when a device turns up; an introduction when Mira is newly set up will offer it (a topic of its own, not here). Changed 2026-11-13: first "by herself when a device is found", dropped for the request and the introduction. The one offer that comes by itself is one Mira promised (decision 14) |
| 5 | Who does what | The blueprint decides **how** it is watched; the model decides **what** in this house and **what is said**. Table below |
| 6 | Which building blocks | Only existing ones, except the blueprints, the device profile and one table (section "Building blocks"). Every change of a component or of the database is named there with its reason. The database's one change: the table `watch_declined` (decision 21) |
| 7 | Who brings the blueprints | The **service** saves them in Home Assistant when they are missing or older. Reason: the code and the model that fill a blueprint's inputs live in the service; with the blueprints in the service they always have the same version, and the interface between integration and service does not change. The integration as the bringer was rejected: service and integration are updated separately, so a newer service could fill an input an older integration's blueprint does not have |
| 8 | Which lights for "left on" | Moved to [Group watch](group-watch.md) with the blueprint "left on" (2026-11-13, decision 20) |
| 9 | Pausing, e.g. on holiday | Moved to [Group watch](group-watch.md) (decision 20) |
| 10 | How "all lights" becomes a trigger | Moved to [Group watch](group-watch.md) (decision 20) |
| 11 | How a promised offer comes (decision 14) | The known way: an entry of a message, and behind it a thread that offers to take the device into the watching; its yes drafts the watch as the skill `aufgabe` does ([Messages](messages.md), as a found device) |
| 12 | What "normal" is | Three kinds, each a blueprint: a **daily routine** (on at fixed times: an aquarium), a **cycle** (on and off again and again: a fridge, a freezer), a **device run** (a run with a start and an end, now and then: a dishwasher, a washing machine, a dryer). No general "deviation from the mean": for a fridge switching between 0 and 45 W a mean says nothing, and it would need a statistics helper per device |
| 13 | Where the numbers come from | A **device profile** of the device, computed by code once when a watch is made or offered, never while it watches: the automation only carries the numbers as inputs. Its own tool, not the history tool: that one is for the model to look into the house in conversation; the device profile is for the offering's code and the drafting. Changed by decision 16: it gives a daily routine's windows too |
| 14 | Too little history | Mira offers a watch only when the device profile has enough to set its numbers. The long-term statistics (hourly, kept for years) find the **candidates** and their kind; the numbers of a cycle or a run need the states of the last days (minutes). A candidate without a run in those days waits: "Die Spülmaschine nehm ich in die Überwachung, wenn sie das nächste Mal gelaufen ist"; after its next run the offer comes. Nobody is asked for numbers they do not know |
| 15 | Where the promise to wait is kept | A **thread of the house**: a concern across turns is what a thread is ([Threads](threads.md)). It is the thread of the offer (decision 11), opened when Mira promises and waiting until the run. Not a key in `kv` |
| 16 | Where a daily routine's windows come from | From the **device profile**, as every number of a blueprint: one way, not two. The history tool is not used for it |
| 17 | Retries | By [Retries](retry.md): a lost connection, a timeout, a 429 and a 5xx are tried again, at most 3 times, with an exponential wait from 1 s (a wait Home Assistant names is kept); anything else (a refusal, a wrong request, a missing entity) ends at once with its trace. This holds for `blueprint/list`, `blueprint/save`, `blueprint/substitute`, `search/related`, the reloads and the recorder's reads; the subscription of a waiting candidate is renewed by the WebSocket client's reconnect, as every subscription |
| 18 | Receiver and importance | A watch's entry goes to the **house**, as every task's. Importance 2 for "water runs on" and "a cycle does not run any more" (a fridge without power), 1 for all else; nothing is urgent |
| 19 | The offer's thread | The target `ueberwachung-anbieten` in the skill `aufgabe`, fields: the device, the blueprint, the numbers; expiry 30 days. When the user says no, Mira remembers the device as declined and does not name it again, neither by herself nor in the list on request; "Pass doch auf die Spülmaschine auf" makes the watch all the same |
| 20 | Two kinds of watch | A **device watch** (this spec): one device against its own normal, learned from its history (its device profile). A **group watch** ([Group watch](group-watch.md)): a group of sensors against a rule for a situation, said by the user (all lights but the garden's, nobody home, at night, when leaving). "A light left on" is a group watch and moves there with its decisions 8 to 10. The ground is shared: blueprints as blueprints, drafting through the skill `aufgabe`, a watch is a task, its entry a message's. Cameras are not part of either for now |
| 21 | Where a declined device is kept (decision 19) | In Mira's database, table `watch_declined` (`entity_id`, `blueprint`, `declined_at`; key `entity_id`, `blueprint`): no label in Home Assistant, whose negative meaning on a device would be out of place and easily too much. Undone by making the watch by voice ("Pass doch auf die Spülmaschine auf"), which deletes the row |

## Who does what

| Step | Blueprint / code (fixed, checked) | Model (at run time) |
|---|---|---|
| How it is detected | The blueprint: triggers, conditions, durations, the message | — |
| Which device | The code lists candidates from the blueprint's entity selector (domain, device class, state class) among the entities exposed to Assist | chooses among several, asks when it is unclear |
| The inputs | The blueprint's selectors bound them; Home Assistant checks them (`blueprint/substitute`, the save) | turns the user's words into inputs ("nachts läuft zwei Stunden die Bewässerung" → a longer duration) |
| A daily routine | The device profile computes the windows from the last days | proposes them in words ("7:40 bis 11:40 und 14:20 bis 21:00") and takes the correction |
| Creating | The task's way as today: check, propose, save on yes | proposes in words, takes the yes |
| Telling | The automation calls `mira.send_message` with the facts (which meter, since when, its value) | words the message when it is read out and talks about it ("Ben ist in der Badewanne"; "mach's aus") |

## Building blocks

| Building block | Used for | Change | Reason |
|---|---|---|---|
| Tasks (`internal/tasks`, skill `aufgabe`, finisher `tasks.automation`) | A watch **is a task**: an automation labelled "Mira", made in conversation, listed, switched off and deleted as one | The drafting step offers the blueprints: the planner may answer with a blueprint and its inputs instead of an automation; the check renders it with `blueprint/substitute` and checks the result as today; the proposal is built from the rendered automation. The skill's prose names the blueprints | One way to make a task; the blueprint only replaces the model's free draft where one fits |
| Saving (`automation.Save`, table `own_automations`) | Saves the automation `{use_blueprint: {path, input}}` with `POST /api/config/automation/config/<id>`, reload, label, row of kind `task` | None | A watch is a task; Home Assistant takes a blueprint's automation through the same call (checked 2026-11-13) |
| `mira/automation/validate` (integration) | Checks the **rendered** automation (the forbidden actions, as today) | None; the service sends the rendered config | No change of the interface between integration and service |
| `mira.send_message` (integration) | The blueprint's action: `text` with the facts, `importance`, `dedupe_key`, `expires_in`, `task: "{{ this.attributes.id }}"` | None | Already there for tasks |
| Threads (`internal/threads`, table `threads`) | The promise to wait for a run and the offer after it (decisions 11, 15): a thread of the house, its entry in a message, its yes drafting the watch | A target for the offer, in the skill `aufgabe` (new target, no new code of threads); an expiry of 30 days, as threads of the house already have an expiry of their own ([Messages](messages.md)) | A promise across turns is what a thread is; the offer comes the known way, as a found device |
| Messages, kind `simple` | A watch's text is an entry of the receiver's `simple` message | None | An entry is a text, as a task's; nothing to work through over several turns: the follow-up ("mach's aus") is the next turn of the same conversation, with Home Assistant's tools, no thread |
| **New: the blueprints** (`internal/watch`, blueprints embedded in the service) | The blueprint files; making sure Home Assistant has them; the catalog the planner sees (name, description, inputs) | New package | No building block holds blueprints. In the service, since the code and the model that fill a blueprint's inputs must match its version |
| **New: table `watch_declined`** (`internal/store`, a migration) | The devices whose offer the user declined, per blueprint | New table | A list of Mira's own with no place in Home Assistant (decision 21); by the rule of [Database](../database.md), a list gets a table, not keys of `kv` |
| Fake devices (test HA) | A water meter and a power plug with a daily routine to drive the blueprints | Extended | The test HA has no water meter; [Fake devices](fake-devices.md) gets them |
| **New: the device profile** (in `internal/watch`, a tool for the drafting) | A device's kind (daily routine, cycle, run) and its numbers | New | No building block computes it: `state_history` condenses for reading (12 periods), a fridge has hundreds of runs in ten days |
| Recorder of Home Assistant (`history/history_during_period`, `recorder/statistics_during_period`) | The device profile's data: the states of the last days, the hourly statistics of months | None | Home Assistant's own, read for one entity at a time |

## The blueprints

Each is a blueprint in `blueprints/automation/mira/` of Home Assistant, `domain: automation`. The text of the entry
is an input the service fills in the house's language with the facts as templates; the model words it when it is read
out. Every entry goes to the house with `dedupe_key: watch:<automation id>` (a new entry of the same watch replaces the
old), `expires_in: 12h` and the importance of decision 18.

### 1. Water runs on (`water_runs_on`)

After the maintainer's automation "Dauerdurchfluss", without its timer helper.

- **Inputs:** `meter` (sensor, device class `water`), `gap` (minutes, default 11: a meter that reports every 5
  minutes counts as running while it changed within this), `duration` (minutes, default 30).
- **Trigger:** a template trigger, true while the meter changed within `gap` (`now() - last_changed`), held for
  `duration`. Home Assistant renders it on every change of the meter and once a minute (it uses `now()`).
- **Entry:** "Seit {duration} Minuten läuft Wasser ohne Pause. Zählerstand: {state} L."
- Once per streak: the trigger fires again only after the template was false (the water stopped for `gap`).

### 2. Water never rests (`water_never_rests`)

After the maintainer's automation "Schleichender Verbrauch" (a dripping tap, a toilet that runs on), without its
timer helper.

- **Inputs:** `meter` (as above), `rest` (hours, default 1), `window` (hours, default 24).
- **Way:** `mode: restart`. Triggers: the meter unchanged for `rest` (a rest: the run starts again), Home Assistant
  started, the automation loaded. The run waits `window`, sends the entry, and repeats every `window` while no rest
  comes (a daily reminder while it leaks).
- **Entry:** "Der Wasserzähler stand in den letzten {window} Stunden keine {rest} Stunde still. Vielleicht tropft
  ein Hahn oder die Spülung läuft nach. Zählerstand: {state} L."
- A restart of Home Assistant or a reload of the automations starts the window anew: the entry comes later, never
  wrongly. Accepted (the timer helper would survive a restart, but the blueprint would need a helper Mira makes and
  owns: a second kind of object for little gain).

### 3. (moved)

"Lights left on" is a group watch: [Group watch](group-watch.md) (decision 20).

### 4. Follows its daily routine (`daily_routine`)

For a device whose power follows the same times every day (an aquarium's light, a pump, a heater on a timer).

- **Inputs:** `power` (sensor, device class `power`), `on_above` (watts, default 5), `windows` (text, the expected
  times on, e.g. `07:40-11:40, 14:20-21:00`), `tolerance` (minutes, default 15).
- **Trigger:** a template trigger, true while the power's being above `on_above` differs from the windows, held for
  `tolerance`. Rendered on every change of the power and once a minute.
- **Entry:** "{name} weicht von seinem Tagesablauf ab: jetzt {power} W, erwartet {an/aus}."
- **Proposing the windows:** the device profile computes them from the last days (decision 16); the model proposes
  them in words, and the user's yes or correction fills `windows`. Where Home Assistant has a
  schedule for the device, the model may take its times instead (assumption: not looked at yet which schedules
  exist; the maintainer's aquarium has a switch "schedule" whose meaning is not checked).
- Example (the maintainer's aquarium light, 2026-11-11 to 13): on 07:41–11:41 and 14:21–21:01, about 50 W; on the
  12th off at 18:36 instead of 21:01. The blueprint would have said so at 18:51.

### 5. Cycle (`duty_cycle`)

For a device that switches on and off all day (a fridge, a freezer, a pump with a pressure switch).

- **Inputs:** `power` (sensor, device class `power`), `on_above` (watts, default 5), `max_on` (minutes: longer on is
  not normal), `max_off` (minutes: longer off is not normal).
- **Triggers:** the power above `on_above` for `max_on` ("läuft seit {max_on} Minuten ohne Pause – Tür offen?"); the
  power at or below `on_above` for `max_off` ("läuft seit {max_off} Minuten nicht mehr – Strom weg oder defekt?").
  Plain numeric state triggers.
- **Numbers from the device profile:** the longest normal run and pause of the last days with a margin, proposed by the model
  in words. Example (the maintainer's freezer, 2026-11-03 to 13, its plug reporting about every 5 minutes): 127 runs
  of 55 to 80 minutes, about every two hours; the fridge 262 runs, most 15 to 20 minutes, once 200 minutes.

### 6. Device run (`device_run`)

For a device that runs a programme now and then (a dishwasher, a washing machine, a dryer).

- **Inputs:** `power`, `on_above` (watts: a run starts), `off_for` (minutes at or below `on_above` that end a run:
  longer than the programme's own pauses), `max_minutes` (a run longer than this is not normal), `heat_above`
  (watts, optional: a run without a peak above it did not heat).
- **Entries:** "{name} läuft schon {max_minutes} Minuten, länger als sonst." "{name} ist fertig, hat aber nicht
  aufgeheizt."
- **Way:** tried on the test HA before it is written down (check rows 14 and 15). Assumption: one automation run per
  device run (`mode: single`, waiting for the heat, the end and the bound) can do it.
- **Numbers from the device profile:** duration, longest pause within a run and peak of the runs of the last days; the
  hourly statistics only find it as a candidate (the maintainer's dishwasher: 174 runs since 2022 with an hourly
  peak above 500 W; one run in the last ten days, 120 minutes, 1883 W).

## The device profile

What it computes, for one power sensor:

1. **Candidate and kind** from the long-term statistics (`recorder/statistics_during_period`, hourly mean and max, the
   last 90 days): on at the same hours every day → daily routine; on in most hours with a mean far below the max →
   cycle; on now and then with a high max → run. Under 5 W in every hour: no candidate. The bounds are tried on the
   maintainer's devices (read only) before they are written down.
2. **Numbers** from the states of the last ten days (`history/history_during_period`, the one entity,
   `minimal_response`): the runs above `on_above` with start, duration, the pause before and the peak; per kind: the
   windows (daily routine), the longest normal run and pause (cycle), duration, longest pause in a run and peak (run).
3. **Enough?** A daily routine needs 3 days, a cycle 2 days, a run one run within the states (proposed; a dishwasher
   running once a week would never have three in ten days; the hourly statistics of months show whether that run's
   length was typical). Not enough:
   the answer says it and what is missing; the candidate waits (decision 14).

The model gets the numbers and proposes the inputs in words; the code checks they lie within what the device profile saw.

### Waiting for the next run (decision 14)

The promise is a thread of the house (decision 15), waiting; its fields name the device and the blueprint. The
service follows only the power sensors of such threads
(`subscribe_trigger` with a numeric state trigger on their power, filtered at the source); when a run ended (`off_for`
after being above `on_above`), the device profile is computed again; with enough, the thread's entry comes (decision 11) and the
thread waits for the yes. At start the service subscribes again for every waiting thread. A thread that expires
before the device ran ends its subscription with a log line (the device and the days waited); nothing is said, as
for every expired thread. Asked in conversation, Mira says it: "Die Spülmaschine nehm ich in die Überwachung, wenn sie das nächste Mal
gelaufen ist."

## What it does, step by step

### A. Making a watch (in conversation)

1. The skill `aufgabe` collects the wish as today (`watch`, `when`, `measure`, `reaction`).
2. **Blueprints ready** (once per start of the service and at first use): `blueprint/list` for the domain `automation`;
   every embedded blueprint that is missing or differs is saved with `blueprint/save` (path `mira/<name>.yaml`,
   `allow_override` when it differs). The blueprints carry a version in their description; "differs" compares it.
3. **Drafting:** the planner gets, besides what it gets today, the catalog of blueprints (name, what it watches, its
   inputs with their bounds). It answers either with a blueprint and its inputs, or with an automation as today.
4. **Check of a blueprint's draft:** `blueprint/substitute` with the inputs (Home Assistant renders the automation and
   refuses a missing or wrong input); the rendered automation goes through `mira/automation/validate` as today. A
   refusal goes back to the planner once with the reason, as today.
5. **Proposal:** from the rendered automation, as today ("Wenn der Wasserzähler eine halbe Stunde ohne Pause läuft,
   sag ich Bescheid."). On yes: saved as a task (`{alias, description, use_blueprint: {path, input}}`).

### B. Telling

1. Home Assistant's trigger fires; the automation calls `mira.send_message` with the entry.
2. The message is a `simple` entry of the receiver's message, as every task's text: announced, ring, screen, "Hab
   ich neue Nachrichten?".
3. Read out, the model words it; what the user answers is the next turn ("mach's aus", "Ben badet", "lass gut sein").

### C. Changing, switching off, deleting

As every task (`tasks-editing`): the inputs are changed by drafting again with the same blueprint; "Pass nicht mehr aufs
Wasser auf" deletes the automation (Mira's own, by its row). The blueprint stays.

### D. A blueprint changes (a new version of the service)

At step A2 the changed blueprint is saved over the old (`allow_override`). Home Assistant applies it to the
automations that use it at their next reload: the service reloads exactly those (`search/related` with
`automation_blueprint` gives their ids; one `automation.reload` per id). A running wait of blueprint 2 starts anew.

## What it costs

- **Per watch in Home Assistant:** one automation. Blueprints 1 and 4 render a template on every change of their sensor
  and once a minute; blueprints 2, 5 and 6 are plain state triggers. Nothing in the Mira service runs until an entry comes.
- **Making a watch** (the turn waits for none of it; the finisher runs after the turn, as today): `blueprint/list` once
  per start and, the first time, one `blueprint/save` per blueprint; the planner's call as today; `blueprint/substitute`
  (milliseconds); the save as a task (four calls). For a blueprint of a power sensor one device profile (below).
- **An entry:** one `mira.send_message`; the model words it when it is read out, as every message.
- **A blueprint changed:** one `blueprint/save`, one `search/related`, one reload per automation using it.
- **A device profile:** two reads of the recorder for one entity: 90 days of hourly statistics (about 2,200 rows) and ten
  days of states (a plug every 5 minutes: up to 3,000 rows). Computed in the service, milliseconds.
- **"Gibt's Geräte, die du überwachen könntest?"** (the turn waits): one read of the hourly statistics of the last 30
  days for all power sensors of the house in one call (`statistic_ids` as a list), the kinds computed from it, the
  states of the last ten days only for the candidates. Grows with the number of power sensors (the maintainer's house:
  about 130, so about 94,000 hourly rows). How long the turn waits for it is measured on the maintainer's house (read
  only) before step 5 is built; assumption: a few seconds.
- **A waiting candidate:** one trigger subscription per waiting entity; nothing else until its run ends.
- Grows with: the number of watches (one automation each), not with the house's size.

## Errors

| Step | Fails | Who learns of it |
|---|---|---|
| A2 | `blueprint/list` or `blueprint/save` refused (not an admin's token, a write error) | The turn: "Dafür braucht Mira erst einen neuen Zugang …" as for every task (not an admin), else the tool's error with Home Assistant's answer; a log line with the blueprint's path and the answer. No watch is drafted with a missing blueprint; free drafts work as before |
| A3 | The planner names a blueprint that does not exist, or inputs it does not have | Refused by the code before Home Assistant is asked; back to the planner once with the reason, as a refused draft today; then the tool's error |
| A4 | `blueprint/substitute` refuses (missing or wrong input) | Back to the planner once with Home Assistant's answer; then the tool's error naming the blueprint and the answer |
| A5 | The save fails | As a task's save today (hacs-default, section 3) |
| B1 | `mira.send_message` fails | Home Assistant's automation trace shows the error; the integration logs it as today. Nothing in the service (it never got the call) |
| D | Saving the changed blueprint fails | A log warning with the path and the answer; the old blueprint stays in use; tried again at the next start |
| D | `search/related` or a reload fails | A log warning with the automation's id; the old blueprint stays in use for it until Home Assistant's next reload |
| Device profile | The recorder refuses or answers with nothing for the entity | The tool's error with the entity and Home Assistant's answer; for the request a log line, and the answer names the sensors it could not read |
| Device profile | Not enough history | Not an error: the answer says what is missing; the candidate waits (decision 14) |
| Waiting | The subscription for a waiting entity fails or the entity is gone | A log line with the entity; the thread stays waiting and the subscription is tried again at the next start; for a gone entity the thread ends, with a log line |
| — | A blueprint's device is gone (deleted, renamed) | Home Assistant's own: the automation's trigger finds no entity; its repair / the automation's state "unavailable". Mira's task list shows the automation as today; nothing of Mira's own |

## Checks on the test HA

| # | Transition | Trigger | Signal | Checked |
|---|---|---|---|---|
| 1 | Mira saves a blueprint | `blueprint/save` (admin) | `success`, `overrides_existing: false`; listed by `blueprint/list` | 2026-11-13, 2026.11.0 |
| 2 | Saving again without override | `blueprint/save` | error `already_exists` | 2026-11-13 |
| 3 | Rendering with inputs | `blueprint/substitute` | `substituted_config` with the inputs in place | 2026-11-13 |
| 4 | An automation from the blueprint | `POST /api/config/automation/config/<id>` with `use_blueprint`, then `automation.reload` with the id | `{"result":"ok"}`; the entity `automation.<alias>` is `on` with the id | 2026-11-13 |
| 5 | A missing input | the same `POST` without the input | HTTP 400 "Message malformed: Missing input meter"; nothing saved | 2026-11-13 |
| 6 | Blueprint 1's template trigger with an input, rendered each minute | a fake meter changing every 5 minutes for 30 minutes | the automation's trace fires once at minute 30 | open |
| 7 | Blueprint 2's restart and wait | a rest of the fake meter; then none for the window (shortened for the test) | the trace: the run restarts on the rest, sends after the window | open |
| 8 | Blueprint 2 starts its window when loaded and at Home Assistant's start | reload / restart of the test HA | a running run in the trace | open |
| 10 | Blueprint 4 | a fake plug's power leaving its window | the entry after the tolerance | open |
| 11 | The entry reaches Mira | the automation's `mira.send_message` with `task` | a `simple` entry with the task as sender | open (as for tasks today) |
| 12 | A changed blueprint reaches its automations | `blueprint/save` with `allow_override`, `search/related`, reload by id | the trace of the next run shows the new version | open |
| 13 | Blueprint 5 | a fake plug on longer than `max_on`, off longer than `max_off` (shortened) | one entry each | open |
| 14 | Blueprint 6: a run too long | a fake plug's run longer than `max_minutes` | the entry | open |
| 15 | Blueprint 6: a run without heat | a fake plug's run ending without a peak above `heat_above` | the entry after `off_for` | open |
| 16 | The end of a waiting candidate's run reaches the service | `subscribe_trigger` with a numeric state trigger on the fake plug, its power dropping | the trigger's event | open |

Rows 6 to 12 are checked before the step that builds on them.

## Steps

1. **Blueprints in Home Assistant:** the package `internal/watch` with the blueprints of blueprints 1, 2 and 4 embedded
   (blueprints 5 and 6 come with step 7); A2 and D; the
   catalog. Tests: saving missing and changed blueprints, nothing for unchanged ones, the errors.
2. **Drafting with blueprints:** the planner's catalog and answer, the check (A4), the proposal, the save as a task;
   the skill's prose. Tests against the fake: a wish becomes a watch from a blueprint, a wrong input is said, a wish
   without a blueprint is drafted as before.
3. **The fakes and the runs:** a fake water meter and a fake plug with a daily routine in the test HA; rows 6 to 11
   checked; each blueprint run end to end (entry, read out, follow-up); the table `watch_declined` with its migration
   comes with step 5.
4. **The device profile:** the tool, the kinds and the numbers, the daily routine's windows included; its bounds tried
   read-only on the maintainer's devices first.
5. **Offering on request:** "Gibt's Geräte, die du überwachen könntest?": the candidates of all blueprints (water meters,
   power sensors by their kind), without the ones watched or declined.
6. **Daily routine from the device profile:** the model proposes the windows; the user corrects them.
7. **Cycle and device run:** the two blueprints, the fakes, rows 13 to 15.
8. **Waiting for the next run:** the thread of the promise, the subscription, the offer after the run; row 16.
9. **The guide**, German and English: watching, the blueprints, asking for candidates, pausing.
