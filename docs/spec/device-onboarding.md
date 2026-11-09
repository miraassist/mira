# Adding a Zigbee device by voice – Spec

Status 2026-11-09: decided; step 1 built (the fake Zigbee2MQTT), step 2 done (the state table checked against the
fake); the real adapter at the end. Scenarios:
[device-onboarding.feature](device-onboarding.feature).

## Goal

A new Zigbee device is added by talking to Mira, without opening Home Assistant or Zigbee2MQTT:

1. Stefan: "Hey Mira, ich hab hier ein neues Zigbee-Gerät." Mira: "Soll ich das Einbinden starten?"
2. Stefan: "Ja." Mira: "Gut, drück jetzt den Knopf am Gerät oder leg die Batterie ein. Ich sag Bescheid, wenn ich es
   sehe."
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
| 10 | The adapter for the real run | A USB adapter at the machine of the test HA, Zigbee2MQTT as a container next to the test broker; not before the maintainer is at the machine |

## Building blocks

- **The concept: a thread of a skill** ([Threads](threads.md)) of the new skill `geraet-einbinden`, bound to the
  satellite where it started (a room's thread, as the answering machine's listening). Its fields are the room and the
  name; the device found is a fact the code writes into the thread, as the answering machine's code writes its plan.
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
| `zigbee_pairing_start` | Finds the Zigbee2MQTT bridge, opens pairing for 3 minutes, binds the wait to the thread | One `mqtt.publish`; one MQTT subscription through Home Assistant while pairing is open |
| `device_assign` (device, room) | Puts the device into the room | One `config/device_registry/update` |
| `device_finish` (device, name) | Renames the device in Zigbee2MQTT (Home Assistant follows), exposes its entities to Assist, ends the thread | One `mqtt.publish`; one read of the device's entities; one `homeassistant/expose_entity` |

### Step by step

1. **Pairing.** On the yes the model calls `zigbee_pairing_start`. Mira finds the bridge (to be checked in step 1:
   by the bridge device that Zigbee2MQTT creates in Home Assistant, and its base topic, by default `zigbee2mqtt`),
   subscribes through Home Assistant to `<base>/bridge/event` and `<base>/bridge/response/permit_join`, and publishes
   `<base>/bridge/request/permit_join` with `{"time": 180}`. The answer `status: ok` returns the tool; the model tells
   the user to press the button. The turn ends.
2. **The device joins.** Zigbee2MQTT reports `device_joined`, then `device_interview` with `started` and, after up to
   a minute (battery devices are slow), `successful` with the device's definition: vendor, model, description, what
   it exposes. Mira says nothing at `device_joined`.
3. **Home Assistant has it.** After the interview Zigbee2MQTT publishes its discovery; Home Assistant creates the
   device and fires `device_registry_updated` with `action: create`. Mira already subscribes to that event
   (`internal/ha/structure`). Mira matches the device by its IEEE address in its identifiers. Pairing is closed
   (`permit_join` with `time: 0`): one device per dialog, the next one is a new "neues Gerät".
4. **The question.** Mira writes the device into the thread (its id, vendor, model, Zigbee2MQTT's description, the
   kinds of entities) and runs one model round in the thread, which writes the question ("Ich hab's: … In welchen Raum
   kommt er?"): the description comes in English from Zigbee2MQTT and is said in the household's language. Mira speaks
   it as the thread's announcement at the thread's satellite with `start_conversation`.
5. **The room.** The answer is a turn of the thread. The model calls `device_assign` with the room's name; Mira
   resolves it among Home Assistant's areas like every room name, and sets the device's area.
6. **The name.** The model proposes a name following the skill (what it is and the room) and asks. On the yes, or
   with the user's own name, it calls `device_finish`: Mira publishes `<base>/bridge/request/device/rename` with
   `{"from": "<ieee>", "to": "<name>", "homeassistant_rename": true}`, waits for `status: ok`, and exposes every
   entity of the device to Assist except those Home Assistant marks as configuration or diagnostic. The thread ends.

### When a step fails

| Fails | Who learns of it |
|---|---|
| No Zigbee2MQTT bridge in Home Assistant | The tool returns the error; Mira says she finds no Zigbee2MQTT in Home Assistant. Log `zigbee bridge not found` |
| `permit_join` answers with an error, or no answer within 10 s | The tool returns Zigbee2MQTT's error (or "no answer"); Mira says so. Log `zigbee pairing failed` with the error |
| Nothing joins within the 3 minutes (Zigbee2MQTT ends pairing on its own) | Mira announces at the satellite that she saw no device and asks whether to try again; the thread waits for the answer and ends at its timeout |
| The interview fails, or the device is not supported | Mira announces it ("hat sich gemeldet, ich verstehe es aber nicht") with vendor and model if known; pairing stays open for the rest of the 3 minutes, so a second press of the button can work. Log `zigbee interview failed` |
| The device is already known (paired again) | No `create` event comes; on its `device_announce` (or an interview's `successful`) during pairing Mira looks the device up by its IEEE address and says it is known, with its name and room. Pairing is closed, the thread ends |
| Nobody answers the question | The thread expires at its timeout (15 minutes); the device stays without room and name. Log `device onboarding expired` with the device |
| The area or the expose call is refused (the token is no administrator's) | The tool returns Home Assistant's error; Mira says she may not change devices. Log with the error |
| The rename answers with an error | The tool returns it; Mira says so and keeps the old name; the room stays set |
| The satellite is busy (music, a call) when the device is found | The announcement is spoken as every announcement of a thread would be there; nothing of its own |

### Cost

Per device: two `mqtt.publish` for pairing, one for the rename, two MQTT subscriptions through Home Assistant for the
3 minutes of pairing, one area update, one read of the device's entities, one expose call, one model round for the
question and the turns of the dialog. Nothing grows with the number of devices in the house: the subscriptions are
filtered to the bridge's topics, the device is matched by its address in one event.

## State table

Each row is checked on the test HA, first with the fake, then with a real adapter, before the code waiting for it is
built.

| # | Transition | Trigger | Signal | How Mira learns of it | Checked on the test HA |
|---|---|---|---|---|---|
| 1 | Pairing opens | `mqtt.publish` `<base>/bridge/request/permit_join` `{"time":180}` | `<base>/bridge/response/permit_join` `status: ok` (with the request's `transaction`), then `<base>/bridge/info` `permit_join: true` | `mqtt/subscribe` on `<base>/bridge/#` through HA's websocket, with the owner's token; the retained `state`, `info` and `devices` come at once on subscribing | fake 2026-11-09 |
| 2 | Device joins | Button pressed, battery in | `<base>/bridge/event` `device_joined`, then `device_announce` and `device_interview` `started` | the same subscription | fake 2026-11-09 |
| 3 | Interview done | – | `device_interview` `successful` with `supported` and `definition` (vendor, model, description, exposes), or `failed` without them | the same subscription | fake 2026-11-09 |
| 4 | HA has the device | Zigbee2MQTT's discovery after a successful interview | `device_registry_updated` `create`; the device's identifiers hold `zigbee2mqtt_<ieee>`, its `manufacturer` the vendor, `model` the description, `model_id` the model; its name is the IEEE address | the existing registry subscription | fake 2026-11-09 (the `create` came in the same moment as the interview's `successful`) |
| 5 | Pairing ends by itself | the time over | `<base>/bridge/info` `permit_join: false` | the same subscription | fake 2026-11-09 |
| 6 | Pairing closed by Mira | `permit_join` `{"time":0}` | response `status: ok`, `info` `permit_join: false` | the same subscription | fake 2026-11-09 |
| 7 | Renamed | `<base>/bridge/request/device/rename` with `homeassistant_rename` | response `status: ok`; Home Assistant removes the device and creates it again **with the same id and its area kept**, the entity ids renamed (`sensor.temperatursensor_wohnzimmer_temperature`). An unknown device: `status: error`, `Device '<name>' does not exist` | subscription; HA's registry events | fake 2026-11-09 (Home Assistant's part is real: the fake only publishes what Zigbee2MQTT publishes) |
| 8 | Device's entities | – | – | `search/related` for the device gives its entity ids (disabled ones not), `config/entity_registry/get_entries` their category; one call each | fake 2026-11-09 |

Also seen (2026-11-09): the area update (`config/device_registry/update`) and `homeassistant/expose_entity` work with the
owner's token. The bridge is a device of `manufacturer` "Zigbee2MQTT", `model` "Bridge"; `mqtt/device/debug_info` of it
gives its topics, so the base topic is read, not configured. Zigbee2MQTT also gives the bridge a switch "Permit join"
in Home Assistant; it always opens for 254 s and answers nothing, so Mira publishes the request herself.

Open until the real run: a known device paired again. The fake sends only `device_announce` for it (assumption:
zigbee-herdsman reports `deviceJoined` only for a new address); Mira then treats a `device_announce` of a known
address during pairing as that device.

Interrupting: "abbrechen" at any time cancels the thread; Mira closes pairing (row 6) if it is open. The wake word
during the announcement is an ordinary interruption of an announcement.

## Testing

- **The fake Zigbee2MQTT** (`testenv/z2m_fake`) runs next to the test broker: it answers `permit_join` and `rename`,
  publishes `bridge/info`, and on a test action plays a device joining: the events and the discovery messages, as
  Zigbee2MQTT's documentation describes them (decision 9). Kinds: a temperature sensor (battery), a plug (mains), a
  device that fails its interview, a known device paired again. After the real run the fake follows what the real
  bridge sent, as the fake satellites follow the real one ([Fake devices](fake-devices.md)).
- **The real run**: Zigbee2MQTT in the test environment (a container in `testenv/compose.yaml`, started only when an
  adapter is configured), on the test broker, with the adapter and real devices.

## Steps

1. **The fake Zigbee2MQTT** from the documentation, next to the test broker.
2. **Check the signals against the fake**: every row of the state table seen in the test HA, `mqtt/subscribe`
   through HA's websocket with Mira's token, the bridge found from Home Assistant, the rename followed by Home
   Assistant (decision 8).
3. **Tools and wait** in the service: the three tools, the bridge's subscription, the match on the registry event,
   the announcement of the thread, every error path. DV-01 to DV-10.
4. **The skill `geraet-einbinden`** and the end-to-end run against the fake at the fake Show; the guide.
5. **The real run** with the adapter and real devices at the test HA (decision 10): every row of the state table
   seen again, every scenario that real hardware can show; the fake corrected where the real bridge differs.
