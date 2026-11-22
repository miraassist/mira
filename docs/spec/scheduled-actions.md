# Scheduled actions (sleep timer): spec

Status 2026-10-18: built in 0.6.45. Decided with the user: The state and the scheduler live in
**Home Assistant**; reminders stay with Mira for now. One-off automations carry their own **label**; the
**cleanup is done by the integration** (since 0.18 by the service: [HACS default catalog](hacs-default.md), 3).
Since 2026-11-22 the service builds the automation and the integration only checks it (Creating, changing,
cancelling).

## Target

"Schlaf-Timer 30 Minuten." – "Schalte in einer Stunde das Licht aus." – "Noch zehn Minuten." – "Doch abbrechen."

Mira should **do something** at a time, not just say something: stop playback (audiobook, radio, music), on
request fade out slowly, and switch off the light in the room. The same carries scenes with a delay ("Heimkino in
fünf Minuten"). See [skill ideas](skill-ideas.md).

## Distinction from reminders

| | Reminder / timer ("erinnere mich in einer Minute an die Wäsche") | Scheduled action (this paper) |
|---|---|---|
| What happens at the time | Mira **says** something | HA **executes actions** |
| Where it lives | Mira's database (`reminder_set`, scheduled message) | automation in Home Assistant |
| Who triggers | Mira's notifier | Home Assistant |
| Runs when the other side is off | no (Mira must be running) | yes (Mira need not be running) |

Both stay side by side. "Welche Timer habe ich?" names both: the sleep timers and Home Assistant's countdown timers.

### "Welche Timer habe ich?" (decided 2026-11-22, the maintainer)

Asked for timers, the model calls Home Assistant's `HassTimerStatus` and, despite the description of `sleep_timer`,
not `sleep_timer` list (seen 2026-11-22 with flash lite: a sleep timer of 20 minutes was left out). So the service
does not leave it to the model: to every answer of `HassTimerStatus`, an error too (Home Assistant has no countdown
timer), it adds the open sleep timers of the person in the room (anonymous: of the room), one line each:
"Außerdem Schlaf-Timer (sleep_timer): endet um 22:35, in 20 Minuten, Raum Wohnzimmer". With none it adds nothing.

- **What it costs**: one `mira/automation/list` (label `once`) per call of `HassTimerStatus`; the turn waits for it.
- **When reading fails**: the line says that the sleep timers could not be read, with the error, and the log names
  it; Home Assistant's answer stays as it is.
- Only `HassTimerStatus` of Home Assistant's Assist server; no other tool's result is changed.

## Countdown timers are a third thing

"Timer auf zehn Minuten für die Nudeln" (a countdown timer, Kurzzeit-Timer, [Glossary](../glossary.md)) Mira does
**not** rebuild. Those are Home Assistant's timers
(`HassStartTimer`, `HassCancelTimer`, `HassTimerStatus`, `HassIncreaseTimer`, `HassDecreaseTimer`, `HassPauseTimer`,
`HassUnpauseTimer`): The satellite that gets them counts down, shows the rest and rings (TECHO5 does that on
Show as on Echo; the Show additionally has the screen). Mira uses these tools herself, just as HA gives them to its
own conversation agents.

That they reach Mira comes down to one line: HA's MCP server builds the tool list **for a device** if the
request carries its ID in the metadata (`_meta` with `io.home-assistant/device_id`). Without a device the
timer tools are missing (26 instead of 33 measured), and HA does not prefer the satellite's room for commands without a room. Mira
therefore sends the satellite's ID with **every** call of an HA tool in a voice conversation
(`device_context`, switched on for HA's Assist server) and queries the list once without and once as each satellite
and merges them, so that she lacks nothing a satellite is offered. The list is fetched again every five minutes and on
every change of the satellites, because HA does not report that a satellite with timers came back.

The rule behind it: **What HA offers, we do not rebuild**, not even to save tokens. And we do not let tools
disappear because the context (here the device) does not come along. Whoever ever decides otherwise does so deliberately.

Nothing is stored at Mira. Without satellites (Telegram) HA knows no timers, and Mira says so honestly. Home Assistant's
timers live in Home Assistant's memory and do not survive its restart.

So there are three things: the countdown timer (HA, on the satellite), the reminder (Mira says a text) and the
sleep timer (HA automation, does something).

## The automation

A **one-off automation** is an ordinary HA automation with the label **"Mira: einmalig"**
(`mira_once`, description "Läuft einmal und räumt sich dann auf"). It does **not** carry the label "Mira" of
[tasks](tasks.md), so that it does not appear there in the task list.

```yaml
alias: "Schlaf-Timer Wohnzimmer 837deda7"   # the end is different for each timer, see below
description: "Mira: Schlaf-Timer Wohnzimmer"
variables:                      # what the integration reads on cleanup and Mira on listing
  mira_title: "Schlaf-Timer Wohnzimmer"
  mira_due: "2026-10-18T22:35:00+02:00"
  mira_owner: anna
  mira_area: wohnzimmer
  mira_kind: sleep
triggers:
  - trigger: time
    at: "22:35:00"
  - trigger: homeassistant      # if HA was off when due: catch up now
    event: start
conditions:                     # not before due, only once
  - condition: template
    value_template: >-
      {{ now() >= as_datetime('2026-10-18T22:35:00+02:00') - timedelta(seconds=2)
         and this.attributes.last_triggered is none }}
actions:
  - action: media_player.volume_set     # fade out: several steps with waiting
    ...
  - action: media_player.media_stop
    target: { entity_id: media_player.show_speaker }
  - action: light.turn_off
    target: { area_id: wohnzimmer }
mode: single
```

- **Triggers:** a time to the second, plus the start of Home Assistant. That is enough even across midnight
  and for times more than 24 hours ahead. A template trigger with `now()` would only be accurate to the minute.
- **Condition: not before due and only once.** `now() >= mira_due` (two seconds of slack for the clock)
  and never triggered yet (`last_triggered` is empty). If HA is off when due, the start of HA triggers the
  automation, and it is caught up: otherwise the light burns all night. There is **no upper limit**
  and no expiry; what is due and has not run yet runs as soon as HA is back.
- **Each timer has its own name.** HA makes the entity ID from the name and restores the state of a
  new entity from the last one with the same ID, even from a deleted one. A second "Schlaf-Timer
  Wohnzimmer" would have inherited "already ran" from the first and never triggered. The test against a real HA
  showed that. That is why the name appends an ID (the last eight characters of the automation ID). The readable title
  is in `mira_title`.
- **Due time and owner** are in `variables`. That is structured, survives changes in HA's editor and
  needs no storage of its own.
- **"Already ran?"** the integration reads from the automation itself: `last_triggered` and the number of running
  runs (`current`). Ran and still executing (e.g. while fading out) is not finished.

## Cleanup by the service

Since 0.18 the service tidies up ([HACS default catalog](hacs-default.md), 3): when a timer's state changes (a
subscription filtered to the timers' entities), at its start and after every reconnect. Before, the integration did it
every minute.

For each automation with the label `mira_once`:

1. **Ran and finished** (`last_triggered` set, no running runs): delete.
2. **Everything else:** do nothing. Due and not run is caught up by the automation itself (start of HA),
   what is not yet due waits. Whoever switches off a timer in HA keeps it until they delete it themselves.

The catching up is thus in the automation itself and not in the sweep. The sweep only cleans up.

If someone changes `mira_due` in the editor without adjusting the trigger's time, the trigger's time still applies; HA's
editor is not meant for that. Extending and cancelling happen in the conversation.

## Creating, changing, cancelling

The service builds, the integration checks, the service saves (since 2026-11-22, as the alarm: [Alarm](alarm.md),
decision 16 and step 3; before, from 0.18, the integration built it with `mira/once/build`):

- **Building** (the service, `internal/timer`): the automation above from title, due time, owner, area, kind and
  actions. Limit: **at most ten open timers per person and room**, counted by the service from the list. No others,
  neither in time nor in duration. A due time in the past or no action: nothing is built, the tool says why.
- **Checking**: `mira/automation/validate` (admins only, `check_actions`, rejecting when in doubt), as for a task; the
  answer `valid: false` with its error ends it, the tool says the error and the log names it.
- **Critical actions** (locks, alarm system, garage door, entities with the label "Mira: nachfragen") are
  **forbidden** in scheduled actions: A confirmation from this evening should open nothing at three in the night.
- **Listing**: `mira/automation/list` with `label: once` (admins only), Mira's automations labelled "Mira: einmalig"
  with their config; the service reads the variables. Cancelling: the service deletes through the API.
- **Extending** rewrites `at`, the date condition and `mira_due`, under the same id; `validate` gets the id, so the
  timer's unchanged actions are not checked again.

### Where the state lives (2026-11-22, when the building moved to the service)

| State | Where, how long | Before | Now |
|---|---|---|---|
| The timer (due time, person, room, kind, actions) | the automation in Home Assistant, labelled "Mira: einmalig", until it ran | built by `mira/once/build` | built by the service, checked by `mira/automation/validate` |
| Extending | the same automation, the same id | `mira/once/build` with `automation_id` | `validate` with `automation_id` |
| At most ten per person and room | the list | the integration | the service |
| Listing, tidying up, taking over timers from before the service's note | the label | `mira/once/list` | `mira/automation/list` with `label: once` |
| Flows across turns | none: listing, extending and cancelling read the timers from Home Assistant each time; no thread | — | the same |

## Tools in the service

First only the case one really needs (no model writes actions):

- **`sleep_timer(action, minutes?, room?, lights_off?, id?)`** (one tool, `action`: `set` (default), `list`, `extend`, `cancel`):
  `set` is the sleep timer. Mira builds the actions herself: stop the players of the room
  (the exposed players in the area, as with `radio_play`), light in the room off. The room is the satellite's.
  `list` shows the person's sleep timers in the room with "how much longer".
  `extend` and `cancel` need no ID if the person has only one in the room; if there are several, Mira asks.

Later, after the first case: a general `action_schedule` with actions the model chooses from the exposed
entities ("Mach in einer Stunde das Licht aus"). The same check, the same limits.

## Open

- **Fading out:** lower the volume in steps (several actions with waiting). Only after the simple version.
- **The audiobook:** On stopping, the position should be kept. That is a matter for Music Assistant (see skill ideas), not
  the timer.
- **Reminders later in HA too?** Possible, but not necessary as long as they work. Missed reminders
  with HA running and Mira switched off Mira probably does not catch up today; that is not checked.
- **Without the integration** there are no scheduled actions, as with tasks.
