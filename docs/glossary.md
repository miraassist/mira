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
| HA app | HA-App | Home Assistant's app (add-on) that runs the Mira service. In texts for users "Mira-App" / "Mira app", as Home Assistant lists it (the repair to update it, 2026-11-18) | `app/` | the app (alone), add-on |
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
| companion app | Companion App | Home Assistant's own app on a phone or tablet; its Assist dialog talks to Mira as its device | a device of the integration `mobile_app` (its identifier `mobile_app`) | the app (alone), mobile app (in prose) |
| asking device | anfragendes Gerät | The device a turn comes from, which Home Assistant names with the turn: a satellite or a companion app; none in the browser's Assist dialog and in Telegram | `device_id` of a conversation request, `sessions.device_id`, `structure.Snapshot.AskingDevices` | requester, client |
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
| kind | Art | What a message is and what happens with it, not where it came from: `simple`, `reminder`, `urgent`, `private`, `confidential`, `postfach` (until the inbox's step 10), `appointment`, `todo`, `voicemail`, `device`, `reply`, `agent_request` | `messages.kind`, `store.Kind*` | type, category |
| agent request | Agentenanfrage | A remote agent's question to the owner of its thread, because input is missing; a kind of message; the answer goes back to the agent | kind `agent_request` | reply (that is what comes back to a request), question |
| receiver | Empfänger | Whom a message is for: a person, a room, the house | `messages.receiver` | audience |
| sender | Absender | Who put an entry: a person, a task, a skill | `message_entries.sender` | author (of a message) |
| announcement | Ansage | Mira saying waiting messages at a satellite | table `announcements`, `notify.compose` | Durchsage |
| broadcast | Durchsage | "Sag allen …": at once over all satellites, not stored | Home Assistant's own (Assist); nothing of Mira's | announcement |
| reminder | Erinnerung | A message of kind `reminder`, due at a time | `messages.kind = reminder`, `deliver_at` | alarm |
| indicator | Anzeige | The LED ring and the screen showing what waits | `internal/notifier` | lamp, light |
| reading (of a mail or document) | Auswertung | The isolated analysis of a mail or document by the agent `inbox.read`: what is to be done, as items of the kinds a switched-on skill takes; nothing is read out | `inbox.read`, the skill `postfach`'s `inbox.extract` | Lesen, Vorlesen (for it) |
| taking skill | aufnehmender Skill | The skill whose threads a kind's entries lead to; it says so with `takes: <kind>`; at most one switched-on skill per kind | a skill's `takes` (until step 10 of the inbox: `store.KindSkill`) | — |
| Mira's inbox | Miras Postfach | Mails and documents Mira reads; a source of appointments and to-dos, no longer a kind of message (decision 16) | `internal/inbox`, kind `postfach` until the inbox's step 10 | mailbox |

## Calendars and lists (new, [To-dos with a due date](spec/todo-due.md))

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| appointment | Termin | Something that takes place at a time, often at a place; over when its time has passed, whether one went or not | an event of a HA `calendar.*` entity; `internal/calendar` | date (alone), event (in prose) |
| to-do | To-do | Something to do, done only when ticked; optionally with a due date and a description | an item of a HA `todo.*` entity (`summary`, `due`, `description`, `status`) | task (that is a HA automation), item (in prose), Aufgabe |
| to-do list | To-do-Liste | Home Assistant's list of to-dos; a person's own list is the one named like them | HA `todo.*` entity | list (alone, where unclear) |
| due date | Fälligkeit | The day a to-do must be done by; past it and not ticked, the to-do is **overdue** (überfällig) | HA's `due` of an item (`due_date`) | deadline (for the field) |
| deadline | Frist | A day by which something must be done, as a document names it ("Rückmeldung bis Fr 28.11."); Mira enters it as a to-do with that due date | a `todo` item of the inbox's reading | due date (for the item) |
| proposal | Vorschlag | One possible time of an appointment; an appointment from a source has one or more, several when one of them is to be chosen | the field `proposals` of a `termin` thread | alternative, option |

## Capabilities

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
## Matter over Thread (new, [Adding a device by voice](spec/device-onboarding.md))

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| Thread network | Thread-Netz | A mesh network of low-power radios that Matter devices may use instead of Wi-Fi; it reaches the home network through a border router | a dataset in HA's Thread integration (`thread/list_datasets`) | Thread (alone, where unclear) |
| border router | Border Router | The device or service that joins a Thread network to the home network, such as an OpenThread Border Router with a radio stick | HA's integration `otbr` (or another vendor's) | gateway, bridge (that is Zigbee2MQTT's) |
| preferred Thread network | bevorzugtes Thread-Netz | The Thread network Home Assistant puts new devices into; Mira gives its credentials to the Matter server | the dataset with `preferred: true`; its credentials from `thread/get_dataset_tlv` | default network |

| skill | Skill | A folder with `SKILL.md`, from a skill folder or a skill source: instructions, settings, optionally a target, schedules, agents, an MCP server, skill files (skill blueprints, from which skill automations and skill scripts are made) | `internal/skills`, `skills/` | Fähigkeit (for one skill), plugin |
| capabilities | Fähigkeiten | The tab of Mira's page with skills, rules, schedule and tools | `internal/ui` | — |
| tool | Werkzeug | A function the model calls | `internal/tools`, MCP tools | action (for a tool), function |
| agent | Agent | One of Mira's helpers: a model with its own context, tools and form of answer, callable like a tool | `internal/agent`, tables `agent_runs`, `agent_run_steps` | the Mira service |
| remote agent | entfernter Agent | An agent of a skill that runs elsewhere and speaks A2A: Mira sends it the work and gets the result; described by its agent card, not by a model ([Remote agents](spec/remote-agents.md)) | a skill's `metadata.mira.agents.<name>.remote`, `internal/a2a` | A2A server (for the agent), external agent |
| agent card | Agent Card | What a remote agent says about itself: name, description, skills, what it can (push, streaming), how to log in; at `/.well-known/agent-card.json` | `internal/a2a` | manifest |
| skill source | Skill-Quelle | A server Mira loads skills from with a subscription key, at her start; its skills live only in her memory, never on disk ([Skill source](spec/skill-source.md)) | `skills.source` in `mira.yaml` (new) | store, marketplace, repository (for the source) |
| subscription key | Abo-Schlüssel | The secret that lets Mira load the skills a subscription holds from a skill source | `skills.source.key`, in the secrets file (new) | license key, token (for it) |
| skill file | Skill-Datei | Any file of a skill's folder, in any subfolder, carried along with it as it is: from a skill directory or a skill source alike ([Skill files](spec/skill-files.md)) | a skill's `Files` (an `fs.FS`, new) | attachment, resource |
| skill blueprint | Skill-Blueprint | A blueprint a skill brings: a file under `blueprints/` of its folder, saved by Mira into Home Assistant under `mira_skills/<skill>/` ([Skill files](spec/skill-files.md)) | `internal/skillha` (new); HA `blueprints/automation/mira_skills/` | template, pattern |
| blueprint's interface | Schnittstelle (eines Blueprints) | A blueprint's inputs: their names, selectors, whether each has a default; what an automation made from it depends on, so an update must keep it ([Skill files](spec/skill-files.md), The interface) | `blueprint.input` | signature, API |
| skill automation | Skill-Automation | An automation a skill declares and Mira makes from one of its skill blueprints, with inputs from the skill's settings; one of Mira's own automations, a task to the user ([Skill files](spec/skill-files.md)) | a skill's `metadata.mira.automations` (new); id `mira_skill_<skill>_<id>`, `own_automations` kind `task` | rule, routine |
| skill script | Skill-Skript | A script a skill declares and Mira makes from one of its script blueprints, with inputs from the skill's settings; not exposed to Assist: Mira offers it as a tool of the skill, `<skill>__<id>`, once the skill is opened ([Skill files](spec/skill-files.md), step 5, decision 27) | a skill's `metadata.mira.scripts` (new 2026-11-18); id `mira_skill_<skill>_<id>`, entity `script.<id>` | tool, routine |
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

## Access (new, [Access tokens](spec/access-tokens.md))

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| access token | Zugangs-Token | A secret that lets a caller use the Mira service's HTTP interface; each has a name that says whose it is | the integration's: `server.token`; the others: new table `access_tokens` | API key, password |
| integration's token | Token der Integration | The access token the integration got at pairing (or `server.token` in `mira.yaml`) | `server.token`, the setup in the database | — |
| partner token | Partner-Token | An access token made on Mira's page for a partner's service | `access_tokens` (kind `partner`) | — |
| partner | Partner | A company that looks after Mira installations for their owners, with a service of its own; not part of Mira's open source | — | operator, reseller |
| Partner service | Partner-Service | The partner's program next to the Mira service in the house, using its interface with a partner token | separate repository `mira-partner` | partner agent |
| Partner portal | Partner-Portal | The partner's website where its people see and look after many installations | separate repository `mira-partner` | dashboard (alone) |
| partner user | Partner-User | One of the partner's people, known to the Partner service; named in `X-Partner-User` | header `X-Partner-User` | technician (as a term) |
| partner activity | Partner-Protokoll | What a partner token did, with time and partner user, shown on Mira's page | new table `access_log` | audit trail |

## Watching (new, [Device watch](spec/device-watch.md), [Group watch](spec/group-watch.md))

| English | German | Meaning | Code / database | Not |
|---|---|---|---|---|
| blueprint | Blueprint | Home Assistant's automation with named inputs; Mira's are the ones the service brings under `mira/`, a fixed way of watching, written once and checked | HA `blueprints/automation/mira/`, `internal/watch` (planned) | pattern, Muster, Vorlage, Blaupause |
| watch | Wächter (one); Überwachung (the feature) | A blueprint applied to the house: a task made from a blueprint; a device watch or a group watch | an automation with `use_blueprint`, `own_automations` kind `task` | monitor, sentinel |
| device watch | Gerätewächter | A watch over one device against its own normal, learned from its history (its device profile): a water meter, a fridge, a dishwasher | blueprints `water_runs_on`, `water_never_rests`, `daily_routine`, `duty_cycle`, `device_run` | consumption watch |
| group watch | Gruppenwächter | A watch over a group of sensors against a rule for a situation the user says: lights left on, windows open, motion while nobody is home | blueprint `left_on` (planned) | — |
| battery watch | Batteriewächter | A group watch over the house's batteries: once a day it names every battery below its limit or reported low | blueprint `battery_low`, scope `batteries` | battery monitor |
| availability watch | Erreichbarkeitswächter | A group watch over the house's devices: once a day it names every device (or entity of one) that is unreachable | blueprint `unreachable`, scope `devices` | offline watch |
| unreachable | nicht erreichbar | An entity of a device in the state `unavailable` while its integration provides it (not `restored`), for the watch's hours | the availability watch's rule ([Group watch](spec/group-watch.md), decision 38) | offline, dead (for a device) |
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
13. The **battery watch** and the **availability watch** are group watches; a device whose entities are `unavailable` is **unreachable** ("nicht erreichbar"). Added 2026-11-13 without the maintainer, to be confirmed.
14. An agent that runs elsewhere and speaks A2A is a **remote agent** (entfernter Agent); "agent" alone stays one of Mira's helpers. Its question for missing input is an **agent request** (kind `agent_request`), not a `reply`. Decided 2026-11-14.
15. Something that takes place at a time is an **appointment** (Termin) and goes into a calendar; something to do by a day is a **to-do** (To-do) with a **due date** (Fälligkeit) and goes onto a to-do list. A document's **deadline** (Frist), payment or callback becomes a to-do. "Aufgabe" stays a task (a HA automation). Decided 2026-11-15.
16. A message's **kind** says what is to happen with it, not its source: the inbox's things become the kinds `appointment` and `todo`; `postfach` goes. Decided 2026-11-15.
17. Skills that come with a subscription come from a **skill source** (Skill-Quelle) with a **subscription key** (Abo-Schlüssel); "store" and "license key" are not used. Added 2026-11-16 with [Skill source](spec/skill-source.md), confirmed the same day.
18. A file of a skill's folder is a **skill file**; a blueprint a skill brings is a **skill blueprint**, an automation Mira makes from it a **skill automation**; a blueprint's inputs are its **interface**. Added 2026-11-16 with [Skill files](spec/skill-files.md), confirmed the same day. A script Mira makes from a skill's script blueprint is a **skill script** (added 2026-11-18).
