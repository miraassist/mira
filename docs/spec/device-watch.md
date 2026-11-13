# Device watch – Spec

Status 2026-11-13: specified (decisions 1 to 21); steps 1 to 8 built 2026-11-13 (decisions 22 to 57 taken while building, to be confirmed), not released. Scenarios: [device-watch.feature](device-watch.feature). First part
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
| 22 | Who reloads the automations of a changed blueprint (D) | **Home Assistant itself**: `blueprint/save` with `allow_override` reloads all automations when it replaced a file (`homeassistant/components/automation/helpers.py`, `_reload_blueprint_automations`, one `automation.reload`). The service does not look them up (`search/related`) nor reload them by id: not rebuilt what Home Assistant does (checked on the test HA, row 12). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 23 | Who checks the inputs | Home Assistant checks only that no input is missing (`blueprint/substitute` and the save; `blueprint/models.py`, `BlueprintInputs.validate`: selectors are not checked, an input the blueprint does not have is ignored). The **code** checks the rest before Home Assistant is asked (A3): an input the blueprint has, a number within its selector's bounds, an entity among the ones exposed to Assist with the selector's domain and device class, times that read (`07:40-11:40, 14:20-21:00`), and for "water runs on" a gap shorter than the duration (else one change of the meter would count as running for the whole duration). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 24 | The entry's text | The input `message` of every blueprint, the **service's**: filled from the service's texts in the house's language (templates over the blueprint's variables, with the time, as every task's text read later), replacing whatever the planner gives for it; the planner's catalog does not show it. The blueprint's own default is English, for one made by hand in Home Assistant. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 25 | When the window of "water never rests" starts | When a **rest ends** (after a rest the run waits for the meter's next change), at Home Assistant's start and when the automations are loaded; at the window's end no entry while the meter rests right then (a rest that began before the automation was loaded, which no state trigger saw). Without it a meter standing still for a day would be told as never resting. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 26 | A daily routine's power that is not a number | `unavailable` or `unknown` is **no deviation**: the device being gone is Home Assistant's to show (Errors, last row). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 27 | Retryable over the WebSocket (decision 17) | Home Assistant's answers over the WebSocket carry no HTTP status: tried again are a lost connection, a timeout and what `ws.Error.Retryable` counts as retryable (`home_assistant_error`, `unknown_error`); `blueprint/substitute`'s refusals are always about the inputs (a missing input comes as `unknown_error`) and are not tried again. CW-31 reads "a lost connection" for "a 503". Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 28 | Which blueprints the planner sees | Only the ones **ready** in Home Assistant (A2 done for them); one that is not is left out of the catalog and refused by the check with why (CW-04). A2 runs at the service's start once Home Assistant is connected, and again at the next drafting while one is not ready. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 29 | What the proposal says of a watch | The planner's sentence, then the inputs with their values as the code reads them from what is saved, defaults included ("Werte: Zähler sensor.x, Pause bis 11 min, Dauer 30 min."), so the yes covers exactly them; a change says the values before and after. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 30 | A change of a watch | The planner sees the watch as saved (`use_blueprint`) and answers with the changed `use_blueprint` or with the blueprint and its inputs; both go through A3 and A4. The integration checks the rendered automation, as for a new one. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 31 | A watch "water runs on" made while the water runs | Home Assistant's template trigger fires when its template **turns** true: a streak that runs while the watch is made is not told, the next one is. Accepted. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 32 | "Water never rests" switched on again (task_enable) | Starts no window by itself: the next rest, Home Assistant's start or a reload of the automations starts it. Open for the maintainer; accepted for now (the entry comes later, never wrongly). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 33 | How many device runs are enough (CW-24) | **One whole run** within the states, as "Enough?" says: a dishwasher running once a week would never have more in ten days. CW-24 read "one run in the last ten days … says that runs are missing", against "Enough?"; it now reads "no whole run". Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 34 | Which power sensors | Only sensors of device class `power` in **W**: the blueprints' numbers are watts and compare the state as it is. A sensor in kW is no candidate, and `device_profile` refuses it with why. A sensor whose hourly min went **below −5 W** is no device (the grid, a battery, an inverter feeding in); noise around 0 W is off. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 35 | What the device profile does with a state that is not a number | `unavailable` or `unknown` changes nothing: the state before it holds, as decision 26 for the blueprint. A run cut by the start of the states, or still on at their end, is not counted (its length is unknown). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 36 | Who gets the device profile | The model, as the tool `device_profile`, which comes with the skill `aufgabe` (offered in a turn that opened it, as the answering machine's tools come with theirs): the model proposes a daily routine's times from it in conversation (step 6). For an entity Mira may see (exposed to Assist) only. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 37 | The house's hours and days | The hours of the statistics and the days of a daily routine are taken in the service's time zone, as the tasks' views take them (`time.Local`; the HA app runs in Home Assistant's). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 38 | Candidates of a blueprint not built yet | Only blueprints **ready** in Home Assistant are offered (as decision 28 for the planner): until step 7 brings `duty_cycle` and `device_run`, a cycle or a device run is no candidate on request. CW-32's plug has a daily routine instead of a cycle (step 7 adds the cycle and the device run to the list). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 39 | What counts as watched, what as declined | **Watched**: an entity among the inputs of one of Mira's automations (`mira/automation/list`) made from a `mira/` blueprint: it is not offered again with any blueprint. **Declined**: per entity and blueprint (decision 21): a water meter declined for "never rests" is still offered for "runs on". Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 40 | How the request reaches the model | The tool `watch_candidates`, with the skill `aufgabe` as `device_profile` (decision 36); the skill's description names the question, so the model opens the skill for it, and its prose says how to answer (in words, without ids and blueprint names; the sensors not read too). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 41 | A sensor that cannot be read on request (CW-35) | All power sensors are read in one call (decision 13's costs): Home Assistant answers an id it has no statistics of with nothing, not with a refusal (checked on the test HA, 2026-11-13); such a sensor, and a candidate whose states are refused, is logged and named in the answer, the others are listed. A refusal of the one call names all power sensors as not read, with Home Assistant's answer; the water meters are listed. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 42 | How the model names the sensor to `device_profile` | By its id **or a name** Home Assistant knows for it (display name, alias, device name), found among the power sensors in W Mira may see, as the field check `entities` finds one; several of the same length: the error lists them. Without it the model guessed ids at the test Mira (`sensor.stromverbrauch` for `sensor.power_consumption`). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 43 | A daily routine's times the user corrects | Taken as the user says them: the code checks only that they read (decision 23), not that they lie within what the device profile saw (the user knows the device's timer; "the code checks they lie within what the device profile saw" holds for the numbers of a cycle and a device run, step 7). Without a routine in the profile the user's times are taken the same way (CW-17). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 44 | What the profile says is missing for a daily routine | "für einen Wächter seines Tagesablaufs fehlen die Zeiten", also when it found another kind or none: the model then asks the user for the times (the skill's prose). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 45 | Which numbers of a cycle and a device run the code takes (step 7, "The tool `device_profile`") | The code reads the device profile with the blueprint's kind (one read of the states) and refuses, back to the planner as every refusal of A3: a profile without enough (the answer names `watch_wait`), `max_on` or `max_off` not longer than the longest run or pause seen, `max_minutes` not longer than the longest run, `off_for` not longer than the longest pause within a run, `heat_above` not below the lowest peak of a run. Nothing above: the user may give a higher bound. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 46 | How a device run is followed (rows 14, 15) | **One run of the automation per run of the device** (`mode: single`, `max_exceeded: silent`; the assumption holds), with one detail found on the test HA: Home Assistant's `numeric_state` fires only on a crossing (an entity already within the range when the trigger is set up is not armed, `homeassistant/triggers/numeric_state.py`), so every wait is set up while the power is on the other side: while on, a wait for the power below `on_above`, above `heat_above` or the bound; while in a pause, a wait for the power above `on_above` with a timeout of `off_for`, whose end ends the run. No `for` within the run. A run that is on when the automation is loaded is not followed (the next is), as decision 31; a crossing above during a run leaves a trace "failed_single" in Home Assistant's traces, nothing in the log. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 47 | A cycle's "off" | `numeric_state` with `below: on_above`: a power exactly at `on_above` is neither on nor off (the spec said "at or below"); a cycle that is off when the watch is loaded is told only after it ran once (a crossing), as decision 31. Accepted. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 48 | Two entries of one blueprint | One input `message` (decision 24) whose template chooses by the trigger (`trigger.id` of the cycle) or the run's variable `what` (device run); the importance by the trigger, decision 18. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 49 | The selectors of blueprints 5 and 6 | `duty_cycle`: `on_above` 0–5000 W (default 5), `max_on` 1–1440 min, `max_off` 1–2880 min (no defaults: the numbers come from the device profile). `device_run`: `on_above` 0–5000 W (default 5), `off_for` 1–240 min (default 30, the profile's join of a programme's pauses), `max_minutes` 5–1440 min (no default), `heat_above` 0–5000 W (default 0: no check; "optional" of the spec). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 50 | Where the target `ueberwachung-anbieten` lives (decision 19: "in the skill `aufgabe`") | In a **skill of its own**, `skills/ueberwachung-anbieten` (on by default as `aufgabe`): a skill holds one target and the target is the skill's name. A **plan** target bound to the house (as the answering machine's), timeout 720 h (30 days); its fields `device`, `blueprint` and, after the run, `numbers` are the thread's fields written by the code (as a found device's `item`; one new method `threads.SetFields`), its plan "wait for the next run / offer the watch". Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 51 | How Mira promises (decision 14) | The tool **`watch_wait`** with the skill `aufgabe`: the model calls it when `device_profile` says runs are missing (the skill's prose); the code starts the offer's thread and subscribes (as `voicemail_list` starts its thread), not `thread_start`. A device whose offer waits already is said, not started twice; a profile that is enough says to make the watch now. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 52 | What the yes and the no do (decisions 11, 19) | The yes is `watch_accept`: the offer's thread ends as done, and a thread of `aufgabe` starts in the turn with its fields from the offer (`watch` the sensor, `measure` the blueprint and the numbers, `reaction` message); its drafting proposes the watch with its values (decision 29) and that yes saves it: two yeses, the offer's and the values'. The no is `watch_decline`: a row of `watch_declined`, the thread cancelled (its entry leaves). Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 53 | When a run counts as ended, and what follows | The subscription: `numeric_state` below 5 W (`on_above`'s default) for 30 minutes for a device run (the profile's join), 1 minute for a cycle or a daily routine. Then the device profile with the offer's kind; not enough: a log line, it waits for the next run; a device that Mira's watches watch meanwhile: the offer ends, with a log line; enough: the numbers, the entry, no more subscription. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 54 | The offer's entry | Kind `simple`, receiver the house, importance 1, source `task`, `dedupe_key` `watch-offer:<thread>`, the thread's expiry; spoken "Die Spülmaschine ist gelaufen. Wie versprochen: Soll ich es jetzt überwachen?", the details the device profile and the tools of the yes and the no. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 55 | Which subscription | Home Assistant's own `subscribe_trigger` (filtered at the source; admins only: Mira works with the owner's token, [HACS default](hacs-default.md)); not the service's `SubscribeTrigger`, which follows state triggers over `subscribe_entities` and knows no `numeric_state` with `for`. Refused (a token without admin): a log line naming the device; the thread waits and is followed again at the next start. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 56 | When the decline is forgotten (decision 21) | When a watch made from a blueprint is saved: the rows of the entities among its inputs with that blueprint. A failure is logged; the watch is saved all the same. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |
| 57 | A thread of the offer started by the model | `thread_start` refuses the target `ueberwachung-anbieten` (a list of targets only Mira's code starts, `threads.Options.CodeStarted`), naming the skill whose tools answer; an offer without a device ends at the service's start, with a log line. Found at the test Mira: after "Hab ich neue Nachrichten?" the voice model called `thread_start` for the offer instead of answering with its tools, which made an empty offer. The tools `watch_accept` and `watch_decline` take the device (`device`), which the entry's details name. Decided by Claude while building, 2026-11-13, to be confirmed by the maintainer |

## Who does what

| Step | Blueprint / code (fixed, checked) | Model (at run time) |
|---|---|---|
| How it is detected | The blueprint: triggers, conditions, durations, the message | — |
| Which device | The code lists candidates from the blueprint's entity selector (domain, device class, state class) among the entities exposed to Assist | chooses among several, asks when it is unclear |
| The inputs | The blueprint's selectors bound them; the code checks them against the selectors, Home Assistant checks that none is missing (`blueprint/substitute`, the save; decision 23) | turns the user's words into inputs ("nachts läuft zwei Stunden die Bewässerung" → a longer duration) |
| A daily routine | The device profile computes the windows from the last days | proposes them in words ("7:40 bis 11:40 und 14:20 bis 21:00") and takes the correction |
| Creating | The task's way as today: check, propose, save on yes | proposes in words, takes the yes |
| Telling | The automation calls `mira.send_message` with the facts (which meter, since when, its value) | words the message when it is read out and talks about it ("Ben ist in der Badewanne"; "mach's aus") |

## Building blocks

| Building block | Used for | Change | Reason |
|---|---|---|---|
| Tasks (`internal/tasks`, skill `aufgabe`, finisher `tasks.automation`) | A watch **is a task**: an automation labelled "Mira", made in conversation, listed, switched off and deleted as one | The drafting step offers the blueprints: the planner may answer with a blueprint and its inputs instead of an automation; the code checks the choice (A3), `blueprint/substitute` renders it, the rendered automation is checked as today; the proposal is built from the rendered automation and the saved inputs; a change of a watch drafts with the same blueprint (decision 30). The thread's draft keeps the blueprint, its inputs and the rendered automation (new optional fields of the draft). The skill's prose names the blueprints | One way to make a task; the blueprint only replaces the model's free draft where one fits |
| Saving (`automation.Save`, table `own_automations`) | Saves the automation `{use_blueprint: {path, input}}` with `POST /api/config/automation/config/<id>`, reload, label, row of kind `task` | None | A watch is a task; Home Assistant takes a blueprint's automation through the same call (checked 2026-11-13) |
| `mira/automation/validate` (integration) | Checks the **rendered** automation (the forbidden actions, as today) | None; the service sends the rendered config | No change of the interface between integration and service |
| `mira.send_message` (integration) | The blueprint's action: `text` with the facts, `importance`, `dedupe_key`, `expires_in`, `task: "{{ this.attributes.id }}"` | None | Already there for tasks |
| Threads (`internal/threads`, table `threads`) | The promise to wait for a run and the offer after it (decisions 11, 15): a thread of the house, its entry in a message, its yes drafting the watch | A target for the offer in a skill of its own, `ueberwachung-anbieten` (decision 50: a skill holds one target); an expiry of 30 days, as threads of the house already have an expiry of their own ([Messages](messages.md)); one method for the code to write the fields of a thread it started (`SetFields`: the numbers after the run) | A promise across turns is what a thread is; the offer comes the known way, as a found device |
| Messages, kind `simple` | A watch's text is an entry of the receiver's `simple` message | None | An entry is a text, as a task's; nothing to work through over several turns: the follow-up ("mach's aus") is the next turn of the same conversation, with Home Assistant's tools, no thread |
| **New: the blueprints** (`internal/watch`, blueprints embedded in the service) | The blueprint files; making sure Home Assistant has them; the catalog the planner sees (name, description, inputs) | New package | No building block holds blueprints. In the service, since the code and the model that fill a blueprint's inputs must match its version |
| **New: table `watch_declined`** (`internal/store`, a migration) | The devices whose offer the user declined, per blueprint | New table | A list of Mira's own with no place in Home Assistant (decision 21); by the rule of [Database](../database.md), a list gets a table, not keys of `kv` |
| Fake devices (test HA) | A water meter, a power plug with a daily routine, a fridge's plug in a cycle and a dishwasher's plug with a programme to drive the blueprints | Extended | The test HA has no water meter; [Fake devices](fake-devices.md) gets them |
| **New: the device profile** (in `internal/watch`, a tool for the drafting) | A device's kind (daily routine, cycle, run) and its numbers | New | No building block computes it: `state_history` condenses for reading (12 periods), a fridge has hundreds of runs in ten days |
| Recorder of Home Assistant (`history/history_during_period`, `recorder/statistics_during_period`) | The device profile's data: the states of the last days, the hourly statistics of months | None | Home Assistant's own, read for one entity at a time |

## The blueprints

Each is a blueprint in `blueprints/automation/mira/` of Home Assistant, `domain: automation`, embedded in the service
(`internal/watch/blueprints/`), with `mira-version: N` in its description. The text of the entry is the input
`message`, which the service fills in the house's language with the facts as templates (decision 24); the model words
it when it is read out. Every entry goes to the house (`receiver: house`) with `dedupe_key: watch:<automation id>` (a
new entry of the same watch replaces the old), `expires_in: 12h`, `task: <automation id>` and the importance of
decision 18. The bounds below are the selectors' and the code's check (decision 23).

### 1. Water runs on (`water_runs_on`)

After the maintainer's automation "Dauerdurchfluss", without its timer helper.

- **Inputs:** `meter` (sensor, device class `water`), `gap` (minutes, 1 to 60, default 11: a meter that reports every
  5 minutes counts as running while it changed within this), `duration` (minutes, 5 to 1440, default 30; longer than
  `gap`).
- **Trigger:** a template trigger, true while the meter changed within `gap` (`now() - last_changed`), held for
  `duration`. Home Assistant renders it on every change of the meter and once a minute (it uses `now()`).
- **Entry:** "Um {time} Uhr lief seit {duration} Minuten Wasser ohne Pause. Zählerstand: {state} {unit}."
- Once per streak: the trigger fires again only after the template was false (the water stopped for `gap`). A streak
  running while the watch is made is not told (decision 31).

### 2. Water never rests (`water_never_rests`)

After the maintainer's automation "Schleichender Verbrauch" (a dripping tap, a toilet that runs on), without its
timer helper.

- **Inputs:** `meter` (as above), `rest` (hours, 0.25 to 12, default 1), `window` (hours, 2 to 168, default 24).
- **Way:** `mode: restart`. Triggers: the meter unchanged for `rest` (a state trigger with `to: null` and `for`: a
  rest; the run starts again), Home Assistant started, the automations loaded (`automation_reloaded`). The run, while
  the meter rests, first waits for its next change (the window starts when the rest ends, decision 25), then waits
  `window`, sends the entry unless the meter rests right then, and repeats (a daily reminder while it leaks).
- **Entry:** "Um {time} Uhr: Der Wasserzähler stand in den letzten {window} Stunden keine {rest} Stunden still.
  Vielleicht tropft ein Hahn oder die Spülung läuft nach. Zählerstand: {state} {unit}."
- A restart of Home Assistant or a reload of the automations starts the window anew: the entry comes later, never
  wrongly. Accepted (the timer helper would survive a restart, but the blueprint would need a helper Mira makes and
  owns: a second kind of object for little gain).

### 3. (moved)

"Lights left on" is a group watch: [Group watch](group-watch.md) (decision 20).

### 4. Follows its daily routine (`daily_routine`)

For a device whose power follows the same times every day (an aquarium's light, a pump, a heater on a timer).

- **Inputs:** `power` (sensor, device class `power`), `on_above` (watts, 0 to 5000, default 5), `windows` (text, the
  expected times on, e.g. `07:40-11:40, 14:20-21:00`; a window may pass midnight), `tolerance` (minutes, 1 to 240,
  default 15).
- **Trigger:** a template trigger, true while the power's being above `on_above` differs from the windows, held for
  `tolerance`. Rendered on every change of the power and once a minute. A power that is not a number is no deviation
  (decision 26).
- **Entry:** "Um {time} Uhr wich {name} von seinem Tagesablauf ab: {power} W, erwartet {an/aus}." ({an/aus} is the
  other of what the power shows: the trigger fired because they differ.)
- **Proposing the windows:** the device profile computes them from the last days (decision 16); the model proposes
  them in words, and the user's yes or correction fills `windows`. Where Home Assistant has a
  schedule for the device, the model may take its times instead (assumption: not looked at yet which schedules
  exist; the maintainer's aquarium has a switch "schedule" whose meaning is not checked).
- Example (the maintainer's aquarium light, 2026-11-11 to 13): on 07:41–11:41 and 14:21–21:01, about 50 W; on the
  12th off at 18:36 instead of 21:01. The blueprint would have said so at 18:51.

### 5. Cycle (`duty_cycle`)

For a device that switches on and off all day (a fridge, a freezer, a pump with a pressure switch).

- **Inputs:** `power` (sensor, device class `power`), `on_above` (watts, default 5), `max_on` (minutes, 1 to 1440:
  longer on is not normal), `max_off` (minutes, 1 to 2880: longer off is not normal); decision 49.
- **Triggers:** the power above `on_above` for `max_on` ("läuft seit {max_on} Minuten ohne Pause – Tür offen?",
  importance 1); the power below `on_above` for `max_off` ("läuft seit {max_off} Minuten nicht mehr – Strom weg oder
  defekt?", importance 2, decision 18). Plain numeric state triggers, each once per crossing (decision 47); one input
  `message` for both (decision 48).
- **Entry:** "Um {time} Uhr lief {name} seit {max_on} Minuten ohne Pause ({power} W). Ist eine Tür offen?" or "Um {time}
  Uhr lief {name} seit {max_off} Minuten nicht mehr. Ist der Strom weg oder ist es defekt?"
- **Numbers from the device profile:** the longest normal run and pause of the last days with a margin, proposed by the model
  in words; the code refuses numbers a normal run or pause reaches (decision 45). Example (the maintainer's freezer, 2026-11-03 to 13, its plug reporting about every 5 minutes): 127 runs
  of 55 to 80 minutes, about every two hours; the fridge 262 runs, most 15 to 20 minutes, once 200 minutes.

### 6. Device run (`device_run`)

For a device that runs a programme now and then (a dishwasher, a washing machine, a dryer).

- **Inputs:** `power`, `on_above` (watts, default 5: a run starts), `off_for` (minutes, 1 to 240, default 30, below
  `on_above` that end a run: longer than the programme's own pauses), `max_minutes` (5 to 1440: a run longer than this
  is not normal), `heat_above` (watts, default 0 for no check: a run without a peak above it did not heat); decision 49.
- **Entries** (importance 1): "Um {time} Uhr lief {name} schon {max_minutes} Minuten, länger als sonst." "Um {time} Uhr
  war {name} nach {minutes} Minuten fertig, hat aber nicht aufgeheizt (nie über {heat_above} W)." One input `message`
  for both (decision 48).
- **Way** (tried on the test HA, rows 14 and 15; decision 46): one automation run per device run, `mode: single`.
  The trigger: the power above `on_above`. The run repeats, while on: wait for the power below `on_above`, above
  `heat_above` (it heated) or the bound (`max_minutes` from the start: the first entry, once); while in a pause: wait
  for the power above `on_above`, at most `off_for`, after which the run ended. At its end without heat the second
  entry. Each wait is set up while the power is on the other side of what it waits for (Home Assistant's
  `numeric_state` fires on a crossing only).
- **Numbers from the device profile:** duration, longest pause within a run and peak of the runs of the last days; the
  hourly statistics only find it as a candidate (the maintainer's dishwasher: 174 runs since 2022 with an hourly
  peak above 500 W; one run in the last ten days, 120 minutes, 1883 W). The code refuses numbers a normal run reaches
  (decision 45).

## The device profile

What it computes, for one power sensor:

1. **Candidate and kind** from the long-term statistics (`recorder/statistics_during_period`, hourly mean, min and
   max, the last 90 days; for the request the last 30): on at the same hours every day → daily routine; on in most
   hours, switching within them → cycle; on now and then with a high max → run. Under 5 W in every hour: no
   candidate. The bounds below (section "The bounds").
2. **Numbers** from the states of the last ten days (`history/history_during_period`, the one entity,
   `minimal_response`): the runs above `on_above` with start, duration, the pause before and the peak; per kind: the
   windows (daily routine), the longest normal run and pause (cycle), duration, longest pause in a run and peak (run).
3. **Enough?** A daily routine needs 3 days that follow its windows, a cycle states of 2 days with a whole run, a run
   one whole run within the states (decision 33; a dishwasher running once a week would never have three in ten days;
   the hourly statistics of months show whether that run's length was typical). Not enough: the answer says it and
   what is missing; the candidate waits (decision 14).

### The bounds

Tried read-only on the maintainer's house on 2026-11-13 (the recorder's database opened read-only: the hourly
statistics of the last 30 and 90 days of all 119 sensors in W with statistics, the states of the last ten days of the freezer, the
fridge, the dishwasher, the washing machine, the aquarium's light and a few others). An hour counts as **on** when its
max is above 5 W, as **switching** when it is on and its min at or below 5 W. In this order:

| Kind | Rule | What it found in the maintainer's house |
|---|---|---|
| no device | an hourly min below −5 W (noise around 0 W is off: a wallbox read −0.1 W) | the grid's, the house's and a battery's power, a wallbox's phases (14 sensors) |
| no candidate | no hour on | lamps, plugs that were off |
| daily routine | every hour of the day is on on at least 90 % of the days or off on at least 90 %, with at least one of each (and 3 days of each hour) | the aquarium's light and pump, the ventilation (on 8 to 22 h), an air purifier on a timer; not the solar power (its first and last hours of daylight change with the days and the weather: 3 to 6 hours neither) |
| cycle | on in at least 90 % of the hours, switching in at least 50 % | the freezer (on in 100 %, switching in 62 to 90 %), the fridge (100 %, 100 %); also a media plug in standby (a nuisance, accepted); not a constant load (a router, a heating pump: switching 0 %) |
| device run | on in at most 20 % of the hours, a max of at least 500 W in one of them | the dishwasher (on 2 %, 1960 W), the washing machine (1 %, 2391 W), the wallbox's charging power; also a lamp and an air conditioner with a single glitch of 1.3 and 21 kW (no run in their states: they wait, a nuisance) |
| none of these | | lights now and then, a kitchen hood, an ice maker |

The numbers from the states (a plug reporting every 5 minutes): a **run** is a time above 5 W; a **device run** joins
runs whose pause is shorter than 30 minutes (a programme's own pauses: the dishwasher's run of 2026-11-13 had none
below 5 W, then a blip of 13 W 15 minutes after its end, which it joins: 140 minutes instead of 120). A **daily
routine's windows**: per minute of the day, on when the device was on at that minute on more than half of the full
days; gaps shorter than 15 minutes (the blueprint's default tolerance) joined, windows shorter than 15 minutes
dropped; a day **follows** them when it differs by at most 60 minutes (the aquarium's light: windows 07:42–11:42 and
14:22–21:02, 8 of 9 days; the 12th, off at 18:36 instead of 21:01, differed by 147 minutes). A **cycle**: the freezer
121 whole runs, mostly 65 minutes, the longest 80, pauses mostly and at most 55 minutes; the fridge 250, mostly 20
minutes, the longest 200, pauses at most 40.

### The tool `device_profile`

The model's way to a device profile (decision 36): `device_profile` with a power sensor's id, offered in a turn that
opened the skill `aufgabe`. Step by step: the entity's state (is it one Mira may see, a power sensor in W?), the
hourly statistics of the last 90 days (`recorder/statistics_during_period`, the one id), the states of the last ten
days (`history/history_during_period`, the one entity, `minimal_response`, `no_attributes`); computed in the service.
It answers in words: the kind, the numbers (a daily routine's windows with the days that follow them and the usual
power; a cycle's runs and pauses, the usual and the longest; a device run's runs with start, length, peak and longest
pause within), and whether they are enough for a watch, else what is missing. Its errors: an entity Mira may not see,
a sensor that is not a power sensor in W, the recorder's refusal or an answer with nothing for the sensor (each
naming the sensor and Home Assistant's answer); decision 17 for the reads (a refusal is not tried again, a lost
connection is).

The model gets the numbers and proposes the inputs in words; the code checks they lie beyond what the device profile saw as normal (a cycle's and a device run's, decision 45: one read of the states; a daily routine's times are the user's, decision 43).

### Waiting for the next run (decision 14)

The promise is a thread of the house (decision 15), waiting; its fields name the device and the blueprint
(decisions 50, 51). The model makes it with the tool `watch_wait` (skill `aufgabe`) when `device_profile` says runs
are missing, and says it: "Die Spülmaschine nehm ich in die Überwachung, wenn sie das nächste Mal gelaufen ist."
Step by step:

1. `watch_wait` with the power sensor (id or name, as `device_profile`): the device profile (a state, two reads of the
   recorder); a profile that is enough or no candidate is said instead, an offer that waits already is said.
2. The thread of `ueberwachung-anbieten` at the house, its plan and fields, expiring in 30 days; the subscription
   (`subscribe_trigger` with a `numeric_state` trigger on the power, below 5 W for 30 minutes for a device run, 1
   minute else, filtered at the source; decisions 53, 55). Its handler only passes the entity on.
3. When a run ended: the device profile again with the offer's kind; with enough, its numbers into the thread's
   field, the entry of the house's simple message leading to the thread (decision 54), no more subscription; the
   thread waits for the yes. Not enough: it waits for the next run.
4. The yes (`watch_accept`): the offer ends, a thread of `aufgabe` drafts the watch in the turn (decision 52). The no
   (`watch_decline`): the device is kept as declined, the thread ends.

At start the service subscribes again for every waiting thread (an entity Home Assistant no longer has ends its
thread). A thread that expires before the device ran ends its subscription with a log line (the device and the days
waited); nothing is said, as for every expired thread.

## What it does, step by step

### A. Making a watch (in conversation)

1. The skill `aufgabe` collects the wish as today (`watch`, `when`, `measure`, `reaction`).
2. **Blueprints ready** (once per start of the service, once Home Assistant is connected, and at the next drafting
   while one is not ready, decision 28): `blueprint/list` for the domain `automation`; every embedded blueprint that is
   missing or differs is saved with `blueprint/save` (path `mira/<name>`, `allow_override` when it differs, also when
   Home Assistant lists the file with an error). The blueprints carry a version in their description
   (`mira-version: N`); "differs" compares it.
3. **Drafting:** the planner gets, besides what it gets today, the catalog of the blueprints ready (name, what it
   watches, its inputs with their bounds and defaults, without `message`). It answers either with a blueprint and its
   inputs (`{"title", "summary", "blueprint", "inputs"}`), or with an automation as today.
   **Check of the choice (A3, code):** decision 23; the input `message` is set (decision 24).
4. **Check of a blueprint's draft:** `blueprint/substitute` with the inputs (Home Assistant renders the automation and
   refuses a missing input); the rendered automation goes through `mira/automation/validate` as today. A refusal of A3
   or A4 goes back to the planner once with the reason, as today.
5. **Proposal:** from the rendered automation, as today ("Wenn der Wasserzähler eine halbe Stunde ohne Pause läuft,
   sag ich Bescheid."), with the values (decision 29). On yes: the rendered automation is checked once more by the
   integration and the task saved (`{alias, description, use_blueprint: {path, input}}`).

### B. Telling

1. Home Assistant's trigger fires; the automation calls `mira.send_message` with the entry.
2. The message is a `simple` entry of the receiver's message, as every task's text: announced, ring, screen, "Hab
   ich neue Nachrichten?".
3. Read out, the model words it; what the user answers is the next turn ("mach's aus", "Ben badet", "lass gut sein").

### C. Changing, switching off, deleting

As every task (`tasks-editing`): the inputs are changed by drafting again with the same blueprint; "Pass nicht mehr aufs
Wasser auf" deletes the automation (Mira's own, by its row). The blueprint stays.

### D. A blueprint changes (a new version of the service)

At step A2 the changed blueprint is saved over the old (`allow_override`). Home Assistant reloads the automations
itself when a save replaced a file (decision 22): all automations of the house, once. A running wait of blueprint 2
starts anew (its trigger `automation_reloaded`).

## What it costs

- **Per watch in Home Assistant:** one automation. Blueprints 1 and 4 render a template on every change of their sensor
  and once a minute; blueprints 2, 5 and 6 are plain state triggers. Nothing in the Mira service runs until an entry comes.
- **Making a watch** (the turn waits for none of it; the finisher runs after the turn, as today): `blueprint/list` once
  per start and, the first time, one `blueprint/save` per blueprint; the planner's call as today; `blueprint/substitute`
  (milliseconds); the save as a task (four calls). For a blueprint of a power sensor one device profile (below).
- **An entry:** one `mira.send_message`; the model words it when it is read out, as every message.
- **A blueprint changed:** one `blueprint/save`; Home Assistant reloads its automations (all, once).
- **A device profile:** a state and two reads of the recorder for one entity: 90 days of hourly statistics (about 2,200
  rows) and ten days of states (a plug every 5 minutes: up to 3,000 rows; the maintainer's plugs report changes only:
  270 to 3,000). Computed in the service, milliseconds. Measured on the test HA (2026-11-13, the fake plug with 90 days
  of statistics and 2,900 states): 30 ms for both reads; the maintainer's recorder reads the states of one plug in 20
  to 140 ms (read-only, its database directly).
- **"Gibt's Geräte, die du überwachen könntest?"** (the turn waits; the tool `watch_candidates`): the states of the
  entities Mira may see (one `get_states` filtered to them, as for drafting), Mira's automations
  (`mira/automation/list`), the declined rows, one read of the hourly statistics of the last 30 days for all power
  sensors in W Mira may see in one call (`statistic_ids` as a list), the kinds computed from it, then the states of
  the last ten days of each candidate whose blueprint is ready (one `history/history_during_period` each). Grows with
  the number of power sensors (the statistics) and of candidates (one read of states each). Measured 2026-11-13: the
  maintainer's recorder reads the statistics of the last 30 days of its 157 sensors in W (63,764 rows) in 0.2 s
  (read-only, its database directly; an i5-4590T); the test HA answers the same call over the WebSocket for 119
  sensors with 30 days each (85,680 rows) in 0.24 to 0.87 s (a fast machine); the states of one plug take 20 to
  140 ms. So on the maintainer's house about 1 to 3 seconds (an estimate from these parts: Home Assistant's own work on
  its slower machine is not measured, no token of the house is used).
- **A cycle's or a device run's draft:** besides the above, one read of the sensor's states of the last ten days for
  the code's check (decision 45), as for a device profile.
- **A waiting candidate:** `watch_wait` reads one device profile (as `device_profile`) and starts a thread (a database
  write); then one trigger subscription per waiting entity, nothing else until its run ends; then one device profile,
  and with enough a few database writes (the numbers, the entry). At the service's start one state per waiting entity
  (is it still there?) and its subscription. The yes: the drafting of a watch, as above; the no: a database write.
- Grows with: the number of watches (one automation each), not with the house's size.

## Errors

| Step | Fails | Who learns of it |
|---|---|---|
| A2 | `blueprint/list` or `blueprint/save` refused (not an admin's token, a write error) | A log line with the blueprint's path and the answer (`could not list home assistant's blueprints` / `could not save a blueprint`); the blueprint is left out of the planner's catalog (decision 28), so no watch is drafted with a missing blueprint; free drafts work as before (without an admin's token their save says "Mira braucht erst wieder einen Zugang" as for every task) |
| A3 | The planner names a blueprint that does not exist, or inputs it does not have | Refused by the code before Home Assistant is asked; back to the planner once with the reason, as a refused draft today; then the tool's error |
| A4 | `blueprint/substitute` refuses (missing or wrong input) | Back to the planner once with Home Assistant's answer; then the tool's error naming the blueprint and the answer |
| A5 | The save fails | As a task's save today (hacs-default, section 3) |
| B1 | `mira.send_message` fails | Home Assistant's automation trace shows the error; the integration logs it as today. Nothing in the service (it never got the call) |
| D | Saving the changed blueprint fails | A log warning with the path and the answer; the old blueprint stays in use; tried again at the next start or drafting |
| Device profile | The recorder refuses or answers with nothing for the entity | The tool's error with the entity and Home Assistant's answer; for the request a log line, and the answer names the sensors it could not read |
| Device profile | Not enough history | Not an error: the answer says what is missing; the candidate waits (decision 14) |
| Request | The states of the entities Mira may see, Mira's automations or the declined rows cannot be read | The tool's error with what failed and Home Assistant's answer (or the database's); the model says it cannot list the candidates now |
| Request | The one statistics call is refused | Decision 41: a log warning with the number of sensors and the answer; the answer names the power sensors as not read, with the answer; the water meters are listed |
| Waiting | `watch_wait`: the sensor is not one Mira may see, not a power sensor in W, or its device profile cannot be read | The tool's error naming the sensor and Home Assistant's answer, as `device_profile`; nothing is started |
| Waiting | The offer's thread cannot be started | The tool's error naming the sensor and the database's answer |
| Waiting | The subscription for a waiting entity fails or the entity is gone | A log line with the entity; the thread stays waiting and the subscription is tried again at the next start; for a gone entity (at the start) the thread ends, with a log line |
| Waiting | After a run the device profile cannot be computed, or is still not enough | A log line with the entity (and the answer); the thread waits for the next run |
| Waiting | After a run the thread cannot be written, or the entry cannot be added | A log line with the entity and the thread; not written: the next run tries again; no entry: the thread waits with its numbers, found on the Plans tab ([Messages](messages.md), errors) |
| Waiting | A run's end comes while 16 others wait to be handled | A log line with the entity; it is dropped, the next run counts (the subscription's handler never blocks) |
| Waiting | The thread expires before the device ran | Its subscription ends with a log line (the device, the days waited); nothing is said |
| Offer | `watch_decline`: the decline cannot be written | The tool's error naming the device, the blueprint and the database's answer; the thread stays |
| Offer | `watch_accept` while the thread still waits for the run | The tool's answer says so; nothing changes |
| A5 | The decline of a watch made by voice cannot be deleted | A log warning with the entity and the blueprint; the watch is saved |
| — | A blueprint's device is gone (deleted, renamed) | Home Assistant's own: the automation's trigger finds no entity; its repair / the automation's state "unavailable". Mira's task list shows the automation as today; nothing of Mira's own |

## Checks on the test HA

| # | Transition | Trigger | Signal | Checked |
|---|---|---|---|---|
| 1 | Mira saves a blueprint | `blueprint/save` (admin) | `success`, `overrides_existing: false`; listed by `blueprint/list` | 2026-11-13, 2026.11.0 |
| 2 | Saving again without override | `blueprint/save` | error `already_exists` | 2026-11-13 |
| 3 | Rendering with inputs | `blueprint/substitute` | `substituted_config` with the inputs in place | 2026-11-13 |
| 4 | An automation from the blueprint | `POST /api/config/automation/config/<id>` with `use_blueprint`, then `automation.reload` with the id | `{"result":"ok"}`; the entity `automation.<alias>` is `on` with the id | 2026-11-13 |
| 5 | A missing input | the same `POST` without the input | HTTP 400 "Message malformed: Missing input meter"; nothing saved | 2026-11-13 |
| 6 | Blueprint 1's template trigger with an input, rendered each minute | a fake meter changing every 5 minutes for 30 minutes (shortened: gap 2, duration 5, a change every minute) | the automation's trace fires once at minute 30 (shortened: 5 minutes after the first change, to the second); not again while the water runs; again after a stop longer than the gap (the template is rendered at the full minute: a stop counts once it lasted the gap at a minute's render) | 2026-11-13, 2026.11.1 |
| 7 | Blueprint 2's restart and wait | a rest of the fake meter; then none for the window (shortened: rest 0.05 h, window 0.1 h) | the trace: sends after the window and repeats; on the rest the run restarts (trigger "state of sensor"), waits for the next change, then the window anew and sends after it, to the second | 2026-11-13, 2026.11.1 |
| 8 | Blueprint 2 starts its window when loaded and at Home Assistant's start | reload / restart of the test HA | a running run in the trace: trigger "event 'automation_reloaded'" after `automation.reload` with its id (also after the reload of another automation, and after a blueprint was saved over); trigger "Home Assistant starting" after `docker restart` | 2026-11-13, 2026.11.1 |
| 10 | Blueprint 4 | a fake plug's power leaving its window (fake routine `18:00-23:00`, 50 W; tolerance 1 minute; power set to 0) | the entry after the tolerance, to the second ("erwartet an") | 2026-11-13, 2026.11.1 |
| 11 | The entry reaches Mira | the automation's `mira.send_message` with `task` | a `simple` entry of the house's message, source `task`, `dedupe_key` `watch:<id>`; the next entry of the same watch replaces it; read out at the fake Show ("Hab ich neue Nachrichten?") | 2026-11-13, 2026.11.1 |
| 12 | A changed blueprint reaches its automations | `blueprint/save` with `allow_override` (decision 22: nothing else) | `overrides_existing: true`; Home Assistant reloads the automations at once (a running wait of blueprint 2 starts anew: "event 'automation_reloaded'"); the trace of the next run shows the new version (importance 1 instead of 2); the service's start then saves its version back ("blueprint updated", from 0 to 1) | 2026-11-13, 2026.11.1 |
| 12a | A blueprint in use cannot be deleted | `blueprint/delete` while an automation uses it | error `home_assistant_error` "Blueprint in use" | 2026-11-13, 2026.11.1 |
| 13 | Blueprint 5 | the fake fridge (`watch_fake.cycle`, 80 s on, 80 s off; `max_on` 1, `max_off` 1) | the trace: one entry 60 s after on ("had run for 1 minutes", importance 1), one 60 s after off ("had not run", importance 2), to the second; not again while it stays off | 2026-11-13, 2026.11.1 |
| 14 | Blueprint 6: a run too long | the fake dishwasher (`watch_fake.program` `200:80, 30:0, 200:80`; `off_for` 1, `max_minutes` 5, `heat_above` 1500) | one run of the automation for the device run (the crossing after the pause: a trace "failed_single"); the entry at 5 minutes, to the second | 2026-11-13, 2026.11.1 |
| 15 | Blueprint 6: a run without heat | the same run, ending; then a run with 60 s at 2000 W | the entry 60 s (`off_for`) after the power dropped, "a run of 7 minutes, but did not heat"; after the run with heat no entry | 2026-11-13, 2026.11.1 |
| 16 | The end of a waiting candidate's run reaches the service | `subscribe_trigger` with `numeric_state` below 5 for 5 s on the fake aquarium plug (admin token); the plug 5 s at 60 W | the trigger's event 5 s after the drop, with `from_state` 60.0 and `to_state` 0.0 | 2026-11-13, 2026.11.1 |

Rows 6 to 12 are checked before the step that builds on them.

## Steps

1. **Blueprints in Home Assistant:** the package `internal/watch` with the blueprints of blueprints 1, 2 and 4 embedded
   (blueprints 5 and 6 come with step 7); A2 and D; the
   catalog. Tests: saving missing and changed blueprints, nothing for unchanged ones, the errors. Built 2026-11-13
   (CW-01 to CW-04, CW-31 in Go); A2 at the service's start once Home Assistant is connected (`cmd/mira`).
2. **Drafting with blueprints:** the planner's catalog and answer, the check (A4), the proposal, the save as a task;
   the skill's prose. Tests against the fake: a wish becomes a watch from a blueprint, a wrong input is said, a wish
   without a blueprint is drafted as before. Built 2026-11-13 (CW-05 to CW-10, CW-21 in Go); the tools `task_enable`
   and `task_delete` name a watch in their descriptions (without it "Pass wieder aufs Wasser auf" started a new task at
   the test Mira).
3. **The fakes and the runs:** a fake water meter and a fake plug with a daily routine in the test HA; rows 6 to 11
   checked; each blueprint run end to end (entry, read out, follow-up); the table `watch_declined` with its migration
   comes with step 5. Built 2026-11-13: `testenv/watch_fake` ([Fake devices](fake-devices.md), The watched devices);
   CW-11 to CW-15 in pytest (`tests/ha/test_watch_blueprints.py`, the blueprints with the fakes and the time moved on);
   rows 6 to 12 checked on the test HA; at the test Mira by voice at the fake Show: "Pass auf, ob bei uns Wasser
   dauernd läuft" made the watch from `water_runs_on`, the entries were read out, the watch was paused, switched on
   and deleted by voice.
4. **The device profile:** the tool, the kinds and the numbers, the daily routine's windows included; its bounds tried
   read-only on the maintainer's devices first. Built 2026-11-13 (decisions 33 to 37): `internal/watch` (`KindOf`,
   `Compute`, `Profiler`, the tool `device_profile` with the skill `aufgabe`), the bounds above; CW-23 to CW-25 in Go
   with synthetic series (and the kinds, the windows through midnight, a day off, states not a number). Run against the
   test HA's recorder: the fake plug's past written by `watch_fake.past` (90 days of statistics, ten days of states of
   a daily routine) gave its windows, 9 of 9 days; a plug never on "no candidate"; a sensor without statistics the
   error naming it.
5. **Offering on request:** "Gibt's Geräte, die du überwachen könntest?": the candidates of all blueprints (water meters,
   power sensors by their kind), without the ones watched or declined. Built 2026-11-13 (decisions 38 to 41): the tool
   `watch_candidates` (`internal/tasks`, with the skill `aufgabe`), the table `watch_declined` (migration
   `0031_watch_declined`, read here, written by step 8), the skill's prose; CW-32 to CW-35 in Go; the cost measured
   (section "What it costs"). At the test Mira by the conversation API with the fake Show: "Gibt's Geräte, die du
   überwachen könntest?" named the water meter and the aquarium plug (whose past `watch_fake.past` wrote), not the
   plugs never on; with the plug declined (a row written by hand) only the water meter; with the water meter then
   watched ("sag Bescheid, wenn das Wasser eine halbe Stunde ohne Pause läuft", made from `water_runs_on`) "Ich finde
   im Moment keine neuen Geräte"; the watch deleted by voice after. The model opened the skill by starting a thread of
   `aufgabe` for the question (it then took the yes for the water meter in the same thread). CW-35 ran in Go only:
   the test HA has no power sensor without statistics.
6. **Daily routine from the device profile:** the model proposes the windows; the user corrects them. Built 2026-11-13
   (decisions 42 to 44): the skill's prose (`device_profile` for a device with fixed times, the times in words, the
   correction taken, the times into `measure`; without a routine: say so and ask for the times, no `assess` instead),
   `device_profile` by name; CW-16 and CW-17 in Go. At the test Mira (the fake plug's past from `watch_fake.past`):
   "Achte aufs Aquarium" – "Das Aquarium läuft laut Profil täglich von 7:40 bis 11:40 und von 14:20 bis 21:00 Uhr.
   Soll ich mich melden, wenn es davon abweicht?" – "Fast, nachmittags geht es erst um halb drei an." – the proposal
   with 14:30, saved from `daily_routine` with the windows `07:40-11:40, 14:30-21:00`, deleted by voice after. CW-17
   ("Pass auf, ob der Stromverbrauch seinen Tagesablauf einhält", a sensor at a constant 100 W): with the model
   `smart` "Für den Stromverbrauch konnte ich keinen festen Tagesablauf erkennen. Zu welchen Uhrzeiten …?", the times
   given then drafted `daily_routine` with `08:00-18:00`; with the test Mira's voice model Flash Lite the model ignored
   the prose twice and drafted a task that judges itself (`assess`): open for the maintainer (the voice model).
7. **Cycle and device run:** the two blueprints, the fakes, rows 13 to 15. Built 2026-11-13 (decisions 45 to 49):
   `duty_cycle` and `device_run` in `internal/watch/blueprints` with their texts, the code's check of their numbers
   against the device profile (`CheckNumbers`), so the candidates and the device profile now offer cycles and device
   runs; the fakes' fridge and dishwasher (`watch_fake.cycle`, `watch_fake.program`, `watch_fake.past` with a cycle
   and device runs); CW-26 to CW-29 in pytest; rows 13 to 15 checked. At the test Mira by the conversation API with
   the fake Show (the fridge's past from `watch_fake.past`, 20 minutes on, 40 off): "Pass auf den Kühlschrank auf" –
   "Ich behalte den Kühlschrank im Auge und melde mich, wenn er länger als 25 Minuten läuft oder länger als 50
   Minuten pausiert. Soll ich das so einrichten?" – "Ja": saved from `duty_cycle` (`max_on` 25, `max_off` 50).
8. **Waiting for the next run:** the thread of the promise, the subscription, the offer after the run; row 16. Built
   2026-11-13 (decisions 50 to 57): the skill `ueberwachung-anbieten`, `watch.Waiter` with the tools `watch_wait`,
   `watch_accept`, `watch_decline`, the offers' threads and entries (`cmd/mira/watchoffers.go`), the decline forgotten
   when a watch is saved; CW-30 and CW-36 in Go; row 16 checked. At the test Mira by the conversation API with the
   fake Show (the dishwasher's past from `watch_fake.past` with `peak_every`, no run in its states): "Pass auf die
   Spülmaschine auf" – "Die Spülmaschine nehm ich in die Überwachung, wenn sie das nächste Mal gelaufen ist."
   (`device_profile`, `watch_wait`, the started `aufgabe` thread cancelled); a restart of the service followed it
   again; a run of 8 minutes (`watch_fake.program`) ended, and 30 minutes after it, to the second, the offer came as
   an entry; "Hab ich neue Nachrichten?" – "Die Spülmaschine ist fertig. Soll ich sie in Zukunft für dich
   überwachen?" – "Ja, gerne": `watch_accept` drafted the watch, saved after the proposal's yes from `device_run`
   (`max_minutes` 30, `heat_above` 1500 for a run of 8 minutes at up to 2000 W), deleted by voice after.
9. **The guide**, German and English: watching, the blueprints, asking for candidates, pausing.
