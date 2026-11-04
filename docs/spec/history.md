# Questions about the past – Spec

Status 2026-11-03: built (steps 1 to 3, in 0.15.2), checked end to end on the test HA. Scenarios: [history.feature](history.feature).

## Goal

Questions about what happened are among the most common at a satellite, and today Mira cannot answer them: Home
Assistant's tools over MCP only know the current state.

- "Wann war das Fenster im Bad heute offen?" – "Zweimal: von 7:12 bis 7:40 und von 18:05 bis 18:20."
- "Wie lange lief die Waschmaschine gestern?" – "Etwa eine Stunde vierzig, von 9:05 bis 10:45."
- "Wann wurde die Haustür zuletzt aufgeschlossen?" – "Heute um 6:58."

A history can be long (a motion sensor changes hundreds of times a day, a power meter every few seconds). The model
never sees it raw: Mira condenses it in code into a short answer with a fixed upper bound, and computes the durations
herself, since models add times badly.

## Decided (2026-11-03)

| # | Question | Decision |
|---|---|---|
| 1 | The concept | A tool of Mira's own (`state_history`), read-only. Nothing is rebuilt: Mira reads Home Assistant's recorder (its history and its statistics), which Home Assistant does not offer as a tool |
| 2 | Longest period | 31 days; for measurements the older part comes coarser, as in Home Assistant's history view (decision 7) |
| 3 | Measurements (power, temperature) | From Home Assistant's statistics, not the raw history; with an optional threshold (`above`) that turns them into periods ("über 5 W") |
| 4 | How the period is given | Fixed names (`today`, `yesterday`, `last_24h`, `last_7_days`, `last_31_days`) or `from`/`to` as points in time |
| 5 | Shortening | More than 12 periods: the first 3 and the last 6 are listed, with the totals and the count |
| 6 | Which entities | One per call, found by its name (or alias, device name, id) and optionally its area, as Home Assistant's intents find it (`ResolveName` of the house structure); only one exposed to Assist; any other is answered like one that does not exist. Several that fit are named with their areas. By name, because the house structure from Home Assistant's Assist prompt shows names, not ids (seen on the test HA 2026-11-03: the model guessed `sensor.außentemperatur` for `sensor.outside_temperature`); ids are found too |
| 7 | Older measurements | The 5-minute statistics where they reach, the hourly ones before; the answer names the resolution of each part, so the model can say it ("bis zum 24. nur stundenweise") |
| 8 | Older states | Not kept by Mira: the answer says from when the history is there; the guide explains how to keep it longer in Home Assistant (`recorder: purge_keep_days: 31`; its database grows accordingly) |

## The tool `state_history`

**Input:**

| Field | Meaning |
|---|---|
| `name` | the device or entity as the house structure names it: name, alias, device name or id |
| `area` | optionally the area, when the name is ambiguous |
| `period` | `today` (midnight until now), `yesterday`, `last_24h`, `last_7_days`, `last_31_days` |
| `from`, `to` | instead of `period`: points in time (ISO 8601, local time; a time of day alone is today); `to` defaults to now |
| `above` | measurements only, optional: a threshold; the answer becomes the periods above it |

Days follow Home Assistant's time zone. A period longer than 31 days, or one in the future, is refused with a short
reason, so the model can ask again with a valid one.

**Which entity:** the entity must be in the list Home Assistant exposes to Assist (the structure's exposed entities,
[HA integration](ha-integration.md)). Any other gets the same answer as an unknown one ("Diese Entität kenne ich
nicht"), so the tool does not confirm that it exists.

### States (lights, switches, doors, windows, locks, covers, players, persons, …)

Every entity without a `state_class` is treated by its states, except a sensor whose state is a number (see
Measurements, 5).

1. **Read:** one call `history/history_during_period` for this one entity, with `minimal_response`, `no_attributes`
   and `significant_changes_only: false` (tried on the test HA 2026-11-03 as Mira's non-admin user: it answers with
   `{"s": state, "lu": time}` per change, about 40 bytes each, and the first entry is the state at the start of the
   period).
2. **Condense:** consecutive equal states become one period with its start, end and duration. `unavailable` and
   `unknown` are periods of their own ("nicht erreichbar").
3. **The current state:** the recorder writes with a delay of a few seconds (seen on the test HA: the last switching
   was missing). The open last period ends with the live state, read from Home Assistant at the same time.
4. **Answer:** the entity's name, the period, the list of periods, and per state the total time and the count:

   > Fenster Bad, heute bis 19:30: zu bis 7:12 · offen 7:12–7:40 (28 min) · zu bis 18:05 · offen 18:05–18:20
   > (15 min) · zu seitdem. Zusammen: offen 43 min, 2-mal; zu 18 h 47 min.

5. **Shortening:** more than 12 periods: the first 3 and the last 6, between them "… 31 weitere …"; the totals and
   counts always cover the whole period. The answer stays under about 1,000 characters, whatever the history.
6. **Not recorded:** the recorder keeps the raw history 10 days by default. If the history begins later than the asked
   start, the answer says so ("Verlauf erst ab 24.10.") instead of treating the time before as one state. An entity
   the recorder leaves out has no history at all: "Dafür zeichnet Home Assistant keinen Verlauf auf."
   States have no statistics to fall back on: Home Assistant keeps long-term statistics only for entities with a
   `state_class` (on the test HA 2026-11-03: 251 statistics, all `sensor.*`), and its history view shows nothing older
   either. Whoever wants 31 days of states sets the recorder's `purge_keep_days` in Home Assistant (decision 8, the
   guide).

### Measurements (power, energy, temperature, …)

Every entity with a `state_class`: its raw history could hold tens of thousands of values a week.

1. **Read:** Home Assistant's statistics (`recorder/statistics_during_period`, already used by the tasks:
   `ws.Statistics`), as Home Assistant's history view does: the 5-minute statistics where they reach (Home Assistant
   keeps them 10 days by default), the hourly ones for the time before. The two parts meet where the 5-minute
   statistics begin; nothing is counted twice.
2. **The resolution is part of the answer:** each part says how fine it is ("bis 24.10. stündlich, danach auf
   5 Minuten"), so the model can tell the user that older values are coarser.
3. **Without `above`:** per day the minimum, maximum and mean (a counter: its change), at most 31 lines; a day from
   the hourly part reads the same, only its values come from hours.
4. **With `above`:** periods above the threshold, with the same answer and the same shortening as for states:
   - from the 5-minute part, the 5-minute rows whose mean lies above, to 5 minutes ("über 5 W 9:05–10:45
     (1 h 40 min)");
   - from the hourly part, the hours whose maximum lay above, merged into whole hours and marked as such ("über
     5 W zwischen 9 und 11 Uhr, auf Stunden genau"); their duration is an upper bound and is called that
     ("höchstens 2 h").
5. **Without statistics** (a number without `state_class`): "Dafür führt Home Assistant keine Statistik." No reading of
   the raw history instead.

### What it costs

| Step | Does | Costs | Turn waits? |
|---|---|---|---|
| a state entity | 1 `history/history_during_period` for one entity, 1 read of its live state | the history grows with its changes in the period: at most 31 days of one entity (a motion sensor: a few thousand entries, some 100 kB); nothing is kept | yes, a call over the open WebSocket |
| a measurement | 2 `recorder/statistics_during_period` (5-minute and hourly; 1 when the 5-minute ones reach back to the start) | at most 10 × 288 rows of 5 minutes and 31 × 24 hours | yes, as above |
| the model | the condensed answer | under 1,000 characters, whatever the period; the tool's description in every turn (about 400 characters) | — |

No subscription, no polling, nothing runs after the turn. What the model reads does not grow with the history.

### Rights

Read-only: background work (tasks, agents) may use it too. It reads with Mira's own Home Assistant user, which may read
the recorder without admin rights (tried 2026-11-03).

## Steps

1. **States:** the tool with periods, condensing, shortening, the live state, the exposed check, "not recorded";
   German and English texts.
2. **Measurements:** statistics, 5-minute or hourly, with and without `above`.
3. **Guide:** a line in the guide's list of what one can ask, with the examples above; and that Home Assistant keeps
   the history of switches, doors and lights 10 days unless its recorder is set to keep it longer
   (`purge_keep_days`), with what that costs (a larger database).

## Later

- Several entities in one call ("Welche Fenster waren heute offen?"), if one at a time proves too slow.
- A period longer than 31 days for measurements, from the hourly statistics only (they are kept for good).
