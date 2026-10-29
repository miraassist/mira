# Mira in Home Assistant: decorate instead of own lists – spec

Status 2026-10-16: built in 0.5.9. Complements [Users](benutzer.md) and [Tasks](aufgaben.md).

## Principle

What already exists in Home Assistant, Mira does not create again. She attaches her settings to the things in HA:

| What Mira needs | Where it lives |
|---|---|
| Persons | HA's persons; Mira's settings as entity registry options `options["mira"]` on the person ([Users](benutzer.md)) |
| Rooms | the **satellites**; Mira's settings as `options["mira"]` on the satellite, the area is the satellite's area |
| Tasks | automations with the label "Mira" ([Tasks](aufgaben.md)) |
| "switch only after confirmation" | the label "Mira: nachfragen" on the entity |
| what Mira sees | HA's exposure to Assist, as before |

**Rule of thumb:** A property that is either there or not is a **label** (visible and settable in HA's normal UI, filterable). A setting with a value is an **option on the entity**. Own lists only for what does not exist in HA (quiet hours, escalation …).

Reading and writing:
- **Writing** happens through the integration (admin): forms under Devices & services → Mira → **Configure**. The agent checks every input before it is stored.
- **Reading** labels is done directly by the agent: its HA user (no admin) may read the entity and label registries, and it gets notified of changes. Person and satellite settings are sent by the integration to the agent on every change, as before.
- A deleted entity takes its options with it; nothing is left behind.

**Watching states:** Mira reads states only as Home Assistant filters them, and watches instead of asking again:
- **Watching** an entity or a domain is a subscription (`subscribe_entities` with `entity_ids` or `include.domains`;
  triggers the same way, `SubscribeTrigger`; waiting for a state, `WaitState`): Home Assistant reports what changed,
  nothing is polled. What Mira needs all the time is watched for as long as she runs: the persons (domain `person`).
- **Reading once** (`GetStates`) names the entities or domains it needs; Home Assistant filters, Mira takes its first
  answer and unsubscribes. `get_states` has no filter, so Mira never sends it: in the test house it is 722 states to
  look at a handful.
- Polling is the last resort, only where nothing reports a change, and then the spec says why.
- **Signals, not timing.** A sequence (play, then speak, then listen) is coordinated by clear signals of state
  transitions: a satellite's state, an event, a turn that arrives. Never by computed durations, estimated delays or
  sleeps; a time is only a safety net, or the very last resort where no signal exists, and then the spec says why
  (2026-10-29: the answering machine waited a recording's computed length from the play command, but the recording
  started 7 seconds later; the satellite's "responding" while it plays was the signal).
- Handlers of a subscription run on the connection's read loop and only pass the change on.

(2026-10-29: five waits on a satellite asked for every state in the house every 300–500 ms, and 14 places read every
state to look at a few, one of them in every voice turn; all replaced.)

## Rooms are satellites

Almost everything that is a "room" today is a property of the satellite:

| Setting on the satellite (`options["mira"]`) | Meaning |
|---|---|
| `presence` | **list** of motion sensors; each of them triggers "someone comes in" (e.g. hallway PIR and living room PIR for the living room Echo) |
| `morning` | **announce on entering**: when one of the motion sensors detects someone, Mira announces on her own what is waiting (after quiet hours with "Guten Morgen") |
| `indicator` | the indicator, e.g. the LED ring of the Echo: lights up as long as something is waiting, independent of motion sensors. Without a setting it is the satellite's own ring, found (2026-10-29): a light named "LED ring" on the satellite's device or on a device hanging off it (TECHO5: "Dot 2 ring"), not its segments. A room with a ring shows what waits there, not on a screen. Satellites with a screen need no setting, Mira detects them on her own |

- The **area** is the satellite's area in HA (entity, otherwise its device). A satellite without an area cannot be configured.
- Satellites are `assist_satellite.*`, for speakers without Assist also `media_player.*` (with `notifier.tts_entity`).
- For now **one satellite per area**: a second one in the same area is rejected. Several per area would be a separate step (who speaks then?).
- A satellite **switched off** in Home Assistant keeps its settings but has no room; the next one in its area speaks there. The integration sends the rooms again when the registries change (a satellite switched off, moved, a ring added), a burst of changes once.
- The integration's "room" sub-entries are dropped. Areas themselves cannot be decorated in HA (the area registry has no options); that is another reason why the satellite is the anchor.
- Without the integration everything stays in `mira.yaml` (`satellites`, `notifier.morning_trigger`, `indicators`); `presence` may be an entity or a list there.

## What Mira sees: exposure to Assist

What Mira may see is determined by HA's "Expose to Assist". HA's own tools via MCP respect that themselves. Mira's own parts, however, talk to HA directly and must keep the same boundary: reading calendars, the checks of calendar and list, the catalog for task drafts, her own structure block and the guard.

- **The integration supplies the list.** On every sync (`PUT /v1/ui/ha`) it sends, alongside persons and satellites, `exposed`: the IDs of all entities that HA exposes to Assist. It determines this the same way HA itself does (`async_should_expose`). If an exposure changes, it sends again.
- **The agent stores the list** persistently and treats it as authoritative.
- **Without this list nothing counts as exposed,** and Mira does not act.
  - The agent does not ask HA itself: the exposure list can only be read by an admin, and Mira's user deliberately has no admin rights. There is also no fallback to entity options or to "everything".
  - Without the list the house plan is empty, as is the selection for tasks, calendars and lists.
  - Without the list, Mira answers a conversation with: "Ich warte noch auf die Verbindung mit Home Assistant …" She calls no tool and does not ask the model. Otherwise HA's MCP server would switch whatever is exposed there, while the guard does not know these names in the empty plan.
  - This only concerns the period before the integration's first sync. After that the agent keeps the list across restarts too.
- **Login names:** With the settings, the integration sends the login name of each person's HA user (`logins`), because only an admin may read it. It determines which calendar belongs to a person ([Calendar](kalender.md)).
- **The list stays correct when either of the two restarts** (since 0.6.50):
  - The agent reports a **fingerprint** of its list in `/v1/status` (`exposed_hash`: sha256 of the sorted IDs, joined by line breaks; empty as long as it never received one). The integration computes the same from HA and compares on **every poll** (every minute), after HA has started and on every change of exposure. If it differs, it sends the list again. This covers a device discovered later (MQTT, discovery), a missed send and an agent that starts with an empty database.
  - **While HA is starting, nothing is sent** (state "running" only after the start event). During startup the entities register one after another; a list from the first seconds would be short and would replace the complete one at the agent (this happened: 14 instead of 185). Until then the agent keeps the stored list.
  - An agent that does not report the fingerprint (older) is served only once, as before.
  - **Restarting the agent** (for instance after a configuration change) changes nothing: the list is in its database. Only with an empty database does the sync fetch it within a minute.
- **Why not derive it from what Mira sees:** HA's Assist prompt and `GetLiveContext` only give names, no IDs, and leave out calendars entirely. A privacy boundary must not guess by names.

## Asking before switching

Mira switches an entity only after an explicit "Ja" if the hook `critical.star` says so ([Hooks](hooks.md)); the
default one asks if
- its domain is a lock or an alarm panel, **or**
- it carries the label **"Mira: nachfragen"**.

A household can replace `critical.star` with its own (until 2026-10-28 this was `security.confirm_domains` and
`confirm_entities`).

A call counts as switching such an entity also when it does not name it (hardened 2026-10-27):
- an area, with or without a domain: the area's entities of that domain;
- a floor, a device class or only a domain: Mira does not know floors and device classes, so every entity of the given domains (of all, without one) counts as a candidate the hooks judge.

It does not get finer than that: what Mira sees at all is governed by HA's exposure; there is no "read only" and no roles. Background jobs may only read, as before.

## Labels and language

In HA, labels have a fixed `label_id` (derived once from the name on creation) and a changeable name. Entities store only the ID.

- Mira recognizes her labels **only by ID**: `mira` (tasks) and `mira_confirm` (ask first).
- The integration creates them on startup if they are missing, and gives them a name and description in HA's language:
  - `mira`: "Mira" – "Aufgaben von Mira" / "Mira's tasks"
  - `mira_confirm`: "Mira: nachfragen" / "Mira: confirm first" – "Mira schaltet das erst, nachdem du es bestätigt hast." / "Mira only switches this after you confirmed."
- Renaming by the user does no harm. A language change does not change existing names.
