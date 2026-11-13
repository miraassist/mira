# Glossary

Status 2026-11-13: first version; its questions decided the same day.

One word for one thing, in English (repository: specs, code, comments, commits) and in German (the conversation
between the maintainer and Claude). The glossary is for precision in the chat and in the specs: it draws sharp lines
and may be technical. It is not the wording of Mira's sentences or the guide; those may say it in plainer words. Most
terms have a counterpart in the code or the database; it is named. A word in the column "Not" is not used for the term, in neither language. A new term is added here before it
is used in a spec; a term that is not here is not a term yet.

## Mira's parts

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| Mira service | Mira-Service | The Go program: conversation, messages, threads, tasks, the page | `cmd/mira`, `internal/`. Kept where stored or part of an interface: the integration's config entry key `CONF_AGENT_TOKEN` and other stored names with "agent". In texts users see it is "Mira" | agent, Mira agent, the app (except for one of Mira's helpers: agent) |
| HA app | HA-App | Home Assistant's app (add-on) that runs the Mira service | `app/` | the app (alone), add-on |
| integration | Integration | Mira's part inside Home Assistant: the conversation agent for Assist, the actions `mira.*`, the device "Mira" | `custom_components/mira` | component |
| Mira's page | Miras Seite | Mira's web pages in Home Assistant's sidebar | `internal/ui`, `/v1/ui/*` | dashboard, UI |

## Conversation

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| turn | Gespräch | One conversation as Home Assistant conducts it: a wake word to the end of listening, or the answer after an announcement; its own history | **session** in the code and the database: `Session`, table `sessions` (`key` = channel:conversation); a synonym there, kept on purpose | dialog |
| chat message | — | One line of a turn's history (user, assistant, tool) | table `chat_messages`, `llm.Message` | message |
| thread | Vorgang | A concern across turns, with a target, a place, a state, fields, a log, optionally a plan | `internal/threads`, table `threads`, `store.Thread` | Anliegen, concern (as a term), session |
| target | — | The kind of thread a skill defines: its fields, checks and how it ends | `Thread.Target`, a skill's `metadata.mira.target` | type |
| owner | Besitzer | Whom a thread belongs to: a person, a room or a chat; whom a fact of the memory belongs to: the house or a person | threads: still `place` in the code (`Thread.Place`, `threads.place`: `person:<id>`, `area:<id>`, …), kept on purpose; memory: `memory_facts.owner` | place (in prose) |
| plan | Plan | A thread's list of steps worked through, with a current one | `thread_plan`, `threads.content` | recipe, checklist |
| finisher | — | The code step that ends a thread of a target (saves, sends, creates) | e.g. `tasks.automation` | handler |
| satellite | Satellit | A voice device in a room | HA `assist_satellite.*`, `sessions.device_id` | speaker (for the satellite), Echo |
| room | Raum | An area where Mira has satellites (Wohnzimmer, Küche): a turn's room, announcements, the ring and the screen, the morning announcement, radio and sleep timer, a room's language, threads and messages | `ui.Room`, the integration's `rooms`; its key is the area's `area_id` (`sessions.area_id`, `messages.area_id`, `threads.area`) | zone |
| area | Bereich | Any area of Home Assistant, with or without a satellite (also Garten, Garage): the house's structure in the prompt, the area a new device goes into, a task or a target found by its area, the outside lighting of a watch | `area`, `area_id`, `internal/ha/structure` | room (for an area without a satellite) |
| floor | Etage | A floor of Home Assistant, holding areas (Keller-, Erd-, Dachgeschoss) | `floor_id`, the floor registry | level, storey |
| person | Person | A member of the household, a Home Assistant person | receiver `person:<id>`, `threads.person` | user (except HA's login), member |
| house | Haus | Everyone at once: a receiver or place that is nobody's alone | receiver/place `house` | home, household (as receiver) |
| channel | Kanal | A way Mira speaks or writes: voice, Telegram, the companion app | `internal/channels`, `sessions.channel` | transport |

## Messages

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| message | Nachricht | What waits for a receiver; lights the ring and the screen | table `messages`, `store.Message` | notice, notification, Hinweis (as a term) |
| entry | Eintrag | One item of a message: a text, often with a thread behind it | table `message_entries` | item, notice |
| kind | Art | What a message is and what happens with it: `simple`, `reminder`, `urgent`, `private`, `confidential`, `postfach`, `voicemail`, `device`, `reply` | `messages.kind`, `store.Kind*` | type, category |
| receiver | Empfänger | Whom a message is for: a person, a room, the house | `messages.receiver` | audience |
| sender | Absender | Who put an entry: a person, a task, a skill | `message_entries.sender` | author (of a message) |
| announcement | Ansage | Mira saying waiting messages at a satellite | table `announcements`, `notify.compose` | Durchsage |
| broadcast | Durchsage | "Sag allen …": at once over all satellites, not stored | Home Assistant's own (Assist); nothing of Mira's | announcement |
| reminder | Erinnerung | A message of kind `reminder`, due at a time | `messages.kind = reminder`, `deliver_at` | alarm |
| indicator | Anzeige | The LED ring and the screen showing what waits | `internal/notifier` | lamp, light |
| Mira's inbox | Miras Postfach | Mails and documents Mira reads and works through, a kind of message | `internal/inbox`, kind `postfach` | mailbox |

## Capabilities

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| skill | Skill | A folder with `SKILL.md`: instructions, settings, optionally a target, schedules, agents, an MCP server | `internal/skills`, `skills/` | Fähigkeit (for one skill), plugin |
| capabilities | Fähigkeiten | The tab of Mira's page with skills, rules, schedule and tools | `internal/ui` | — |
| tool | Werkzeug | A function the model calls | `internal/tools`, MCP tools | action (for a tool), function |
| agent | Agent | One of Mira's helpers: a model with its own context, tools and form of answer, callable like a tool | `internal/agent`, tables `agent_runs`, `agent_run_steps` | the Mira service |
| hook | Regel | A check on every tool call: allow, confirm, deny ([Hooks](spec/hooks.md): "Regel" for users, "hook" in spec and code) | `internal/hooks`, folder `hooks/` | policy, guard (for the hook), rule (in English) |
| schedule | Zeitplan | When a skill acts by itself (cron) | a skill's `schedules`, `kv` `schedule:*` | timer, job |
| task | Aufgabe | A Home Assistant automation as the user sees it; Mira's own carry the label "Mira" | `internal/tasks`, table `own_automations` (kind `task`) | automation (to the user), job |
| assessment | Bewertung | A model's judgement a task asks for (`mira.assess`, `mira.decide`) | table `assessments`, `tasks.assess`, `tasks.decide` | evaluation |
| own automation | eigene Automation | An automation Mira saved and may delete: a task, a timer | table `own_automations` (`id`, `kind`) | — |

## Memory

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| memory | Gedächtnis | What Mira keeps about the house and each person | table `memory_facts` (`internal/store/memory.go`); as a file for people `internal/memfile` | knowledge base |
| fact | Fakt | One thing in a memory, with who said it and when | `memory_facts` row | note |
| memory profile | Gedächtnisprofil | The nightly summary of one memory | `internal/profile`, table `memory_profiles`, agent `profile.compact` | profile (alone) |

## Storage

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| key-value table | Schlüssel-Wert-Tabelle | Small marks without a table of their own; under review | table `kv` (`key`, `value`) | cache, settings |
| configuration | Einstellungen | `mira.yaml`, the one truth; the pages write it | `internal/config` | options (except HA's options dialog) |

## Watching (new, [Device watch](spec/device-watch.md), [Group watch](spec/group-watch.md))

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| blueprint | Blueprint | Home Assistant's automation with named inputs; Mira's are the ones the service brings under `mira/`, a fixed way of watching, written once and checked | HA `blueprints/automation/mira/`, `internal/watch` (planned) | pattern, Muster, Vorlage, Blaupause |
| watch | Wächter (one); Überwachung (the feature) | A blueprint applied to the house: a task made from a blueprint; a device watch or a group watch | an automation with `use_blueprint`, `own_automations` kind `task` | monitor, sentinel |
| device watch | Gerätewächter | A watch over one device against its own normal, learned from its history (its device profile): a water meter, a fridge, a dishwasher | blueprints `water_runs_on`, `water_never_rests`, `daily_routine`, `duty_cycle`, `device_run` | consumption watch |
| group watch | Gruppenwächter | A watch over a group of sensors against a rule for a situation the user says: lights left on, windows open, motion while nobody is home | blueprint `left_on` (planned) | — |
| watch group | Wächtergruppe | The sensors a group watch watches: a selection by device class, area or floor (combinable), or a group helper; without what carries the label "Mira: nicht überwachen" (not for a group helper) | blueprint inputs (planned) | group (alone) |
| group helper | Gruppen-Helfer | Home Assistant's group entity with fixed members ("Fenster Gast offen") | domain `group` config entries, e.g. `binary_sensor.fenster_gast_offen` | group (alone) |
| device class | device class | What an entity is within its domain (window, door, motion, …), in Home Assistant's UI "Anzeigen als" | `device_class`, `original_device_class` | device type, Gerätetyp |
| candidate | Kandidat | A device a blueprint could watch | computed, not stored: the tool `watch_candidates` lists them on request | suggestion |
| watch offer | Angebot (eines Wächters) | Mira's promise to take a device into the watching after its next run, and the offer after it: a thread of the house that waits for the run, then for the yes | target `ueberwachung-anbieten` (a skill of its own), tools `watch_wait`, `watch_accept`, `watch_decline`; `internal/watch` (`Waiter`) | suggestion |
| device profile | Geräteprofil | A device's usual behaviour: its kind (daily routine, cycle, device run) and its numbers | computed, not stored: `internal/watch` (`Profile`, `Profiler`), the tool `device_profile` | profile (alone), habit, baseline |
| daily routine | Tagesablauf | On at the same times every day | blueprint `daily_routine` | schedule |
| cycle | Takt | On and off again and again all day | blueprint `duty_cycle` | — |
| device run | Gerätelauf | A device's run of a programme, with a start and an end, now and then (a dishwasher, a washing machine) | blueprint `device_run` | run (alone: a schedule's or an agent's), programme run, cycle |

## Decided (2026-11-13)

1. A device's usual behaviour is its **device profile** (Geräteprofil); the memory's is the **memory profile** (Gedächtnisprofil); "profile" alone is not used (changed the same day: first "habit").
2. One **watch** is a Wächter; the feature is the Überwachung.
3. "Mira agent" in older documents (`docs/system.md`, the guide) is corrected to the Mira service everywhere.
4. The guide says Vorgang for a thread, also where it said "Anliegen".
5. Whom a thread belongs to is its **owner**, as a fact's; the code keeps `place` for threads (not renamed).
6. The Mira service is "service" in the system documentation, the specs, the code's comments and logs; stored names and
   interfaces with "agent" stay (above); the integration's texts for users say "Mira".
7. "session" is the code's and the database's word for a turn and stays there.
8. "notice" is not used: a message, or an entry of one.
9. A **room** is an area where Mira has satellites; an **area** is any area of Home Assistant.
10. Two kinds of watch: the **device watch** and the **group watch**; "consumption watch" is not used.
11. Mira's fixed ways of watching are **blueprints** ("Blueprint" in German too, as in Home Assistant); "pattern" and "Muster" are not used.
12. A group watch watches a **watch group**: a selection by device class, area or floor, or a group helper; "group" alone is not used. The **floor** is Home Assistant's floor.
