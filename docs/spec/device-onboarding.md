# Adding a Zigbee device by voice – Spec

Status 2026-11-10: steps 1 to 5 built (not released): the fake Zigbee2MQTT, the state table checked against it, the
tools and the wait in the service, the skill, run end to end at the fake Show, the guide, and the real run against
a household's Zigbee2MQTT (a new sensor added, the same sensor paired again). Step 6, ZHA, built against Home
Assistant's source; its real run is open. Scenarios:
[device-onboarding.feature](device-onboarding.feature).

## Goal

A new Zigbee device is added by talking to Mira, without opening Home Assistant or Zigbee2MQTT:

1. Stefan: "Hey Mira, ich hab hier ein neues Zigbee-Gerät." Mira: "Soll ich das Einbinden starten?"
2. Stefan: "Ja." Mira: "Das Einbinden läuft drei Minuten. Halt den Knopf am Gerät ein paar Sekunden gedrückt, bis
   es schnell blinkt, oder steck es neu ein. Ich sag Bescheid, wenn ich es sehe."
3. A minute later, unasked, at the same satellite: "Ich hab's: ein Temperatur- und Feuchtesensor von Aqara. In
   welchen Raum kommt er?" Stefan: "Wohnzimmer."
4. Mira: "Ist im Wohnzimmer. Ich nenne ihn ‚Temperatursensor Wohnzimmer', passt das?" Stefan: "Ja."
5. Mira: "Fertig: Temperatursensor Wohnzimmer, im Wohnzimmer, und ich kann ihn jetzt auch abfragen."

Today this takes the Zigbee2MQTT page (permit join), the Home Assistant device page (area, name) and the Assist
settings (expose), and the device is called `0x00158d0001a2b3c4` until someone renames it.

## Decided (2026-11-09)

| # | Question | Decision |
|---|---|---|
| 1 | Which Zigbee bridge | **Zigbee2MQTT** only, for now. ZHA and deCONZ later; from step "wait for the device" on, everything is Home Assistant's and the same for all |
| 2 | How far the dialog goes | Pairing, the room, the name, and exposing the device to Assist. No automation, no dashboard |
| 3 | Who may add a device | Anyone at a satellite, as anyone may switch a light |
| 4 | How it is tested | First against a fake Zigbee2MQTT in the test environment; then for real, with a Zigbee adapter and real devices at the test HA |
| 5 | Devices per dialog | One: once a device is in Home Assistant, Mira closes pairing; the next device is a new "neues Gerät" |
| 6 | Who writes the question "Ich hab's: …" | One model round in the thread, from the device's facts: Zigbee2MQTT describes it in English |
| 7 | Exposing to Assist | Every entity of the device except configuration and diagnostic ones, without asking |
| 8 | Where the name is changed | In Zigbee2MQTT (`homeassistant_rename`), Home Assistant follows with the device's name and its entity ids, if that works uniformly (checked against the fake, confirmed in the real run). If it does not: only Home Assistant's name of the device (`name_by_user`), the same for every bridge later |
| 9 | Order of the tests | The fake is built from Zigbee2MQTT's documentation first, and everything is built against it; the real run comes at the end and corrects the fake where the real bridge differs |
| 10 | The adapter for the real run | A USB adapter at the machine of the test HA, Zigbee2MQTT as a container next to the test broker; not before the maintainer is at the machine. Changed 2026-11-09: the real run is made against an existing household installation (Zigbee2MQTT 2.14.2 with its own coordinator and routers) with the service built from this branch, no adapter at the test HA |

### ZHA (decided 2026-11-10)

ZHA, Home Assistant's own Zigbee, comes as a second bridge after the found devices ([Found devices](found-devices.md)).
From "the device is in Home Assistant" on everything is the same; what differs is opening pairing and the wait: ZHA's
websocket command `zha/devices/permit` (internal to Home Assistant, not documented; checked by `testha compat` once
built).

| # | Question | Decision |
|---|---|---|
| 11 | How it is tested | The Go tests against ZHA's answers as Home Assistant's source gives them; at the very end the test HA gets ZHA with a USB adapter of its own, and the real run corrects them |
| 12 | The name | Home Assistant's name of the device (`name_by_user`) **and** its entity ids, as Home Assistant's own rename dialog offers (`sensor.temperatursensor_wohnzimmer_temperature`) |
| 13 | ZHA and Zigbee2MQTT both in Home Assistant | Mira asks which network |
| 14 | A known device paired again | Seen in the real run, then specified (as for Zigbee2MQTT) |

How it works (built 2026-11-10, from Home Assistant's source; the real run is open):

1. `zigbee_pairing_start` reads whether Home Assistant runs ZHA (`config_entries/get` of the domain `zha`: an entry
   `loaded`). With ZHA and a Zigbee2MQTT bridge both, and no `network` named, it opens nothing and returns that Mira
   asks which network (decision 13); the model calls it again with `network`.
2. On ZHA Mira subscribes to `zha/devices/permit` with `duration: 180`: Home Assistant permits joining and, while the
   subscription lasts, sends ZHA's gateway messages over it (`homeassistant/components/zha/websocket_api.py`,
   `helpers.py`; `zha/application/gateway.py`): `device_joined` (ieee, `pairing_status: PAIRED`),
   `raw_device_initialized` (manufacturer, model), `device_fully_initialized` with the device's info and its Home
   Assistant device id (`device_reg_id`). The first `device_joined` is said as for Zigbee2MQTT.
3. `device_fully_initialized` of a device that joined in this run: a device id Home Assistant had before pairing
   opened is a known device paired again (said with its name and room; decision 14, an assumption until the real run);
   a new one is the thread's device. Pairing is closed (`zha/devices/permit` with `duration: 0`, its subscription
   ended at once), the facts (manufacturer, model) go into the thread, and the question is asked as for
   Zigbee2MQTT.
4. The room as for every device; the name: Home Assistant's name of the device (`name_by_user`) and each entity id
   that begins with the device's old name gets the new name there (`config/entity_registry/update` with
   `new_entity_id`, one call per entity), as Home Assistant's rename dialog does (decision 12).
5. ZHA says nothing when pairing ends: after the 180 s Mira says she saw no device (a time, as no signal exists); a
   device that joined and is not initialised 2 minutes later is said as a failed interview.

Fails: the subscription refused or ZHA not loaded: the tool returns the error, log `zigbee pairing failed on ZHA`;
closing refused: log `could not close zigbee pairing on ZHA; it ends by itself`; a rename of an entity id refused:
the tool says it with how many values were exposed, log `could not rename the device's entity ids`. Cost: one
`config_entries/get` per start; one subscription while pairing; one call to close; per rename one device update and
one call per entity of the device.


## Building blocks

- **The concept: a thread with a plan** ([Threads](threads.md), Plans) of the new skill `geraet-einbinden`, bound to
  the room where it started (`bound: room`, as the answering machine's listening). The plan's three steps (the device
  joins, the room, the name) are ticked by Mira's code, not by the model; the device found is written into the
  thread's log as a fact (`Note`), so the next turns see it. Departure from fields: the room and the name are no
  fields a finish waits for, but steps the tools do one by one, each with its own answer.
- **An announcement of the thread** ([Threads](threads.md), [Answering machine](voicemail.md), "Nächste …" belongs to
  the thread): "Ich hab's …" is the thread's question at its satellite, spoken with
  `assist_satellite.start_conversation`, so the answer "Wohnzimmer" is a direct answer to the thread.
- **Home Assistant's own APIs**, nothing rebuilt: `mqtt.publish` to talk to Zigbee2MQTT, the device registry for the
  room and the new device, Assist's expose settings. All through Mira's existing connection to Home Assistant; Mira
  gets no MQTT client of its own.
- **Departure:** the thread waits for a signal from outside (the device joining) between two turns. Today only Mira's
  own playback does that (the answering machine). The wait is held by the code, with the thread's timeout as the
  safety net, not by the model.

## What it does

### The skill and its tools

The skill `geraet-einbinden` (German, like every skill) carries the dialog: when to ask before pairing, how to say
what was found, how to propose a name. Three tools, built into the service and given only to this skill's thread:

| Tool | Does | Costs |
|---|---|---|
| `zigbee_pairing_start` | Only at a satellite. Finds the Zigbee2MQTT bridge, starts the thread (or takes the open one), opens pairing for 3 minutes, binds the wait to the thread | One `mqtt/device/debug_info` of the bridge; three MQTT subscriptions and one registry subscription through Home Assistant; one `mqtt.publish` |
| `device_assign` (room) | Puts the device the thread found into the room | One `config/device_registry/update` |
| `device_finish` (name) | Exposes the device's entities to Assist, then renames it in Zigbee2MQTT (Home Assistant follows), ends the thread | One `search/related`, one `config/entity_registry/get_entries`, one `homeassistant/expose_entity`; one `mqtt.publish` |

The skill is not on by itself: a household with Zigbee2MQTT switches it on with a line `geraet-einbinden: {}` under
`skills` (or on Mira's page), as every skill that needs something in the house.

### Step by step

1. **Pairing.** On the yes the model calls `zigbee_pairing_start`. Mira finds the bridge: the device of
   `manufacturer` "Zigbee2MQTT", `model` "Bridge" among the devices Mira keeps (several: the first by id), and its base
   topic from its MQTT discovery (`mqtt/device/debug_info`, the state topic `<base>/bridge/state`). She subscribes
   through Home Assistant to `<base>/bridge/response/#` (the answers, for the thread's whole life), to the entity
   registry's `create` events, to `<base>/bridge/event` and, last, to `<base>/bridge/info`. Home Assistant subscribes
   at the broker a moment after it confirms (seen on the test HA 2026-11-09: an answer right after was lost), so Mira
   waits for the retained info to come: then all of them are live (a signal; 10 s as the safety net). She publishes
   `<base>/bridge/request/permit_join` with `{"time": 180}` and a `transaction`; the answer with that transaction and
   `status: ok` returns the tool; the model tells the user to press the button. The turn ends.
2. **The device joins.** Zigbee2MQTT reports `device_joined`, then `device_interview` with `started` and, after up to
   a minute (battery devices are slow), `successful` with the device's definition: vendor, model, description, what
   it exposes. At the first `device_joined` of the run Mira says at the satellite that she sees a device and is
   getting to know it, that this can take up to two minutes, and to press its button briefly now and then (decided
   2026-11-09 after the real run: an interview of 70 s without a word felt broken; a battery device answers the
   interview only while awake). It is a message of the thread like the question, so a reply joins the thread; the
   question waits until the satellite is free again. A device that leaves and joins again is not said twice.
3. **Home Assistant has it.** After the interview Zigbee2MQTT publishes its discovery; Home Assistant creates the
   device and its entities and fires `entity_registry_updated` `create` for each. Mira reads the first such entity
   (`config/entity_registry/get_entries`): its `unique_id` begins with the IEEE address of the device that joined, its
   `device_id` is the device. The bridge's events and the registry events come over the one connection, in order.
   With the interview successful and the device known, pairing is closed (`permit_join` with `time: 0`): one device
   per dialog. The subscriptions to the events, the info and the registry end.
4. **The question.** Mira writes the device's facts into the thread (vendor, model, Zigbee2MQTT's description, its
   values) and ticks the first step. The skill's agent `geraet-einbinden.frage` (one model round, no tools) words the
   question from the facts ("Ich hab's: … In welchen Raum kommt es?"): the description comes in English from
   Zigbee2MQTT and is said in the household's language. Mira says it as the thread's question at the satellite where
   the thread began (the notifier, with `start_conversation`), so the answer is a reply to the thread.
5. **The room.** The answer is a turn of the thread. The model calls `device_assign` with the room's name; Mira
   resolves it among Home Assistant's areas by name, id or alias, sets the device's area and ticks the step. An unknown
   room: the tool names the rooms and the model asks.
6. **The name.** The model proposes a name following the skill (what it is and the room) and asks. On the yes, or
   with the user's own name, it calls `device_finish`: Mira first exposes every entity of the device to Assist except
   those Home Assistant marks as configuration or diagnostic (disabled ones are not among them); then she publishes
   `<base>/bridge/request/device/rename` with `{"from": "<ieee>", "to": "<name>", "homeassistant_rename": true}` and
   waits for `status: ok`. Home Assistant removes the device and its entities and creates them again with the same
   ids of device and area, the new entity ids, and the expose setting kept (seen on the test HA 2026-11-09), so nothing
   is read after the rename. The thread ends as done; its subscription to the answers ends with it.

### When a step fails

| Fails | Who learns of it |
|---|---|
| Not at a satellite (Telegram, the app) | The tool returns that this works only at a satellite, where Mira speaks up once the device is there; nothing is opened |
| No Zigbee2MQTT bridge in Home Assistant | The tool returns the error; Mira says she finds no Zigbee2MQTT in Home Assistant. Log `zigbee bridge not found` |
| The bridge's topics cannot be read, a subscription fails, or the retained info does not come within 10 s | The tool returns the error; Mira says so. Log `zigbee pairing failed` with the error |
| `permit_join` answers with an error, or no answer within 10 s | The tool returns Zigbee2MQTT's error (or "no answer"); Mira says so. Log `zigbee pairing failed` with the error |
| Nothing joins within the 3 minutes (Zigbee2MQTT ends pairing on its own) | Mira announces at the satellite that she saw no device and asks whether to try again; the thread waits for the answer and ends at its timeout |
| The interview fails | Mira says at the satellite that a device came up that she does not understand, and to press its button again; pairing stays open for the rest of the 3 minutes. Log `zigbee interview failed`. A device Zigbee2MQTT does not support but interviews (`supported: false`, a generated definition) is added like any other |
| A device joined, pairing ended, and its interview does not finish within 2 minutes | Mira says the same as for a failed interview. Log `zigbee device did not finish its interview in time` |
| The device is already known (paired again) | It does not join; on its `device_announce` during pairing Mira finds the device by the name Zigbee2MQTT gives it (Home Assistant's device name) and says it is known, with its name and room. Pairing is closed, the thread ends |
| The question cannot be worded (the model fails) | Mira asks with a fixed sentence naming vendor and model ("Ich habe ein neues Gerät gefunden: IKEA E2206. In welchen Raum kommt es?"). Log `could not word the question for the device` |
| Nobody answers the question | The thread expires at its timeout (15 minutes); the device stays in Home Assistant without room and name. Log `thread ended` with `state=expired` |
| The area or the expose call is refused (the token is no administrator's) | The tool returns Home Assistant's error; Mira says so. Log `could not put the zigbee device into its room` / `could not expose the zigbee device's entities` |
| The rename answers with an error (Zigbee2MQTT refuses the name) | The tool returns it with how many values were exposed; Mira says so; the room and the exposing stay. Log `could not rename the zigbee device` |
| The message that a device is being learned cannot be delivered | As every message of a thread: the notifier logs it and sends it privately; the wait goes on |
| The satellite is busy (music, a call) when the device is found | The announcement is spoken as every announcement of a thread would be there; nothing of its own |

### Cost

Per device: one read of the bridge's discovery, two `mqtt.publish` for pairing, one for the rename; three MQTT
subscriptions and one registry subscription through Home Assistant while pairing is open (the one to the answers until
the thread ends); one read of the entity registry per entity Home Assistant creates while a device is awaited (a
device has a handful; other integrations creating entities in those minutes add theirs); one area update; one
`search/related` and one read of the entity registry for exposing; one expose call; one model round for the question
and the turns of the dialog; two announcements at the satellite (the device being learned, the question). Nothing grows with the number of devices in the house: the subscriptions are filtered to
the bridge's topics and to `create` events.

## State table

Each row is checked on the test HA with the fake before the code waiting for it is built, then again against a real
Zigbee2MQTT (decision 10): "real" in the last column is Zigbee2MQTT 2.14.2, a real sensor (Aqara WSDCGQ11LM),
2026-11-09.

| # | Transition | Trigger | Signal | How Mira learns of it | Checked |
|---|---|---|---|---|---|
| 1 | Pairing opens | `mqtt.publish` `<base>/bridge/request/permit_join` `{"time":180}` | `<base>/bridge/response/permit_join` `status: ok` (with the request's `transaction`), then `<base>/bridge/info` `permit_join: true` | `mqtt/subscribe` through HA's websocket, with the owner's token; a retained topic comes at once once the subscription is live at the broker | fake 2026-11-09; real 2026-11-09 (the response `ok` with the transaction) |
| 2 | Device joins | Button pressed, battery in | `<base>/bridge/event` `device_joined`, then `device_announce` and `device_interview` `started` | the same subscription | fake 2026-11-09; real 2026-11-09 (`device_joined` about 13 s after pairing opened) |
| 3 | Interview done | – | `device_interview` `successful` with `supported` and `definition` (vendor, model, description, exposes), or `failed` without them | the same subscription | fake 2026-11-09; real 2026-11-09 (`successful` with vendor and model; `failed` not seen for real) |
| 4 | HA has the device | Zigbee2MQTT's discovery after a successful interview | `device_registry_updated` `create`, then `entity_registry_updated` `create` per entity; an entity's `unique_id` is `<ieee>_<property>_<base>`; the device's identifiers hold `zigbee2mqtt_<ieee>`, its `manufacturer` the vendor, `model` the description, `model_id` the model; its name is the IEEE address | a subscription to `entity_registry_updated` while pairing, and `config/entity_registry/get_entries` of the entity | fake 2026-11-09 (the `create` came in the same moment as the interview's `successful`); real 2026-11-09 (the entity found about 10 s after the join) |
| 5 | Pairing ends by itself | the time over | `<base>/bridge/info` `permit_join: false` | the same subscription | fake 2026-11-09; not seen for real |
| 6 | Pairing closed by Mira | `permit_join` `{"time":0}` | response `status: ok`, `info` `permit_join: false` | the same subscription | fake 2026-11-09; real 2026-11-09 |
| 7 | Renamed | `<base>/bridge/request/device/rename` with `homeassistant_rename` | response `status: ok`; Home Assistant removes the device and creates it again **with the same id and its area kept**, the entity ids renamed (`sensor.temperatursensor_wohnzimmer_temperature`). An unknown device: `status: error`, `Device '<name>' does not exist` | subscription; HA's registry events | fake 2026-11-09 (Home Assistant's part is real: the fake only publishes what Zigbee2MQTT publishes); real 2026-11-09 (device and entity ids renamed, area kept) |
| 8 | Device's entities | – | – | `search/related` for the device gives its entity ids (disabled ones not), `config/entity_registry/get_entries` their category; one call each | fake 2026-11-09; real 2026-11-09 (temperature, humidity, pressure exposed; battery and voltage are diagnostic) |

Also seen (2026-11-09): the area update (`config/device_registry/update`) and `homeassistant/expose_entity` work with the
owner's token. The bridge is a device of `manufacturer` "Zigbee2MQTT", `model` "Bridge"; `mqtt/device/debug_info` of it
gives its topics, so the base topic is read, not configured. Zigbee2MQTT also gives the bridge a switch "Permit join"
in Home Assistant; it always opens for 254 s and answers nothing, so Mira publishes the request herself.

A known device paired again (real 2026-11-09: the sensor reset while pairing was open): Zigbee2MQTT sends only
`device_announce` with its name and address; no `device_joined`, no interview, no registry event in Home Assistant.
The fake does the same; Mira treats a `device_announce` of a known address during pairing as that device.

Interrupting: "abbrechen" at any time cancels the thread; Mira closes pairing (row 6) if it is open. The wake word
during the announcement is an ordinary interruption of an announcement.

## Testing

- **The fake Zigbee2MQTT** (`testenv/z2m_fake`) runs next to the test broker: it answers `permit_join` and `rename`,
  publishes `bridge/info`, and on a test action plays a device joining: the events and the discovery messages, as
  Zigbee2MQTT's documentation describes them (decision 9). Kinds: a temperature sensor (battery), a plug (mains), a
  device that fails its interview, a known device paired again. After the real run the fake follows what the real
  bridge sent, as the fake satellites follow the real one ([Fake devices](fake-devices.md)).
- **The real run**: the service built from the branch against a real Zigbee2MQTT with its own coordinator (decision
  10), with real devices; Mira's log and the bridge's topics (read through a subscription in Home Assistant) side by
  side.

## Steps

1. **The fake Zigbee2MQTT** from the documentation, next to the test broker.
2. **Check the signals against the fake**: every row of the state table seen in the test HA, `mqtt/subscribe`
   through HA's websocket with Mira's token, the bridge found from Home Assistant, the rename followed by Home
   Assistant (decision 8).
3. **Tools and wait** in the service (built 2026-11-09): the three tools, the bridge's subscriptions, the match on
   the registry event, the announcement of the thread, every error path. DV-01 to DV-10.
4. **The skill `geraet-einbinden`** and the end-to-end run against the fake at the fake Show (built 2026-11-09: a
   plug added, room and name set, three values exposed; "abbrechen" while pairing; a failed interview; a known
   device; nothing joining); the guide.
5. **The real run** against a real Zigbee2MQTT (decision 10; run 2026-11-09): a new sensor added by voice (room,
   name, three values exposed), the same sensor paired again (known device). Every row of the state table seen
   again except row 5 and a failed interview; the fake needed no correction.
6. **ZHA** (decisions 11 to 14), after the found devices: the spec of its signals from Home Assistant's source, the
   tools on ZHA, the Go tests (built 2026-11-10, DV-12 to DV-17); at the end the real run with the test HA's own
   adapter (open).
